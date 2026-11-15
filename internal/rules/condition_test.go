package rules

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConditions(t *testing.T) {
	query, err := ParseConditions("refresh_token\npage=2\n\n", "query")
	if err != nil {
		t.Fatal(err)
	}
	if len(query) != 2 || query[0].Value != nil || query[1].Value == nil || *query[1].Value != "2" {
		t.Fatalf("query = %#v", query)
	}
	body, err := ParseConditions("grant_type=refresh_token\nuser.role=admin", "body")
	if err != nil {
		t.Fatal(err)
	}
	if FormatConditions(body) != "grant_type=refresh_token\nuser.role=admin\n" {
		t.Fatalf("format = %q", FormatConditions(body))
	}
	cases := []struct{ text, label, substr string }{
		{"=missing", "query", "missing a key"},
		{"user..role=admin", "body", "empty path segment"},
		{"grant_type=a\ngrant_type=b", "body", `duplicate body condition "grant_type"`},
		{"a=1\na", "query", `duplicate query condition "a"`},
	}
	for _, tc := range cases {
		_, err := ParseConditions(tc.text, tc.label)
		if err == nil || !strings.Contains(err.Error(), tc.substr) {
			t.Fatalf("%q err = %v", tc.text, err)
		}
	}
}

func TestQueryConditions(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.Method = http.MethodGet
	draft.PathPattern = "/v1/items"
	draft.Match.Query = mustParse(t, "refresh_token\npage=2", "query")
	if _, err := store.Create(draft); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.MatchHTTP(http.MethodGet, "/v1/items", "", "refresh_token=abc&page=2", nil, nil); !ok {
		t.Fatal("present and exact query did not match")
	}
	if _, ok := store.MatchHTTP(http.MethodGet, "/v1/items", "", "page=2&page=9&refresh_token=", nil, nil); !ok {
		t.Fatal("repeated query value did not match")
	}
	if _, ok := store.MatchHTTP(http.MethodGet, "/v1/items", "", "page=3&refresh_token=abc", nil, nil); ok {
		t.Fatal("wrong query value matched")
	}
	if _, ok := store.MatchHTTP(http.MethodGet, "/v1/items", "", "page=2", nil, nil); ok {
		t.Fatal("missing query key matched")
	}
	if _, ok := store.Match("GET", "/v1/items"); ok {
		t.Fatal("Match ignored query conditions")
	}
}

func TestBodyConditionsFormAndJSON(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.Method = http.MethodPost
	draft.PathPattern = "/oauth/token"
	draft.Match.Body = mustParse(t, "grant_type=refresh_token\nuser.role=admin\nuser.count=1\nuser.active=true\nuser.nested.ok=false", "body")
	if _, err := store.Create(draft); err != nil {
		t.Fatal(err)
	}
	form := "grant_type=refresh_token&user.role=admin&user.count=1&user.active=true&user.nested.ok=false"
	formHeader := http.Header{"Content-Type": {"application/x-www-form-urlencoded; charset=UTF-8"}}
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", formHeader, []byte(form)); !ok {
		t.Fatal("form body did not match")
	}
	jsonHeader := http.Header{"Content-Type": {"application/json; charset=utf-8"}}
	jsonBody := []byte(`{"grant_type":"refresh_token","user":{"role":"admin","count":1,"active":true,"nested":{"ok":false}}}`)
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", jsonHeader, jsonBody); !ok {
		t.Fatal("json body did not match")
	}
	other := []byte(`{"grant_type":"authorization_code","user":{"role":"admin","count":1,"active":true,"nested":{"ok":false}}}`)
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", jsonHeader, other); ok {
		t.Fatal("login json matched the refresh rule")
	}
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", http.Header{"Content-Type": {"text/plain"}}, jsonBody); ok {
		t.Fatal("plain body matched")
	}
}

func TestBodyConditionSizeCapAndGzip(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.Method = http.MethodPost
	draft.PathPattern = "/oauth/token"
	draft.Match.Body = mustParse(t, "grant_type=refresh_token", "body")
	if _, err := store.Create(draft); err != nil {
		t.Fatal(err)
	}
	plain := []byte(`{"grant_type":"refresh_token"}`)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	gzipHeader := http.Header{
		"Content-Type":     {"application/json"},
		"Content-Encoding": {"gzip"},
	}
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", gzipHeader, compressed.Bytes()); !ok {
		t.Fatal("gzip json did not match")
	}
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", gzipHeader, []byte{0x1f, 0x8b, 0x08, 0x00}); ok {
		t.Fatal("corrupt gzip matched")
	}
	huge := bytes.Repeat([]byte("a"), MaxMatchBody+1)
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "page=2", gzipHeader, huge); ok {
		t.Fatal("oversized body matched a body condition")
	}

	queryOnly := sampleDraft()
	queryOnly.PathPattern = "/v1/items"
	queryOnly.Match.Query = mustParse(t, "page=2", "query")
	if _, err := store.Create(queryOnly); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.MatchHTTP(http.MethodGet, "/v1/items", "", "page=2", nil, huge); !ok {
		t.Fatal("query condition lost to an oversized body")
	}

	var bomb bytes.Buffer
	bw := gzip.NewWriter(&bomb)
	if _, err := bw.Write(bytes.Repeat([]byte("x"), MaxMatchBody+1)); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", gzipHeader, bomb.Bytes()); ok {
		t.Fatal("gzip larger than the cap matched")
	}
}

