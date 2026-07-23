package nzb

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeNNTPServer runs a tiny NNTP server good enough to exercise Conn's
// greeting, AUTHINFO, GROUP, and BODY handling, including dot-stuffing on
// the wire (a literal line starting with "." is doubled per RFC 3977).
func fakeNNTPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		w := bufio.NewWriter(conn)
		r := bufio.NewReader(conn)
		write := func(s string) {
			w.WriteString(s + "\r\n")
			w.Flush()
		}

		write("200 fake NNTP server ready")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "AUTHINFO USER"):
				write("381 password required")
			case strings.HasPrefix(line, "AUTHINFO PASS"):
				write("281 authenticated")
			case strings.HasPrefix(line, "GROUP"):
				write("211 100 1 100 alt.binaries.test")
			case strings.HasPrefix(line, "BODY"):
				write("222 body follows")
				// A line that's just "." on the wire is doubled to "..";
				// the decoded article body must come back as a single ".".
				write("=ybegin line=128 size=1 name=t.bin")
				write("..")
				write("=yend size=1")
				write(".")
			case line == "QUIT":
				write("205 bye")
				return
			}
		}
	}()

	return ln.Addr().String()
}

func TestConnAuthGroupBody(t *testing.T) {
	addr := fakeNNTPServer(t)
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	var port int
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := Dial(ctx, host, port, false)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if err := conn.Authenticate("user", "pass"); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if err := conn.Group("alt.binaries.test"); err != nil {
		t.Fatalf("Group: %v", err)
	}
	body, err := conn.Body("<msg1@example.com>")
	if err != nil {
		t.Fatalf("Body: %v", err)
	}

	decoded, m, err := decodeYenc(body)
	if err != nil {
		t.Fatalf("decodeYenc: %v", err)
	}
	// The dot-stuffed ".." line un-stuffs to a single "." (0x2E) content
	// byte, which yEnc's -42 shift turns into 0x04. Getting this value
	// proves un-dot-stuffing ran before yEnc decoding rather than after
	// (which would have left an extra "." in the decoded output).
	if string(decoded) != "\x04" {
		t.Errorf("decoded = %q, want a single 0x04 byte", decoded)
	}
	if m.Name != "t.bin" {
		t.Errorf("name = %q", m.Name)
	}
}
