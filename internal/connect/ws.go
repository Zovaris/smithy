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
	"sync"
	"time"
)

// wsMax caps one received message so a server can't make the daemon buffer without bound.
const wsMax = 1 << 20

// wsConn is a minimal RFC 6455 client: text messages, ping/pong and close; no extensions.
type wsConn struct {
	c  net.Conn
	r  *bufio.Reader
	mu sync.Mutex
}

var errWSClosed = errors.New("websocket closed")

func wsDial(ctx context.Context, raw string) (*wsConn, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "wss" && u.Scheme != "ws") || u.Host == "" {
		return nil, errors.New("bad websocket url")
	}
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
	if u.Scheme == "wss" {
		c, err = (&tls.Dialer{NetDialer: d, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", host)
	} else {
		c, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, err
	}
	kb := make([]byte, 16)
	_, _ = rand.Read(kb)
	key := base64.StdEncoding.EncodeToString(kb)
	path := u.RequestURI()
	req := "GET " + path + " HTTP/1.1\r\nHost: " + u.Host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\nSec-WebSocket-Version: 13\r\nUser-Agent: smithy\r\n\r\n"
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
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	if resp.StatusCode != http.StatusSwitchingProtocols || !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") ||
		resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(sum[:]) {
		c.Close()
		return nil, errors.New("websocket handshake refused: HTTP " + resp.Status)
	}
	_ = c.SetDeadline(time.Time{})
	return &wsConn{c: c, r: br}, nil
}

func (w *wsConn) write(op byte, p []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	h := []byte{0x80 | op}
	switch n := len(p); {
	case n < 126:
		h = append(h, 0x80|byte(n))
	case n <= 0xffff:
		h = append(h, 0x80|126, byte(n>>8), byte(n))
	default:
		h = append(h, 0x80|127)
		h = binary.BigEndian.AppendUint64(h, uint64(n))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	h = append(h, mask...)
	b := make([]byte, len(p))
	for i := range p {
		b[i] = p[i] ^ mask[i%4]
	}
	_ = w.c.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, err := w.c.Write(append(h, b...))
	return err
}

// WriteText sends one text message.
func (w *wsConn) WriteText(p []byte) error { return w.write(1, p) }

// ReadText returns the next text message, answering pings on the way.
func (w *wsConn) ReadText() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(w.r, h[:]); err != nil {
			return nil, err
		}
		fin, op := h[0]&0x80 != 0, h[0]&0x0f
		masked, n := h[1]&0x80 != 0, uint64(h[1]&0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if masked {
			return nil, errors.New("websocket: masked server frame")
		}
		if op >= 8 && (n > 125 || !fin) {
			return nil, errors.New("websocket: bad control frame")
		}
		if op == 0 && msg == nil {
			return nil, errors.New("websocket: bad fragment")
		}
		if n > wsMax || uint64(len(msg))+n > wsMax {
			return nil, errors.New("websocket message too large")
		}
		p := make([]byte, n)
		if _, err := io.ReadFull(w.r, p); err != nil {
			return nil, err
		}
		switch op {
		case 8:
			_ = w.write(8, nil)
			return nil, errWSClosed
		case 9:
			if err := w.write(10, p); err != nil {
				return nil, err
			}
		case 10:
		case 0, 1, 2:
			if op != 0 && msg != nil {
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
			return nil, errors.New("websocket: bad opcode")
		}
	}
}

// Close sends a close frame and drops the connection.
func (w *wsConn) Close() error {
	_ = w.write(8, []byte{0x03, 0xe8})
	return w.c.Close()
}
