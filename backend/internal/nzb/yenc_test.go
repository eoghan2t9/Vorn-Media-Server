package nzb

import (
	"bytes"
	"strconv"
	"testing"
)

// encodeYencLine is a minimal yEnc encoder used only by tests, so the
// decoder can be verified against a real round trip instead of hand-typed
// encoded strings.
func encodeYencLine(data []byte) string {
	var out bytes.Buffer
	for _, b := range data {
		e := b + 42
		if e == 0x00 || e == 0x0A || e == 0x0D || e == 0x3D {
			out.WriteByte('=')
			e += 64
		}
		out.WriteByte(e)
	}
	return out.String()
}

func TestDecodeYencSinglePart(t *testing.T) {
	payload := []byte("the quick brown fox jumps over the lazy dog 0123456789")

	var article bytes.Buffer
	article.WriteString("=ybegin line=128 size=" + strconv.Itoa(len(payload)) + " name=test.bin\n")
	article.WriteString(encodeYencLine(payload) + "\n")
	article.WriteString("=yend size=" + strconv.Itoa(len(payload)) + " crc32=00000000\n")

	decoded, m, err := decodeYenc(article.Bytes())
	if err != nil {
		t.Fatalf("decodeYenc: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded mismatch:\n got: %q\nwant: %q", decoded, payload)
	}
	if m.Name != "test.bin" {
		t.Errorf("name = %q, want test.bin", m.Name)
	}
	if m.PartBegin != 1 || m.PartEnd != int64(len(payload)) {
		t.Errorf("single-part offsets = [%d,%d], want [1,%d]", m.PartBegin, m.PartEnd, len(payload))
	}
}

func TestDecodeYencMultiPart(t *testing.T) {
	payload := []byte("second segment of a bigger file")

	var article bytes.Buffer
	article.WriteString("=ybegin part=2 total=3 line=128 size=1000 name=movie.mkv\n")
	article.WriteString("=ypart begin=101 end=" + strconv.Itoa(100+len(payload)) + "\n")
	article.WriteString(encodeYencLine(payload) + "\n")
	article.WriteString("=yend size=" + strconv.Itoa(len(payload)) + " part=2 pcrc32=deadbeef\n")

	decoded, m, err := decodeYenc(article.Bytes())
	if err != nil {
		t.Fatalf("decodeYenc: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded mismatch:\n got: %q\nwant: %q", decoded, payload)
	}
	if m.PartBegin != 101 || m.PartEnd != int64(100+len(payload)) {
		t.Errorf("part offsets = [%d,%d], want [101,%d]", m.PartBegin, m.PartEnd, 100+len(payload))
	}
	if m.Part != 2 || m.Total != 3 {
		t.Errorf("part/total = %d/%d, want 2/3", m.Part, m.Total)
	}
}

func TestDecodeYencEscapedBytes(t *testing.T) {
	// Bytes that land on NUL/LF/CR/'=' after the +42 shift and must be escaped:
	// (214+42)%256=0x00, (224+42)%256=0x0A, (227+42)%256=0x0D, (19+42)%256=0x3D.
	payload := []byte{214, 224, 227, 19, 'A'}

	var article bytes.Buffer
	article.WriteString("=ybegin line=128 size=" + strconv.Itoa(len(payload)) + " name=escaped.bin\n")
	article.WriteString(encodeYencLine(payload) + "\n")
	article.WriteString("=yend size=" + strconv.Itoa(len(payload)) + "\n")

	decoded, _, err := decodeYenc(article.Bytes())
	if err != nil {
		t.Fatalf("decodeYenc: %v", err)
	}
	if !bytes.Equal(decoded, payload) {
		t.Fatalf("decoded mismatch:\n got: %v\nwant: %v", decoded, payload)
	}
}
