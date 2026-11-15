package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuleMatchesOnlyItsUpstreamOrAny(t *testing.T) {
	store := NewMemory()
	anyRule := sampleDraft()
	anyRule.Name = "any"
	anyRule.Body = `{"from":"any"}`
	savedAny, err := store.Create(anyRule)
	if err != nil {
		t.Fatal(err)
	}
	staging := sampleDraft()
	staging.Name = "staging"
	staging.UpstreamID = "aaaaaaaaaaaaaaaa"
	staging.Body = `{"from":"staging"}`
	savedStaging, err := store.Create(staging)
	if err != nil {
		t.Fatal(err)
	}
	develop := sampleDraft()
	develop.PathPattern = "/v1/other"
	develop.UpstreamID = "bbbbbbbbbbbbbbbb"
	if _, err := store.Create(develop); err != nil {
		t.Fatal(err)
	}

	got, ok := store.MatchActive("GET", "/v1/items", "aaaaaaaaaaaaaaaa")
	if !ok || got.ID != savedStaging.ID {
		t.Fatalf("staging match = %+v ok=%v", got, ok)
	}
	got, ok = store.MatchActive("GET", "/v1/items", "bbbbbbbbbbbbbbbb")
	if !ok || got.ID != savedAny.ID {
		t.Fatalf("other upstream should use any, got %+v ok=%v", got, ok)
	}
	got, ok = store.Match("GET", "/v1/items")
	if !ok || got.ID != savedAny.ID {
		t.Fatalf("no active upstream should use any, got %+v ok=%v", got, ok)
	}
	if _, ok := store.MatchActive("GET", "/v1/other", "aaaaaaaaaaaaaaaa"); ok {
		t.Fatal("develop rule matched while staging was active")
	}
	if _, ok := store.MatchActive("GET", "/v1/other", ""); ok {
		t.Fatal("specific rule matched with no active upstream")
	}

	exact := sampleDraft()
	exact.PathPattern = "/api/users/1"
	exact.Body = `{"from":"exact-any"}`
	if _, err := store.Create(exact); err != nil {
		t.Fatal(err)
	}
	wild := sampleDraft()
	wild.PathPattern = "/api/users/*"
	wild.UpstreamID = "aaaaaaaaaaaaaaaa"
	wild.Body = `{"from":"wild-staging"}`
	if _, err := store.Create(wild); err != nil {
		t.Fatal(err)
	}
	got, ok = store.MatchActive("GET", "/api/users/1", "aaaaaaaaaaaaaaaa")
	if !ok || string(got.Body) != `{"from":"exact-any"}` {
		t.Fatalf("exact any should beat a wildcard on one upstream, got %+v", got)
	}
}

func TestOldRulesFileLoadsAsAnyUpstream(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	body := `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rule, ok := store.MatchActive("GET", "/v1/old", "aaaaaaaaaaaaaaaa")
	if !ok || rule.UpstreamID != "" {
		t.Fatalf("legacy = %+v ok=%v", rule, ok)
	}
	draft := sampleDraft()
	draft.UpstreamID = "aaaaaaaaaaaaaaaa"
	created, err := store.Create(draft)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"upstream_id": "aaaaaaaaaaaaaaaa"`) {
		t.Fatalf("file = %s", raw)
	}
	if err := store.DetachUpstream("aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(created.ID)
	if !ok || got.UpstreamID != "" {
		t.Fatalf("detached = %+v", got)
	}
}

func TestDetachRefusesDuplicateAnyRule(t *testing.T) {
	store := NewMemory()
	anyRule := sampleDraft()
	if _, err := store.Create(anyRule); err != nil {
		t.Fatal(err)
	}
	specific := sampleDraft()
	specific.UpstreamID = "aaaaaaaaaaaaaaaa"
	if _, err := store.Create(specific); err != nil {
		t.Fatal(err)
	}
	err := store.DetachUpstream("aaaaaaaaaaaaaaaa")
	if err == nil || !strings.Contains(err.Error(), "duplicate Any rule") {
		t.Fatalf("err = %v", err)
	}
	if len(store.List()) != 2 {
		t.Fatalf("rules changed: %+v", store.List())
	}
}
