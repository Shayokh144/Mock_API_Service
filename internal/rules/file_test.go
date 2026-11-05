package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileRoundTripAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "rules.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("file should not exist yet")
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("load created a missing file")
	}
	rule, err := store.Create(sampleDraft())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"rules"`) || !strings.Contains(text, rule.ID) || !strings.Contains(text, `"/v1/items"`) {
		t.Fatalf("file = %s", text)
	}
	if strings.Contains(text, path+".tmp") {
		t.Fatal("temp path leaked into the file")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file was left behind")
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.Match("GET", "/v1/items")
	if !ok || got.ID != rule.ID || string(got.Body) != `{"ok":true}` {
		t.Fatalf("reloaded = %+v ok=%v", got, ok)
	}
}

func TestInvalidJSONIsLeftUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "RULES_PATH") {
		t.Fatalf("err = %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "{" {
		t.Fatalf("file changed to %s", raw)
	}
}

func TestFailedSaveKeepsMemoryAndPreviousFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(sampleDraft()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.path = filepath.Join(blocker, "rules.json")
	other := sampleDraft()
	other.PathPattern = "/v1/other"
	if _, err := store.Create(other); err == nil {
		t.Fatal("expected save to fail")
	}
	if len(store.List()) != 1 {
		t.Fatalf("memory changed: %+v", store.List())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(before) {
		t.Fatal("previous file changed")
	}
}
