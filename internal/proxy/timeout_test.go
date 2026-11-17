package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUpstreamTimeoutReturns504(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: 80 * time.Millisecond}))
	defer proxySrv.Close()

	resp, err := http.Get(proxySrv.URL + "/v1/slow")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusGatewayTimeout || !strings.Contains(string(body), "upstream timed out") {
		t.Fatalf("slow = %d %s", resp.StatusCode, body)
	}
	if resp.Header.Get("X-Mock-Response") != "" {
		t.Fatal("timeout response has mock header")
	}
}

func TestSmallMaxBodyRejectsRequestAndBufferedResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "8")
		_, _ = w.Write([]byte("abcdefgh"))
	}))
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: time.Second, MaxBody: 4}))
	defer proxySrv.Close()

	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/v1/in", strings.NewReader("12345"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("request = %d", resp.StatusCode)
	}

	got, err := http.Get(proxySrv.URL + "/v1/out")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	raw, _ := io.ReadAll(got.Body)
	if got.StatusCode != http.StatusBadGateway || !strings.Contains(string(raw), "upstream response too large") {
		t.Fatalf("response = %d %s", got.StatusCode, raw)
	}
}
