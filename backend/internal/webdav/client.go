// Package webdav provides a minimal WebDAV client for listing files from a
// WebDAV server (e.g. TorBox's https://webdav.torbox.app) via PROPFIND,
// returning discovered files that the scanner can stage and promote into
// media_items.
package webdav

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"
)

// DiscoveredFile mirrors scanner.DiscoveredFile -- a file found on a WebDAV
// server, ready to be staged and promoted into a media_item.
type DiscoveredFile struct {
	Path       string // full WebDAV URL, e.g. https://webdav.torbox.app/Movies/Foo.mkv
	SizeBytes  int64
	ModifiedAt time.Time
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<propfind xmlns="DAV:">
  <prop>
    <resourcetype xmlns="DAV:"/>
    <getcontentlength xmlns="DAV:"/>
    <getlastmodified xmlns="DAV:"/>
    <displayname xmlns="DAV:"/>
  </prop>
</propfind>`

// multistatus is the top-level element of a WebDAV PROPFIND 207 response.
type multistatus struct {
	XMLName   xml.Name   `xml:"DAV: multistatus"`
	Responses []response `xml:"DAV: response"`
}

type response struct {
	Href     string   `xml:"DAV: href"`
	Propstat propstat `xml:"DAV: propstat"`
}

type propstat struct {
	Status string `xml:"DAV: status"`
	Prop   prop   `xml:"DAV: prop"`
}

type prop struct {
	ResourceType     resourceType `xml:"DAV: resourcetype"`
	ContentLength    *int64       `xml:"DAV: getcontentlength"`
	LastModified     string       `xml:"DAV: getlastmodified"`
	DisplayName      string       `xml:"DAV: displayname"`
}

type resourceType struct {
	Collection *struct{} `xml:"DAV: collection"`
}

// httpClient is shared across calls (connection pooling) and has a timeout
// suitable for listing directories -- generous enough for a large PROPFIND
// response, bounded so a hung server doesn't stall a scan forever.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// Walk recursively lists dirURL on the WebDAV server, authenticating with
// HTTP Basic auth (username "torbox" per TorBox convention, password is
// the account's API key). Only files (non-collection resources) are emitted
// to onFile; directories are followed recursively. The returned path in
// DiscoveredFile is the full WebDAV URL.
func Walk(ctx context.Context, dirURL, apiKey string, onFile func(DiscoveredFile)) error {
	return walk(ctx, dirURL, dirURL, apiKey, onFile, nil)
}

// depth tracks recursion depth to guard against pathological redirect loops
// or infinite directory structures -- 20 is more than enough for any real
// WebDAV tree.
func walk(ctx context.Context, rootURL, dirURL, apiKey string, onFile func(DiscoveredFile), depth *int) error {
	if depth == nil {
		d := 0
		depth = &d
	}
	*depth++
	if *depth > 20 {
		return fmt.Errorf("webdav: exceeded max recursion depth at %s", dirURL)
	}
	defer func() { *depth-- }()

	entries, err := propfind(ctx, dirURL, apiKey)
	if err != nil {
		return fmt.Errorf("webdav: listing %s: %w", dirURL, err)
	}

	for _, e := range entries {
		if e.isCollection {
			// Resolve relative hrefs against the current directory URL first,
			// then skip the directory's own self-reference (the PROPFIND
			// response always includes the queried URL itself as the first
			// entry, with isCollection=true). Resolving before comparing
			// handles both absolute and relative href forms correctly.
			childURL := resolveURL(dirURL, e.href)
			if strings.TrimRight(childURL, "/") == strings.TrimRight(dirURL, "/") {
				continue
			}
			if err := walk(ctx, rootURL, childURL, apiKey, onFile, depth); err != nil {
				return err
			}
			continue
		}

		// Only emit files that look like video/audio -- same filter the
		// local scanner applies (see scanner.IsVideoFile/IsAudioFile).
		name := e.displayName
		if name == "" {
			name = path.Base(e.href)
		}
		if !isMediaFile(name) {
			continue
		}

		fileURL := resolveURL(dirURL, e.href)
		onFile(DiscoveredFile{
			Path:       fileURL,
			SizeBytes:  e.sizeBytes,
			ModifiedAt: e.lastModified,
		})
	}
	return nil
}

type propfindEntry struct {
	href         string
	displayName  string
	isCollection bool
	sizeBytes    int64
	lastModified time.Time
}

func propfind(ctx context.Context, dirURL, apiKey string) ([]propfindEntry, error) {
	// Ensure trailing slash so the server treats this as a collection.
	reqURL := dirURL
	if !strings.HasSuffix(reqURL, "/") {
		reqURL += "/"
	}

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", reqURL, strings.NewReader(propfindBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")
	req.SetBasicAuth("torbox", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("unauthorized (401) -- check API key")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found (404): %s", reqURL)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var ms multistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("parsing PROPFIND response: %w", err)
	}

	entries := make([]propfindEntry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		if !strings.Contains(r.Propstat.Status, "200") {
			continue
		}
		entry := propfindEntry{
			href:         r.Href,
			displayName:  r.Propstat.Prop.DisplayName,
			isCollection: r.Propstat.Prop.ResourceType.Collection != nil,
			lastModified: time.Now(), // fallback if no LastModified header
		}
		if r.Propstat.Prop.ContentLength != nil {
			entry.sizeBytes = *r.Propstat.Prop.ContentLength
		}
		if t, err := http.ParseTime(r.Propstat.Prop.LastModified); err == nil {
			entry.lastModified = t
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// resolveURL resolves href against baseDir (the directory URL we PROPFIND'd).
// href can be an absolute URL (returned as-is), a server-absolute path
// (starts with "/" -- resolved against the origin of baseDir), or a relative
// path (appended to baseDir).
func resolveURL(baseDir, href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if strings.HasPrefix(href, "/") {
		// Server-absolute path: resolve against the origin, not the
		// current directory. e.g. baseDir="http://host/Movies/" and
		// href="/Shows/" becomes "http://host/Shows/".
		return serverOrigin(baseDir) + href
	}
	// Relative path: append to baseDir.
	baseDir = strings.TrimRight(baseDir, "/")
	href = strings.TrimLeft(href, "/")
	return baseDir + "/" + href
}

// serverOrigin returns the scheme+host portion of rawURL (everything up to
// the first single slash after "://"). e.g. "http://host/dir" returns
// "http://host".
func serverOrigin(rawURL string) string {
	idx := strings.Index(rawURL, "://")
	if idx < 0 {
		return rawURL
	}
	rest := rawURL[idx+3:]
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return rawURL
	}
	return rawURL[:idx+3+slash]
}

// videoExtensions mirrors scanner.videoExtensions -- only these extensions
// are treated as playable media files during WebDAV discovery.
var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true,
	".mov": true, ".wmv": true, ".flv": true, ".webm": true,
	".mts": true, ".m2ts": true, ".ts": true, ".mpg": true,
	".mpeg": true, ".ogv": true, ".divx": true,
}

// audioExtensions mirrors scanner.audioExtensions.
var audioExtensions = map[string]bool{
	".mp3": true, ".flac": true, ".aac": true, ".ogg": true,
	".wma": true, ".m4a": true, ".wav": true, ".opus": true,
}

func isMediaFile(name string) bool {
	name = strings.ToLower(name)
	for ext := range videoExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	for ext := range audioExtensions {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// Refresh instructs the WebDAV server to refresh its file index. For TorBox
// this means hitting GET https://webdav.torbox.app/refresh/ (the server
// refreshes its file listing every 15 min; calling this forces an immediate
// refresh so newly added files appear right away).
func Refresh(ctx context.Context, baseURL, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/refresh/", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth("torbox", apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webdav: refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("webdav: refresh returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
