package rules

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWildcardSegmentRules(t *testing.T) {
	store := NewMemory()
	for _, pattern := range []string{"/v1/users/*", "/v1/*/profile", "/v1/users/*/posts/*"} {
		draft := sampleDraft()
		draft.PathPattern = pattern
		if _, err := store.Create(draft); err != nil {
			t.Fatalf("create %s: %v", pattern, err)
		}
	}
	cases := []struct {
		path string
		ok   bool
	}{
		{path: "/v1/users/42", ok: true},
		{path: "/v1/users/ada", ok: true},
		{path: "/v1/me/profile", ok: true},
		{path: "/v1/users/42/posts/7", ok: true},
		{path: "/v1/users", ok: false},
		{path: "/v1/users/42/posts", ok: false},
		{path: "/v1/users/", ok: false},
	}
	for _, tc := range cases {
		_, ok := store.Match("GET", tc.path)
		if ok != tc.ok {
			t.Fatalf("path %s ok=%v want %v", tc.path, ok, tc.ok)
		}
	}
}

func TestPathPatternRejectsQueryAndFragment(t *testing.T) {
	cases := []struct {
		pattern string
		want    string
	}{
		{"/oauth/token?refresh_token", "path pattern can't include a query string. Use /oauth/token. Rules match the path only, so any query matches."},
		{"/oauth/token#section", "path pattern can't include a fragment. Use /oauth/token. Rules match the path only, so any query matches."},
		{"/v1/items?page=2#top", "path pattern can't include a query string. Use /v1/items. Rules match the path only, so any query matches."},
	}
	for _, tc := range cases {
		draft := sampleDraft()
		draft.PathPattern = tc.pattern
		_, err := NewMemory().Create(draft)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%s err = %v", tc.pattern, err)
		}
	}
}

func TestRejectedWildcardPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		substr  string
	}{
		{pattern: "/v1/users/*/", substr: "empty segment"},
		{pattern: "/v1/users/id*", substr: "whole segment"},
		{pattern: "/v1/users/**", substr: "whole segment"},
		{pattern: "/v1/users/*/**", substr: "whole segment"},
		{pattern: "v1/users", substr: "start with /"},
		{pattern: "/v1/users/?", substr: "query string"},
	}
	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			draft := sampleDraft()
			draft.PathPattern = tc.pattern
			_, err := NewMemory().Create(draft)
			if err == nil || !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestPrecedenceExample(t *testing.T) {
	store := NewMemory()
	for _, pattern := range []string{"/api/users/1", "/api/users/*", "/api/*/*"} {
		draft := sampleDraft()
		draft.PathPattern = pattern
		if _, err := store.Create(draft); err != nil {
			t.Fatal(err)
		}
	}
	exact, ok := store.Match("GET", "/api/users/1")
	if !ok || exact.PathPattern != "/api/users/1" {
		t.Fatalf("winner = %+v", exact)
	}
	wild, ok := store.Match("GET", "/api/users/2")
	if !ok || wild.PathPattern != "/api/users/*" {
		t.Fatalf("winner = %+v", wild)
	}
	disabled := sampleDraft()
	disabled.PathPattern = "/api/users/2"
	disabled.Enabled = false
	if _, err := store.Create(disabled); err != nil {
		t.Fatal(err)
	}
	still, ok := store.Match("GET", "/api/users/2")
	if !ok || still.PathPattern != "/api/users/*" {
		t.Fatalf("disabled exact changed winner to %+v", still)
	}
}

func TestCreatedAtThenIDTieBreak(t *testing.T) {
	store := NewMemory()
	start := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	var ids []string
	store.newID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	ids = []string{"bbbbbbbbbbbbbbbb", "aaaaaaaaaaaaaaaa"}
	store.now = func() time.Time { return start }
	first := sampleDraft()
	first.PathPattern = "/a/*"
	if _, err := store.Create(first); err != nil {
		t.Fatal(err)
	}
	second := sampleDraft()
	second.PathPattern = "/*/a"
	if _, err := store.Create(second); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Match("GET", "/a/a")
	if !ok || got.ID != "aaaaaaaaaaaaaaaa" {
		t.Fatalf("same timestamp winner = %+v", got)
	}

	store = NewMemory()
	ids = []string{"bbbbbbbbbbbbbbbb", "aaaaaaaaaaaaaaaa"}
	store.newID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	step := 0
	store.now = func() time.Time {
		step++
		return start.Add(time.Duration(step) * time.Second)
	}
	if _, err := store.Create(first); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(second); err != nil {
		t.Fatal(err)
	}
	got, ok = store.Match("GET", "/a/a")
	if !ok || got.ID != "bbbbbbbbbbbbbbbb" {
		t.Fatalf("earlier rule lost: %+v", got)
	}
}

func TestDuplicateWildcardIsRejected(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.PathPattern = "/v1/users/*"
	if _, err := store.Create(draft); err != nil {
		t.Fatal(err)
	}
	_, err := store.Create(draft)
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v", err)
	}
}
