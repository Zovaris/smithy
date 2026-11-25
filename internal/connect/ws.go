package connect

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// wsConn is a minimal RFC 6455 client: text frames out (masked), text frames in (fragments
// joined), pings answered, at most wsMax bytes a message.
type wsConn struct {
	c  net.Conn
	br *bufio.Reader
}

const wsMax = 1 << 20

var errWSClosed = errors.New("websocket closed")

func wsDial(ctx context.Context, u *url.URL) (*wsConn, error) {
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	d := &net.Dialer{Timeout: 15 * time.Second}
	var c net.Conn
	var err error
	if u.Scheme == "wss" {
		c, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", host)
	} else {
		c, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	k := make([]byte, 16)
	_, _ = rand.Read(k)
	key := base64.StdEncoding.EncodeToString(k)
	path := u.RequestURI()
	req := "GET " + path + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\n\r\n"
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		c.Close()
		return nil, err
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		c.Close()
		return nil, err
	}
	resp.Body.Close()
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.StatusCode != http.StatusSwitchingProtocols || resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		c.Close()
		return nil, errors.New("websocket handshake refused: HTTP " + resp.Status)
	}
	_ = c.SetDeadline(time.Time{})
	return &wsConn{c: c, br: br}, nil
}

func (w *wsConn) write(op byte, p []byte) error {
	h := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		h = append(h, 0x80|byte(n))
	case n < 1<<16:
		h = append(h, 0x80|126, byte(n>>8), byte(n))
	default:
		h = binary.BigEndian.AppendUint64(append(h, 0x80|127), uint64(n))
	}
	m := make([]byte, 4)
	_, _ = rand.Read(m)
	b := append(h, m...)
	for i, c := range p {
		b = append(b, c^m[i%4])
	}
	_ = w.c.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err := w.c.Write(b)
	return err
}

// WriteText sends one text message.
func (w *wsConn) WriteText(p []byte) error { return w.write(0x1, p) }

// Read returns the next text message, waiting until deadline (zero: forever).
func (w *wsConn) Read(deadline time.Time) ([]byte, error) {
	_ = w.c.SetReadDeadline(deadline)
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.br, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var e [2]byte
			if _, err := io.ReadFull(w.br, e[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(e[:]))
		case 127:
			var e [8]byte
			if _, err := io.ReadFull(w.br, e[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(e[:])
		}
		var mask []byte
		if h[1]&0x80 != 0 {
			return nil, errors.New("websocket: masked server frame")
		}
		if op >= 0x8 && (n > 125 || !fin) {
			return nil, errors.New("websocket: bad control frame")
		}
		if op == 0x0 && msg == nil {
			return nil, errors.New("websocket: bad fragment")
		}
		if n > wsMax || uint64(len(msg))+n > wsMax {
			return nil, errors.New("websocket message too large")
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.br, p); err != nil {
			return nil, err
		}
		if mask != nil {
			for i := range p {
				p[i] ^= mask[i%4]
			}
		}
		switch op {
		case 0x8:
			_ = w.write(0x8, nil)
			return nil, errWSClosed
		case 0x9:
			if err := w.write(0xA, p); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		case 0x0, 0x1, 0x2:
			if op != 0x0 && msg != nil {
				return nil, errors.New("websocket: bad fragment")
			}
			msg = append(msg, p...)
			if msg == nil {
				msg = []byte{}
			}
			if fin {
				return msg, nil
			}
		default:
			return nil, errors.New("websocket: bad frame")
		}
	}
}

func (w *wsConn) Close() error { return w.c.Close() }

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func wsURLOK(u *url.URL) bool {
	return u.Scheme == "wss" && (u.Hostname() == "slack.com" || strings.HasSuffix(u.Hostname(), ".slack.com"))
}
