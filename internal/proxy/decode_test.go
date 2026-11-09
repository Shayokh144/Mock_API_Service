package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
)

func TestDisplayBodyDecodesEncodings(t *testing.T) {
	raw := []byte(`{"hotel":"easy","ok":true}`)
	cases := []struct {
		name     string
		encoding string
		body     []byte
		want     string
		note     string
	}{
		{name: "gzip", encoding: "gzip", body: gzipBytes(t, raw), want: "easy"},
		{name: "deflate zlib", encoding: "deflate", body: zlibBytes(t, raw), want: "easy"},
		{name: "deflate raw", encoding: "deflate", body: rawDeflateBytes(t, raw), want: "easy"},
		{name: "br", encoding: "br", body: brotliBytes(t, raw), want: "easy"},
		{name: "identity", encoding: "identity", body: raw, want: "easy"},
		{name: "stacked", encoding: "gzip, br", body: brotliBytes(t, gzipBytes(t, raw)), want: "easy"},
		{name: "unknown", encoding: "zstd", body: raw, note: `could not decode content-encoding "zstd": unsupported encoding`},
		{name: "corrupt gzip", encoding: "gzip", body: []byte{0x1f, 0x8b, 0x08, 0x00}, note: `could not decode content-encoding "gzip": corrupt body`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{"Content-Encoding": {tc.encoding}, "Content-Type": {"application/json"}}
			got, truncated := displayBody(headers, tc.body)
			if tc.note != "" {
				if got != tc.note || truncated {
					t.Fatalf("got %q truncated=%v", got, truncated)
				}
				if strings.Contains(got, "easy") {
					t.Fatal("note included payload text")
				}
				return
			}
			if truncated || !strings.Contains(got, tc.want) || strings.Contains(got, "could not decode") {
				t.Fatalf("got %q truncated=%v", got, truncated)
			}
			if !strings.Contains(got, "\n") {
				t.Fatal("json was not indented")
			}
		})
	}
}

func TestDisplayBodyCapsDecompressedSize(t *testing.T) {
	raw := bytes.Repeat([]byte("x"), config.HistoryBodyPreview+2048)
	headers := http.Header{"Content-Encoding": {"gzip"}, "Content-Type": {"text/plain"}}
	got, truncated := displayBody(headers, gzipBytes(t, raw))
	if !truncated || len(got) != config.HistoryBodyPreview || !strings.Contains(got, "x") {
		t.Fatalf("len=%d truncated=%v", len(got), truncated)
	}
	if strings.Contains(got, "could not decode") {
		t.Fatalf("cap became an error: %s", got)
	}
}

func TestDisplayBodyNotesBinary(t *testing.T) {
	body := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}
	headers := http.Header{"Content-Type": {"image/png"}}
	got, truncated := displayBody(headers, body)
	if truncated || got != "binary body, 6 bytes, content-type image/png" {
		t.Fatalf("got %q truncated=%v", got, truncated)
	}
	headers.Set("Content-Type", "text/plain")
	got, truncated = displayBody(headers, []byte{0xff, 0xfe, 0xfd})
	if truncated || !strings.HasPrefix(got, "binary body, 3 bytes, content-type text/plain") {
		t.Fatalf("non-utf8 got %q truncated=%v", got, truncated)
	}
}

func TestDisplayBodyDecodesRequestAndKeepsForwardedBytes(t *testing.T) {
	response := []byte(`{"hotel":"easy","ok":true}`)
	compressed := gzipBytes(t, response)
	var seenBody []byte
	var seenEncoding string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenEncoding = r.Header.Get("Content-Encoding")
		var err error
		seenBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream", "yes")
		if _, err := w.Write(compressed); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: 2 * time.Second, History: log}))
	defer proxySrv.Close()

	requestBody := gzipBytes(t, []byte(`{"n":1}`))
	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/hotel", bytes.NewReader(requestBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, compressed) {
		t.Fatalf("client bytes differ: %d vs %d", len(got), len(compressed))
	}
	if resp.Header.Get("Content-Encoding") != "gzip" || resp.Header.Get("X-Upstream") != "yes" {
		t.Fatalf("headers = %v", resp.Header)
	}
	if seenEncoding != "gzip" || !bytes.Equal(seenBody, requestBody) {
		t.Fatalf("upstream saw encoding %q body len %d", seenEncoding, len(seenBody))
	}
	entry := log.List()[0]
	if !strings.Contains(entry.ResponseBody, `"hotel": "easy"`) || strings.Contains(entry.ResponseBody, "could not decode") {
		t.Fatalf("history response = %s", entry.ResponseBody)
	}
	if !strings.Contains(entry.RequestBody, `"n": 1`) {
		t.Fatalf("history request = %s", entry.RequestBody)
	}
	if entry.ResponseHeaders.Get("Content-Encoding") != "gzip" {
		t.Fatalf("stored headers = %v", entry.ResponseHeaders)
	}
}

func gzipBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zlibBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func rawDeflateBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func brotliBytes(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