func TestConditionSpecificityAndDuplicates(t *testing.T) {
	store := NewMemory()
	start := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	step := 0
	store.now = func() time.Time {
		step++
		return start.Add(time.Duration(step) * time.Second)
	}
	plain := sampleDraft()
	plain.Method = http.MethodPost
	plain.PathPattern = "/oauth/token"
	plain.Name = "plain"
	if _, err := store.Create(plain); err != nil {
		t.Fatal(err)
	}
	refresh := plain
	refresh.Name = "refresh"
	refresh.Match.Body = mustParse(t, "grant_type=refresh_token", "body")
	if _, err := store.Create(refresh); err != nil {
		t.Fatal(err)
	}
	both := refresh
	both.Name = "both"
	both.Match.Query = mustParse(t, "debug=1", "query")
	if _, err := store.Create(both); err != nil {
		t.Fatal(err)
	}
	header := http.Header{"Content-Type": {"application/json"}}
	body := []byte(`{"grant_type":"refresh_token"}`)
	got, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "debug=1", header, body)
	if !ok || got.Name != "both" {
		t.Fatalf("more conditions lost: %+v", got)
	}
	got, ok = store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", header, body)
	if !ok || got.Name != "refresh" {
		t.Fatalf("body condition lost to the plain rule: %+v", got)
	}
	got, ok = store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", header, []byte(`{"grant_type":"authorization_code"}`))
	if !ok || got.Name != "plain" {
		t.Fatalf("login did not fall through: %+v", got)
	}

	swapped := refresh
	swapped.PathPattern = "/oauth/token"
	swapped.Match.Body = mustParse(t, "grant_type=refresh_token", "body")
	if _, err := store.Create(swapped); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate err = %v", err)
	}
	firstOrder := refresh
	firstOrder.Name = "order"
	firstOrder.Match.Body = mustParse(t, "grant_type=refresh_token\nextra=1", "body")
	if _, err := store.Create(firstOrder); err != nil {
		t.Fatal(err)
	}
	secondOrder := firstOrder
	secondOrder.Match.Body = mustParse(t, "extra=1\ngrant_type=refresh_token", "body")
	if _, err := store.Create(secondOrder); err == nil {
		t.Fatal("reordered conditions were treated as a different rule")
	}

	wild := sampleDraft()
	wild.PathPattern = "/oauth/*"
	wild.Match.Body = mustParse(t, "grant_type=refresh_token", "body")
	if _, err := store.Create(wild); err != nil {
		t.Fatal(err)
	}
	got, ok = store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", header, body)
	if !ok || got.Name != "refresh" {
		t.Fatalf("wildcard with conditions beat the exact rule: %+v", got)
	}
}

func TestOldRulesFileHasNoConditions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	legacy := `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"POST","path_pattern":"/oauth/token","status_code":200,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := store.MatchHTTP(http.MethodPost, "/oauth/token", "", "", http.Header{"Content-Type": {"application/json"}}, []byte(`{"grant_type":"authorization_code"}`))
	if !ok || rule.Name != "Old" || conditionCount(rule.Match) != 0 {
		t.Fatalf("legacy = %+v ok=%v", rule, ok)
	}
	added := sampleDraft()
	added.Method = http.MethodPost
	added.PathPattern = "/oauth/token"
	added.Match.Body = mustParse(t, "grant_type=refresh_token", "body")
	saved, err := store.Create(added)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"match"`) || !strings.Contains(string(raw), `"grant_type"`) {
		t.Fatalf("file = %s", raw)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(saved.ID)
	if !ok || FormatConditions(got.Match.Body) != "grant_type=refresh_token\n" {
		t.Fatalf("reloaded = %+v", got)
	}
}

func mustParse(t *testing.T, text, label string) []Condition {
	t.Helper()
	list, err := ParseConditions(text, label)
	if err != nil {
		t.Fatal(err)
	}
	return list
}
