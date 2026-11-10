package rules

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestParseRawResponse(t *testing.T) {
	body := "{\r\n  \"error\": \"unauthorized\",\r\n  \"message\": \"The access token has expired or is invalid.\"\r\n}"
	crlf := "HTTP/1.1 401 Unauthorized\r\nContent-Type: application/json; charset=utf-8\r\nConnection: close\r\nContent-Length: 5\r\n\r\n" + body
	parsed, err := ParseRawResponse(crlf)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Code != 401 || parsed.Reason != "Unauthorized" || parsed.Proto != "HTTP/1.1" {
		t.Fatalf("status = %+v", parsed)
	}
	if string(parsed.Body) != body {
		t.Fatalf("body = %q", parsed.Body)
	}
	if !WantsClose(parsed) {
		t.Fatal("expected connection close")
	}
	wire := string(WireBytes(parsed, false))
	if !strings.HasPrefix(wire, "HTTP/1.1 401 Unauthorized\r\n") || strings.Contains(wire, "X-Mock-Response") || strings.Contains(wire, "Content-Length: 5\r\n") {
		t.Fatalf("wire = %q", wire)
	}
	if !strings.Contains(wire, "Content-Length: "+strconv.Itoa(len(body))+"\r\n\r\n") || !strings.HasSuffix(wire, body) {
		t.Fatalf("wire = %q", wire)
	}
	marked := WireBytes(parsed, true)
	if !strings.Contains(string(marked), "X-Mock-Response: true\r\n") {
		t.Fatalf("marked = %q", marked)
	}

	lfBody := "{\n  \"error\": \"unauthorized\"\n}"
	lf := "HTTP/1.0 201 Created\nX-Trace: abc\n\n" + lfBody
	parsed, err = ParseRawResponse(lf)
	if err != nil || parsed.Proto != "HTTP/1.0" || parsed.Code != 201 || string(parsed.Body) != lfBody {
		t.Fatalf("lf = %+v err=%v", parsed, err)
	}

	noHeaders := "HTTP/1.1 204\n\n"
	parsed, err = ParseRawResponse(noHeaders)
	if err != nil || parsed.Code != 204 || parsed.Reason != "" || len(parsed.Body) != 0 {
		t.Fatalf("empty = %+v err=%v", parsed, err)
	}

	cases := []struct {
		raw    string
		substr string
	}{
		{"", "status line"},
		{"not http\n\n", "status line"},
		{"HTTP/1.1 99 Nope\n\n", "status code"},
		{"HTTP/1.1 600 Nope\n\n", "status code"},
		{"HTTP/2 200 OK\n\n", "status line"},
		{"HTTP/1.1 200 OK\nContent-Type: application/json", "blank line"},
		{"HTTP/1.1 200 OK\nBad header\n\n", "Name: value"},
		{"HTTP/1.1 200 OK\nTransfer-Encoding: chunked\n\nhello", "Transfer-Encoding"},
	}
	for _, tc := range cases {
		if _, err := ParseRawResponse(tc.raw); err == nil || !strings.Contains(err.Error(), tc.substr) {
			t.Fatalf("raw %q err = %v", tc.raw, err)
		}
	}
}

func TestRawModePersistsAndOldFileStaysFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	draft := sampleDraft()
	draft.ResponseMode = ModeRaw
	draft.RawResponse = "HTTP/1.1 401 Unauthorized\nContent-Type: application/json\n\n{\"error\":\"unauthorized\"}"
	draft.MockHeader = true
	draft.DelayMS = 1500
	saved, err := store.Create(draft)
	if err != nil || saved.ResponseMode != ModeRaw || saved.StatusCode != 401 || saved.DelayMS != 1500 || !saved.MockHeader {
		t.Fatalf("saved = %+v err=%v", saved, err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(saved.ID)
	if !ok || got.RawResponse != saved.RawResponse || got.ResponseMode != ModeRaw {
		t.Fatalf("reloaded = %+v ok=%v", got, ok)
	}

	legacy := filepath.Join(dir, "legacy.json")
	body := `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(legacy, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := Load(legacy)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := old.Match("GET", "/v1/old")
	if !ok || rule.ResponseMode != "" || rule.RawResponse != "" || rule.StatusCode != 200 {
		t.Fatalf("legacy = %+v ok=%v", rule, ok)
	}
}
