// Package nzb implements Usenet (NZB) acquisition: parsing .nzb index
// files, a minimal NNTP client for fetching articles, a yEnc decoder for
// the encoded article bodies, and download orchestration that reassembles
// segments into files and (optionally) repairs them with par2.
package nzb

import (
	"bufio"
	"encoding/xml"
	"io"
	"regexp"
	"sort"
	"strings"
)

type Segment struct {
	Bytes     int64  `xml:"bytes,attr"`
	Number    int    `xml:"number,attr"`
	MessageID string `xml:",chardata"`
}

type File struct {
	Subject  string    `xml:"subject,attr"`
	Groups   []string  `xml:"groups>group"`
	Segments []Segment `xml:"segments>segment"`
}

type metaEntry struct {
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type NZB struct {
	Meta  []metaEntry `xml:"head>meta"`
	Files []File      `xml:"file"`
}

// Parse reads a .nzb document. Segments within each file are sorted by
// their declared number, since the XML order isn't guaranteed.
func Parse(r io.Reader) (*NZB, error) {
	dec := xml.NewDecoder(r)
	dec.CharsetReader = charsetReader

	var doc NZB
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	for i := range doc.Files {
		segs := doc.Files[i].Segments
		sort.Slice(segs, func(a, b int) bool { return segs[a].Number < segs[b].Number })
	}
	return &doc, nil
}

// charsetReader lets Parse accept .nzb files that declare a non-UTF-8
// encoding, which encoding/xml otherwise rejects outright. Most indexer
// software still emits ISO-8859-1 (the NZB DTD's own example encoding);
// its code points map 1:1 onto the first 256 Unicode code points, so
// widening each byte to UTF-8 is a correct, complete transcode. Any other
// declared charset is passed through unchanged as a best-effort fallback,
// since .nzb content (subjects, group names) is ASCII in practice.
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(charset) {
	case "iso-8859-1", "latin1", "windows-1252":
		return &latin1Reader{r: bufio.NewReader(input)}, nil
	default:
		return input, nil
	}
}

type latin1Reader struct {
	r       *bufio.Reader
	pending []byte
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	for len(l.pending) == 0 {
		b, err := l.r.ReadByte()
		if err != nil {
			return 0, err
		}
		if b < 0x80 {
			l.pending = append(l.pending, b)
		} else {
			l.pending = append(l.pending, 0xC0|(b>>6), 0x80|(b&0x3F))
		}
	}
	n := copy(p, l.pending)
	l.pending = l.pending[n:]
	return n, nil
}

// Title returns the nzb's <head><meta type="title"> value, if present.
func (n *NZB) Title() string {
	for _, m := range n.Meta {
		if m.Type == "title" {
			return strings.TrimSpace(m.Value)
		}
	}
	return ""
}

var quotedFilenameRe = regexp.MustCompile(`"([^"]+)"`)

// SubjectFilename best-effort extracts a filename from a segment subject
// line like `"Some.Movie.2020.1080p.mkv" yEnc (1/50)`, for use only as a
// fallback when a yEnc article's =ybegin header doesn't carry a name
// (which is the authoritative source).
func SubjectFilename(subject string) string {
	if m := quotedFilenameRe.FindStringSubmatch(subject); m != nil {
		return m[1]
	}
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, subject)
}
