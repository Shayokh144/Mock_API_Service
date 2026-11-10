package proxy

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func rawExample(mock bool) rules.Draft {
	body := "{\r\n  \"error\": \"unauthorized\",\r\n  \"message\": \"The access token has expired or is invalid.\"\r\n}"
	return rules.Draft{
		Name: "expired token", Enabled: true, Method: "GET", PathPattern: "/v1/auth",
		StatusCode: 200, ResponseMode: rules.ModeRaw, MockHeader: mock,
		RawResponse: "HTTP/1.1 401 Unauthorized\r\nContent-Type: application/json; charset=utf-8\r\nConnection: close\r\nContent-Length: 5\r\n\r\n" + body,
	}
}

func TestRawResponseOnTheWire(t *testing.T) {
	store := rules.NewMemory()
	saved, err := store.Create(rawExample(false))
	if err != nil {
		t.Fatal(err)
	}
	opt, err := store.Create(rules.Draft{
		Name: "marked", Enabled: true, Method: "GET", PathPattern: "/v1/marked",
		ResponseMode: rules.ModeRaw, MockHeader: true,
		RawResponse: "HTTP/1.1 401 Unauthorized\nContent-Type: application/json\nConnection: close\n\n{\"error\":\"unauthorized\"}",
	})
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	handler := New(Options{Rules: store, History: log, Timeout: time.Second})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	body := []byte("{\r\n  \"error\": \"unauthorized\",\r\n  \"message\": \"The access token has expired or is invalid.\"\r\n}")
	got := readRaw(t, ln.Addr().String(), "/v1/auth")
	if !bytes.Contains(got, []byte("HTTP/1.1 401 Unauthorized\r\n")) {
		t.Fatalf("status line missing: %q", got)
	}
	if bytes.Contains(got, []byte("X-Mock-Response")) || bytes.Contains(got, []byte("Content-Length: 5\r\n")) || bytes.Contains(got, []byte("\r\nDate:")) {
		t.Fatalf("unexpected headers: %q", got)
	}
	wantLen := "Content-Length: " + strconv.Itoa(len(body)) + "\r\n"
	if !bytes.Contains(got, []byte(wantLen)) || !bytes.HasSuffix(got, body) {
		t.Fatalf("wire = %q", got)
	}
	entry := waitHistory(t, log, saved.ID)
	if entry.Source != history.SourceMock || entry.Status != 401 {
		t.Fatalf("history = %+v", entry)
	}
	if entry.ResponseHeaders.Get("Content-Type") != "application/json; charset=utf-8" || entry.ResponseHeaders.Get("X-Mock-Response") != "" {
		t.Fatalf("recorded headers = %v", entry.ResponseHeaders)
	}
	if string(entry.ResponseBody) == "" || !bytes.Contains([]byte(entry.ResponseBody), []byte(`"error"`)) {
		t.Fatalf("recorded body = %q", entry.ResponseBody)
	}

	marked := readRaw(t, ln.Addr().String(), "/v1/marked")
	if !bytes.Contains(marked, []byte("X-Mock-Response: true\r\n")) {
		t.Fatalf("opt-in header missing: %q", marked)
	}
	markedEntry := waitHistory(t, log, opt.ID)
	if markedEntry.ResponseHeaders.Get("X-Mock-Response") != "true" {
		t.Fatalf("recorded headers = %v", markedEntry.ResponseHeaders)
	}
}

func waitHistory(t *testing.T, log *history.Log, ruleID string) history.Entry {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range log.List() {
			if entry.RuleID == ruleID {
				return entry
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("history missing rule %s", ruleID)
	return history.Entry{}
}

func TestRawResponseWaitsForTheDelay(t *testing.T) {
	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Name: "slow raw", Enabled: true, Method: "GET", PathPattern: "/v1/slow",
		ResponseMode: rules.ModeRaw, DelayMS: 1500,
		RawResponse: "HTTP/1.1 401 Unauthorized\nContent-Type: application/json\n\n{\"error\":\"unauthorized\"}",
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	handler := New(Options{Rules: store, History: log, Timeout: time.Second})
	var waited time.Duration
	handler.wait = func(ctx context.Context, d time.Duration) error {
		waited = d
		return nil
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/slow", nil)
	handler.ServeHTTP(rec, req)
	if waited != 1500*time.Millisecond {
		t.Fatalf("waited = %s", waited)
	}
	if rec.Code != 401 || rec.Body.String() != `{"error":"unauthorized"}` {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Mock-Response") != "" {
		t.Fatalf("marker = %q", rec.Header().Get("X-Mock-Response"))
	}
	entry := log.List()[0]
	if entry.Source != history.SourceMock || entry.DelayMS != 1500 || entry.RuleID == "" {
		t.Fatalf("history = %+v", entry)
	}
}

func readRaw(t *testing.T, addr, path string) []byte {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET "+path+" HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("empty response")
	}
	return got
}
