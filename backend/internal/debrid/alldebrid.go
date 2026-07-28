package debrid

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// classify maps AllDebrid's error-code string onto the shared taxonomy --
// AllDebrid documents codes like "AUTH_BAD_APIKEY"/"AUTH_MISSING_APIKEY" for
// bad credentials and several "*_TOO_MANY*"/"*LIMIT*" codes for quota/limit
// conditions, so a substring match covers the cases Vorn actually needs to
// react to without hand-enumerating AllDebrid's full error-code list.
func (e *adAPIError) classify() error {
	if e == nil {
		return nil
	}
	switch {
	case strings.Contains(e.Code, "AUTH"):
		return &ClassifiedError{Kind: FailureAuth, Err: e}
	case strings.Contains(e.Code, "TOO_MANY"), strings.Contains(e.Code, "LIMIT"):
		return &ClassifiedError{Kind: FailureQuotaExceeded, Err: e}
	default:
		return e
	}
}

const (
	allDebridBaseURL = "https://api.alldebrid.com"
	// AllDebrid doesn't publish a fixed per-key rate cap the way Real-Debrid
	// (250/min) and TorBox (300/min) do -- this is a conservative default,
	// not a documented number, purely to keep a lagging admin UI/scanner
	// from hammering the API.
	allDebridRateLimit = 300
	adPollInterval     = 1 * time.Second
	adPollTimeout      = 20 * time.Minute
)

// AllDebridClient implements Provider against the AllDebrid v4/v4.1 API
// (https://docs.alldebrid.com/): upload a magnet, poll its status until
// AllDebrid has cached it (statusCode 4 == "Ready"), then list its files --
// each file's link is already directly downloadable, no separate
// unlock step is needed for magnet-cached files.
type AllDebridClient struct {
	httpClient   *http.Client
	baseURL      string
	limiter      *Limiter
	pollInterval time.Duration
}

func NewAllDebridClient() *AllDebridClient {
	return &AllDebridClient{
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      allDebridBaseURL,
		limiter:      NewLimiter(allDebridRateLimit),
		pollInterval: adPollInterval,
	}
}

func (c *AllDebridClient) Name() string { return "alldebrid" }

func (c *AllDebridClient) Resolve(ctx context.Context, apiKey, magnetOrHash string) (*ResolveResult, error) {
	magnet := asMagnet(magnetOrHash)

	id, ready, err := c.uploadMagnet(ctx, apiKey, magnet)
	if err != nil {
		return nil, fmt.Errorf("alldebrid: uploading magnet: %w", err)
	}
	// The upload response already tells us if this magnet was instantly
	// cached (the common case for a popular torrent) -- polling anyway would
	// just be a needless extra round trip.
	if !ready {
		if err := c.waitUntilReady(ctx, apiKey, id); err != nil {
			return nil, err
		}
	}
	files, err := c.magnetFiles(ctx, apiKey, id)
	if err != nil {
		return nil, fmt.Errorf("alldebrid: listing files: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("alldebrid: magnet %d has no files", id)
	}
	return &ResolveResult{ProviderRef: strconv.Itoa(id), Files: files}, nil
}

// Delete removes a magnet from the account via /v4/magnet/delete, reclaiming
// the active-magnet slot it held.
func (c *AllDebridClient) Delete(ctx context.Context, apiKey, providerRef string) error {
	if providerRef == "" {
		return nil
	}
	var resp adEnvelope[json.RawMessage]
	if err := c.do(ctx, "/v4/magnet/delete", apiKey, url.Values{"id": {providerRef}}, &resp); err != nil {
		return err
	}
	return resp.checkStatus()
}

// adAPIError is AllDebrid's error shape, both at the envelope level
// ({"status":"error","error":{...}}) and per-magnet inside a batch upload
// response ({"magnets":[{"magnet":"...","error":{...}}]}).
type adAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *adAPIError) Error() string {
	if e == nil {
		return "alldebrid: request failed"
	}
	return fmt.Sprintf("alldebrid: %s: %s", e.Code, e.Message)
}

type adEnvelope[T any] struct {
	Status string      `json:"status"`
	Data   T           `json:"data"`
	Error  *adAPIError `json:"error,omitempty"`
}

func (e adEnvelope[T]) checkStatus() error {
	if e.Status == "success" {
		return nil
	}
	if e.Error != nil {
		return e.Error.classify()
	}
	return fmt.Errorf("alldebrid: request failed with status %q", e.Status)
}

type adMagnetUploadData struct {
	Magnets []struct {
		ID    int         `json:"id"`
		Ready bool        `json:"ready"`
		Error *adAPIError `json:"error,omitempty"`
	} `json:"magnets"`
}

func (c *AllDebridClient) uploadMagnet(ctx context.Context, apiKey, magnet string) (id int, ready bool, err error) {
	var resp adEnvelope[adMagnetUploadData]
	if err := c.do(ctx, "/v4/magnet/upload", apiKey, url.Values{"magnets[]": {magnet}}, &resp); err != nil {
		return 0, false, err
	}
	if err := resp.checkStatus(); err != nil {
		return 0, false, err
	}
	if len(resp.Data.Magnets) == 0 {
		return 0, false, fmt.Errorf("alldebrid: no magnet returned from upload")
	}
	m := resp.Data.Magnets[0]
	if m.Error != nil {
		return 0, false, m.Error.classify()
	}
	return m.ID, m.Ready, nil
}

