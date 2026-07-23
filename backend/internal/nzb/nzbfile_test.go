package nzb

import (
	"strings"
	"testing"
)

const sampleNZB = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <head>
    <meta type="title">Some.Movie.2020.1080p</meta>
    <meta type="category">Movies</meta>
  </head>
  <file poster="poster@example.com" date="1600000000" subject="&quot;some.movie.2020.1080p.mkv&quot; yEnc (1/2)">
    <groups>
      <group>alt.binaries.test</group>
    </groups>
    <segments>
      <segment bytes="500000" number="2">part2@example.com</segment>
      <segment bytes="500000" number="1">part1@example.com</segment>
    </segments>
  </file>
</nzb>
`

func TestParseNZB(t *testing.T) {
	doc, err := Parse(strings.NewReader(sampleNZB))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if doc.Title() != "Some.Movie.2020.1080p" {
		t.Errorf("Title() = %q", doc.Title())
	}
	if len(doc.Files) != 1 {
		t.Fatalf("len(Files) = %d, want 1", len(doc.Files))
	}
	f := doc.Files[0]
	if len(f.Groups) != 1 || f.Groups[0] != "alt.binaries.test" {
		t.Errorf("Groups = %v", f.Groups)
	}
	if len(f.Segments) != 2 {
		t.Fatalf("len(Segments) = %d, want 2", len(f.Segments))
	}
	// Segments must come out sorted by number even though the XML listed
	// segment 2 before segment 1.
	if f.Segments[0].Number != 1 || f.Segments[0].MessageID != "part1@example.com" {
		t.Errorf("Segments[0] = %+v, want number=1 part1@example.com", f.Segments[0])
	}
	if f.Segments[1].Number != 2 || f.Segments[1].MessageID != "part2@example.com" {
		t.Errorf("Segments[1] = %+v, want number=2 part2@example.com", f.Segments[1])
	}
}

func TestSubjectFilename(t *testing.T) {
	got := SubjectFilename(`"some.movie.2020.1080p.mkv" yEnc (1/2)`)
	if got != "some.movie.2020.1080p.mkv" {
		t.Errorf("SubjectFilename = %q", got)
	}
}
