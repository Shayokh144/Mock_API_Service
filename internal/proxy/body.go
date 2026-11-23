package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
)

var errBodyTooLarge = errors.New("body too large")

func readUpTo(r io.Reader, max int) ([]byte, error) {
	if r == nil {
		return []byte{}, nil
	}
	payload, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > max {
		return payload, errBodyTooLarge
	}
	return payload, nil
}

type bodyCapture struct {
	http.ResponseWriter
	status       int
	buf          bytes.Buffer
	truncated    bool
	wrote        bool
	previewLimit int
	previewCut   bool
	upgrade      bool
	unwritten    bool
	limit        int
	fault        string
	step         int
	// sent is Host, Origin, and Referer as forwarded upstream.
	sent http.Header
}

func (c *bodyCapture) WriteHeader(status int) {
	if c.wrote {
		return
	}
	c.wrote = true
	c.status = status
	c.ResponseWriter.WriteHeader(status)
}

func (c *bodyCapture) Write(p []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	limit := c.limit
	if limit <= 0 {
		limit = config.MaxBufferedBody
	}
	if c.previewLimit > 0 {
		limit = c.previewLimit
	}
	room := limit - c.buf.Len()
	if room > 0 {
		if len(p) <= room {
			_, _ = c.buf.Write(p)
		} else {
			_, _ = c.buf.Write(p[:room])
			c.noteCut()
		}
	} else if len(p) > 0 {
		c.noteCut()
	}
	return c.ResponseWriter.Write(p)
}

func (c *bodyCapture) noteCut() {
	if c.previewLimit > 0 {
		c.previewCut = true
		return
	}
	c.truncated = true
}

// Flush pushes buffered bytes to the client. ReverseProxy uses it for streams.
func (c *bodyCapture) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *bodyCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := c.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijack is not available")
	}
	return hj.Hijack()
}

func (c *bodyCapture) note(status int, body []byte) {
	c.wrote = true
	c.status = status
	limit := c.limit
	if limit <= 0 {
		limit = config.MaxBufferedBody
	}
	if len(body) > limit {
		_, _ = c.buf.Write(body[:limit])
		c.truncated = true
		return
	}
	_, _ = c.buf.Write(body)
}

func (c *bodyCapture) snapshot() (int, http.Header, []byte, bool) {
	status := c.status
	if status == 0 {
		switch {
		case c.upgrade:
			status = http.StatusSwitchingProtocols
		case c.fault != "" || c.unwritten:
			status = 0
		default:
			status = http.StatusOK
		}
	}
	header := c.Header().Clone()
	body := append([]byte(nil), c.buf.Bytes()...)
	if c.previewCut {
		body = cutUTF8(body, len(body))
	}
	c.buf.Reset()
	return status, header, body, c.truncated
}
