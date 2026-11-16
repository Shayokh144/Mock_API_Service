package proxy

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
)

func TestSSEFirstEventArrivesBeforeStreamEnds(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	t.Cleanup(letGo)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: one\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "data: two\n\n")
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second}))
	defer proxySrv.Close()

	resp, err := http.Get(proxySrv.URL + "/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	got := make(chan string, 1)
	go func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			got <- "err:" + err.Error()
			return
		}
		got <- line
	}()
	select {
	case line := <-got:
		if !strings.Contains(line, "data: one") {
			t.Fatalf("first line = %q", line)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("first SSE event waited for the stream to end")
	}
	letGo()
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rest), "data: two") {
		t.Fatalf("rest = %q", rest)
	}
}

func TestChunkedStreamFlushesBeforeEnd(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	t.Cleanup(letGo)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "early")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "-late")
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second}))
	defer proxySrv.Close()
	resp, err := http.Get(proxySrv.URL + "/v1/chunked")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 5)
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(resp.Body, buf)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("chunked bytes waited for the stream to end")
	}
	if string(buf) != "early" {
		t.Fatalf("chunk = %q", buf)
	}
	letGo()
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(rest) != "-late" {
		t.Fatalf("rest = %q", rest)
	}
}

func TestStreamOutlivesUpstreamTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: start\n\n")
		w.(http.Flusher).Flush()
		time.Sleep(500 * time.Millisecond)
		_, _ = io.WriteString(w, "data: still-open\n\n")
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: 150 * time.Millisecond}))
	defer proxySrv.Close()
	resp, err := http.Get(proxySrv.URL + "/v1/long")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "data: start") || !strings.Contains(string(body), "data: still-open") {
		t.Fatalf("body = %s", body)
	}
}

func TestStreamHistoryKeepsFirst64KB(t *testing.T) {
	payload := strings.Repeat("a", config.HistoryBodyPreview+128)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, payload)
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, History: log}))
	defer proxySrv.Close()
	resp, err := http.Get(proxySrv.URL + "/v1/big-stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != payload {
		t.Fatalf("client length = %d", len(body))
	}
	entries := log.List()
	if len(entries) != 1 {
		t.Fatalf("entries = %d", len(entries))
	}
	entry := entries[0]
	if !entry.ResponseBodyTruncated {
		t.Fatal("history was not marked truncated")
	}
	if len(entry.ResponseBody) != config.HistoryBodyPreview {
		t.Fatalf("history length = %d", len(entry.ResponseBody))
	}
	if !strings.HasPrefix(payload, entry.ResponseBody) {
		t.Fatal("history is not a prefix of the stream")
	}
}

func TestKnownLengthResponseOverCapIsRejected(t *testing.T) {
	payload := bytes.Repeat([]byte("d"), config.MaxBufferedBody+1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: 5 * time.Second}))
	defer proxySrv.Close()
	resp, err := http.Get(proxySrv.URL + "/v1/over")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !bytes.Contains(body, []byte("upstream response too large")) {
		t.Fatalf("status=%d length=%d", resp.StatusCode, len(body))
	}
}

func TestBodyCaptureFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	capw := &bodyCapture{ResponseWriter: rec}
	if _, err := capw.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	capw.Flush()
	if !rec.Flushed {
		t.Fatal("Flush did not reach the client")
	}
}
