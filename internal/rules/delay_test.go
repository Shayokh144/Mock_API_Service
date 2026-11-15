package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDelayBoundsAndOldFile(t *testing.T) {
	store := NewMemory()
	draft := sampleDraft()
	draft.DelayMS = 1500
	saved, err := store.Create(draft)
	if err != nil || saved.DelayMS != 1500 {
		t.Fatalf("saved = %+v err=%v", saved, err)
	}
	for _, ms := range []int{-1, 60001} {
		draft.DelayMS = ms
		draft.PathPattern = "/v1/other"
		if _, err := store.Create(draft); err == nil || !strings.Contains(err.Error(), "delay_ms") {
			t.Fatalf("delay %d err = %v", ms, err)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	fileStore, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	zero := sampleDraft()
	zero.DelayMS = 0
	if _, err := fileStore.Create(zero); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "delay_ms") {
		t.Fatalf("zero delay was stored: %s", raw)
	}
	slow := sampleDraft()
	slow.PathPattern = "/v1/slow"
	slow.DelayMS = 60000
	created, err := fileStore.Create(slow)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"delay_ms": 60000`) {
		t.Fatalf("file = %s", raw)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Get(created.ID)
	if !ok || got.DelayMS != 60000 {
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
	if !ok || rule.DelayMS != 0 {
		t.Fatalf("legacy = %+v ok=%v", rule, ok)
	}

	broken := filepath.Join(dir, "broken.json")
	body = `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"delay_ms":60001,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(broken, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(broken); err == nil || !strings.Contains(err.Error(), "delay_ms") {
		t.Fatalf("broken file err = %v", err)
	}
}
