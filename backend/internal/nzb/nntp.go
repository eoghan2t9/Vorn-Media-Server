package nzb

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Conn is a minimal NNTP (RFC 3977) client connection: enough to
// authenticate, select a group, and fetch article bodies by message-ID.
// It deliberately doesn't implement posting, XOVER, or multi-server
// failover — a single configured server is all Vorn's acquisition flow
// needs.
type Conn struct {
	conn   net.Conn
	reader *bufio.Reader
}

// Dial connects to an NNTP server and reads its greeting.
func Dial(ctx context.Context, host string, port int, useTLS bool) (*Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	netConn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("nzb: dialing %s: %w", addr, err)
	}
	if useTLS {
		tlsConn := tls.Client(netConn, &tls.Config{ServerName: host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			netConn.Close()
			return nil, fmt.Errorf("nzb: TLS handshake with %s: %w", addr, err)
		}
		netConn = tlsConn
	}

	c := &Conn{conn: netConn, reader: bufio.NewReaderSize(netConn, 64<<10)}
	code, msg, err := c.readLine()
	if err != nil {
		netConn.Close()
		return nil, err
	}
	if code != 200 && code != 201 {
		netConn.Close()
		return nil, fmt.Errorf("nzb: unexpected greeting from %s: %d %s", addr, code, msg)
	}
	return c, nil
}

func (c *Conn) Close() error { return c.conn.Close() }

func (c *Conn) writeLine(s string) error {
	_, err := c.conn.Write([]byte(s + "\r\n"))
	return err
}

func (c *Conn) readLine() (code int, rest string, err error) {
	line, err := c.reader.ReadString('\n')
	if err != nil {
		return 0, "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if len(line) < 3 {
		return 0, "", fmt.Errorf("nzb: malformed response line %q", line)
	}
	code, err = strconv.Atoi(line[:3])
	if err != nil {
		return 0, "", fmt.Errorf("nzb: malformed response code %q", line)
	}
	return code, strings.TrimSpace(line[3:]), nil
}

// readMultiline reads a dot-terminated multi-line block (RFC 3977 §3.1.1),
// undoing byte-stuffing (a leading ".." on the wire means a literal line
// starting with ".").
func (c *Conn) readMultiline() ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if trimmed == "." {
			break
		}
		if strings.HasPrefix(trimmed, "..") {
			trimmed = trimmed[1:]
		}
		buf.WriteString(trimmed)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// Authenticate performs AUTHINFO USER/PASS. A blank user skips
// authentication entirely, for providers/servers that don't require it.
func (c *Conn) Authenticate(user, pass string) error {
	if user == "" {
		return nil
	}
	if err := c.writeLine("AUTHINFO USER " + user); err != nil {
		return err
	}
	code, msg, err := c.readLine()
	if err != nil {
		return err
	}
	if code == 281 {
		return nil
	}
	if code != 381 {
		return fmt.Errorf("nzb: AUTHINFO USER rejected: %d %s", code, msg)
	}

	if err := c.writeLine("AUTHINFO PASS " + pass); err != nil {
		return err
	}
	code, msg, err = c.readLine()
	if err != nil {
		return err
	}
	if code != 281 {
		return fmt.Errorf("nzb: authentication failed: %d %s", code, msg)
	}
	return nil
}

// Group selects the current newsgroup, required by some servers before
// BODY will resolve a message-ID.
func (c *Conn) Group(name string) error {
	if err := c.writeLine("GROUP " + name); err != nil {
		return err
	}
	code, msg, err := c.readLine()
	if err != nil {
		return err
	}
	if code != 211 {
		return fmt.Errorf("nzb: GROUP %s failed: %d %s", name, code, msg)
	}
	return nil
}

// Body fetches the raw (still yEnc-encoded) body of the article identified
// by messageID, which must include the surrounding angle brackets.
func (c *Conn) Body(messageID string) ([]byte, error) {
	if err := c.writeLine("BODY " + messageID); err != nil {
		return nil, err
	}
	code, msg, err := c.readLine()
	if err != nil {
		return nil, err
	}
	if code != 222 {
		return nil, fmt.Errorf("nzb: BODY %s failed: %d %s", messageID, code, msg)
	}
	return c.readMultiline()
}
