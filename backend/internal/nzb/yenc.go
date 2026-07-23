package nzb

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// meta holds the yEnc control-line fields needed to decode an article body
// and place its decoded bytes at the right offset in the reassembled file.
// PartBegin/PartEnd are 1-based, inclusive, matching the yEnc spec.
type meta struct {
	Name      string
	Size      int64 // total decoded size of the whole (possibly multi-part) file
	Part      int
	Total     int
	PartBegin int64
	PartEnd   int64
}

// decodeYenc decodes one NNTP article body (already de-dot-stuffed, "\n"
// line endings) that's yEnc-encoded, returning the decoded bytes and the
// header metadata needed to place them.
func decodeYenc(raw []byte) ([]byte, meta, error) {
	var m meta
	var out bytes.Buffer

	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	inBody := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "=ybegin"):
			parseYencControlLine(line, &m)
			inBody = true
			continue
		case strings.HasPrefix(line, "=ypart"):
			parseYencControlLine(line, &m)
			continue
		case strings.HasPrefix(line, "=yend"):
			inBody = false
			continue
		}
		if !inBody {
			continue
		}
		decodeYencLine(line, &out)
	}
	if err := scanner.Err(); err != nil {
		return nil, m, err
	}
	if m.Name == "" {
		return nil, m, fmt.Errorf("nzb: article has no =ybegin header (not yEnc, or a non-article response)")
	}

	// Single-part encodes have no =ypart line; the whole decoded body maps
	// to the start of the file.
	if m.PartBegin == 0 && m.PartEnd == 0 {
		m.PartBegin = 1
		m.PartEnd = m.Size
	}
	return out.Bytes(), m, nil
}

// decodeYencLine reverses yEnc's byte transform: normally byte-42 (mod
// 256), except after a literal '=' escape, where the following byte is
// byte-42-64 (mod 256) instead.
func decodeYencLine(line string, out *bytes.Buffer) {
	data := []byte(line)
	for i := 0; i < len(data); i++ {
		b := data[i]
		if b == '=' && i+1 < len(data) {
			i++
			b = data[i] - 64
		}
		out.WriteByte(b - 42)
	}
}

// parseYencControlLine parses the space-separated key=value pairs on a
// =ybegin/=ypart line. "name" is special-cased: it's always the last field
// and may itself contain spaces, so everything from "name=" onward is
// taken verbatim rather than being split on whitespace.
func parseYencControlLine(line string, m *meta) {
	kind := "=ypart"
	if strings.HasPrefix(line, "=ybegin") {
		kind = "=ybegin"
	}

	rest := line
	if nameIdx := strings.Index(line, "name="); nameIdx >= 0 {
		m.Name = strings.TrimSpace(line[nameIdx+len("name="):])
		rest = line[:nameIdx]
	}

	for _, f := range strings.Fields(rest) {
		kv := strings.SplitN(f, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "part":
			m.Part, _ = strconv.Atoi(kv[1])
		case "total":
			m.Total, _ = strconv.Atoi(kv[1])
		case "size":
			if kind == "=ybegin" {
				m.Size, _ = strconv.ParseInt(kv[1], 10, 64)
			}
		case "begin":
			m.PartBegin, _ = strconv.ParseInt(kv[1], 10, 64)
		case "end":
			m.PartEnd, _ = strconv.ParseInt(kv[1], 10, 64)
		}
	}
}
