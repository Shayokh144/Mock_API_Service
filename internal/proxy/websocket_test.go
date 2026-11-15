package proxy

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestWebSocketEchoThroughPrefixOutlivesTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/echo" {
			http.Error(w, "path "+r.URL.Path, http.StatusNotFound)
			return
		}
		if !isProtocolUpgrade(r.Header) || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "not a websocket upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijack unavailable", http.StatusInternalServerError)
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		accept := websocketAccept(r.Header.Get("Sec-WebSocket-Key"))
		_, _ = bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n")
		if err := bufrw.Flush(); err != nil {
			return
		}
		echoWebSocket(conn, bufrw.Reader)
	}))
	defer upstream.Close()

	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	store := upstreams.NewMemory()
	if _, err := store.Create("realtime", base.String(), "/sock"); err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	proxySrv := httptest.NewServer(New(Options{
		Upstreams: store,
		Timeout:   200 * time.Millisecond,
		History:   log,
	}))
	defer proxySrv.Close()

	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", proxyURL.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	key := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	_, err = io.WriteString(conn, "GET /sock/echo HTTP/1.1\r\nHost: "+proxyURL.Host+"\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: "+key+"\r\nSec-WebSocket-Version: 13\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "101") {
		t.Fatalf("status line = %q", status)
	}
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}

	// The upstream timeout is 200ms. The socket has to still echo after that.
	time.Sleep(500 * time.Millisecond)
	payload := "hello-through-proxy"
	if err := writeClientText(conn, payload); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := readServerText(reader)
	if err != nil {
		t.Fatal(err)
	}
	if got != payload {
		t.Fatalf("echo = %q", got)
	}
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for len(log.List()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	entries := log.List()
	if len(entries) != 1 {
		t.Fatalf("history entries = %d", len(entries))
	}
	entry := entries[0]
	if entry.Status != http.StatusSwitchingProtocols {
		t.Fatalf("history status = %d", entry.Status)
	}
	if entry.Path != "/sock/echo" || entry.ForwardedPath != "/echo" {
		t.Fatalf("paths = %s forwarded %s", entry.Path, entry.ForwardedPath)
	}
	if entry.ResponseBody != "protocol upgrade; messages were not captured" || entry.ResponseBodyTruncated {
		t.Fatalf("history body = %q truncated=%v", entry.ResponseBody, entry.ResponseBodyTruncated)
	}
	if entry.Source != history.SourceUpstream {
		t.Fatalf("source = %s", entry.Source)
	}
}

func websocketAccept(key string) string {
	sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func writeClientText(w io.Writer, payload string) error {
	mask := []byte{1, 2, 3, 4}
	frame := []byte{0x81, byte(0x80 | len(payload))}
	frame = append(frame, mask...)
	for i := 0; i < len(payload); i++ {
		frame = append(frame, payload[i]^mask[i%4])
	}
	_, err := w.Write(frame)
	return err
}

func readServerText(r io.Reader) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return "", err
	}
	n := int(header[1] & 0x7f)
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return "", err
	}
	return string(payload), nil
}

func echoWebSocket(conn net.Conn, pending *bufio.Reader) {
	reader := pending
	if reader == nil {
		reader = bufio.NewReader(conn)
	}
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(reader, header); err != nil {
			return
		}
		n := int(header[1] & 0x7f)
		masked := header[1]&0x80 != 0
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(reader, mask[:]); err != nil {
				return
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return
		}
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
		if header[0]&0x0f == 0x8 {
			return
		}
		out := []byte{0x81, byte(len(payload))}
		out = append(out, payload...)
		if _, err := conn.Write(out); err != nil {
			return
		}
	}
}