type adMagnetStatusData struct {
	Magnets []struct {
		ID         int    `json:"id"`
		Status     string `json:"status"`
		StatusCode int    `json:"statusCode"`
	} `json:"magnets"`
}

// waitUntilReady polls v4.1/magnet/status. statusCode 4 is AllDebrid's
// documented "Ready" state; values above it are terminal error states
// (dead magnet, too many downloads, etc), values below are processing.
func (c *AllDebridClient) waitUntilReady(ctx context.Context, apiKey string, id int) error {
	deadline := time.Now().Add(adPollTimeout)
	for {
		var resp adEnvelope[adMagnetStatusData]
		form := url.Values{"id": {strconv.Itoa(id)}}
		err := c.doVersioned(ctx, "/v4.1/magnet/status", apiKey, form, &resp)
		if err == nil {
			err = resp.checkStatus()
		}
		if err != nil {
			var ce *ClassifiedError
			if errors.As(err, &ce) && ce.Kind == FailureRateLimited {
				if wait := capRetry(ce.RetryAfter, deadline); wait > 0 {
					select {
					case <-time.After(wait):
						continue
					case <-ctx.Done():
						return ctx.Err()
					}
				}
			}
			return err
		}
		if len(resp.Data.Magnets) == 0 {
			return fmt.Errorf("alldebrid: magnet %d not found while polling status", id)
		}
		m := resp.Data.Magnets[0]
		if m.StatusCode == 4 {
			return nil
		}
		if m.StatusCode > 4 {
			return &ClassifiedError{Kind: FailurePermanent, Err: fmt.Errorf("alldebrid: magnet %d: %s", id, m.Status)}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("alldebrid: magnet %d: timed out waiting past status %q", id, m.Status)
		}
		select {
		case <-time.After(c.pollInterval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// adFileEntry is one entry in a magnet's file tree: a leaf file has n/s/l
// (name/size/link), a folder has n/e (name/nested entries) with no size or
// link of its own.
type adFileEntry struct {
	N string        `json:"n"`
	S int64         `json:"s"`
	L string        `json:"l"`
	E []adFileEntry `json:"e"`
}

type adMagnetFilesData struct {
	Magnets []struct {
		ID    int           `json:"id"`
		Files []adFileEntry `json:"files"`
	} `json:"magnets"`
}

func flattenAllDebridFiles(entries []adFileEntry) []ResolvedFile {
	var out []ResolvedFile
	for _, e := range entries {
		if len(e.E) > 0 {
			out = append(out, flattenAllDebridFiles(e.E)...)
			continue
		}
		if e.L == "" {
			continue
		}
		out = append(out, ResolvedFile{Name: e.N, SizeBytes: e.S, StreamURL: e.L})
	}
	return out
}

func (c *AllDebridClient) magnetFiles(ctx context.Context, apiKey string, id int) ([]ResolvedFile, error) {
	var resp adEnvelope[adMagnetFilesData]
	if err := c.do(ctx, "/v4/magnet/files", apiKey, url.Values{"id[]": {strconv.Itoa(id)}}, &resp); err != nil {
		return nil, err
	}
	if err := resp.checkStatus(); err != nil {
		return nil, err
	}
	if len(resp.Data.Magnets) == 0 {
		return nil, fmt.Errorf("alldebrid: magnet %d not found while listing files", id)
	}
	return flattenAllDebridFiles(resp.Data.Magnets[0].Files), nil
}

type adUserData struct {
	User struct {
		Username     string `json:"username"`
		IsPremium    bool   `json:"isPremium"`
		PremiumUntil int64  `json:"premiumUntil"`
	} `json:"user"`
}

// AccountInfo calls AllDebrid's POST /v4/user, the lightest documented
// endpoint that requires only a valid Authorization header.
func (c *AllDebridClient) AccountInfo(ctx context.Context, apiKey string) (*AccountInfo, error) {
	var resp adEnvelope[adUserData]
	if err := c.do(ctx, "/v4/user", apiKey, url.Values{}, &resp); err != nil {
		return nil, fmt.Errorf("alldebrid: fetching account info: %w", err)
	}
	if err := resp.checkStatus(); err != nil {
		return nil, err
	}
	info := &AccountInfo{Username: resp.Data.User.Username, Premium: resp.Data.User.IsPremium}
	if info.Premium && resp.Data.User.PremiumUntil > 0 {
		info.Detail = "premium until " + time.Unix(resp.Data.User.PremiumUntil, 0).Format("2006-01-02")
	} else {
		info.Detail = "free account (no premium)"
	}
	return info, nil
}

// do issues a request against the v4 base path.
func (c *AllDebridClient) do(ctx context.Context, path, apiKey string, form url.Values, out any) error {
	return c.doVersioned(ctx, path, apiKey, form, out)
}

// doVersioned issues a form-encoded POST -- path carries its own version
// prefix (/v4/... or /v4.1/...) since AllDebrid moved magnet/status to
// v4.1 while everything else Vorn needs is still on v4.
func (c *AllDebridClient) doVersioned(ctx context.Context, path, apiKey string, form url.Values, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return &ClassifiedError{Kind: FailureRateLimited, RetryAfter: retryAfter(resp.Header), Err: fmt.Errorf("alldebrid: rate limited (429) on %s", path)}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return &ClassifiedError{Kind: FailureAuth, Err: fmt.Errorf("alldebrid: unauthorized (401) on %s", path)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("alldebrid: %s: unexpected status %d: %s", path, resp.StatusCode, string(data))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
