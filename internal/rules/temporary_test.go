package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemporaryRulesAreLeftOutOfLaterSaves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyDrafts([]Draft{{
		Name:        "Ping",
		APIName:     "ping",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/ping",
		StatusCode:  200,
		Body:        `{"ok":true}`,
	}}, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("non-persist scenario wrote rules.json")
	}
	if _, err := store.Create(Draft{
		Name:        "Kept",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/kept",
		StatusCode:  200,
		Body:        `{"ok":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	saved := readRulesFile(t, path)
	if strings.Contains(saved, "ping") || strings.Contains(saved, "/v1/ping") {
		t.Fatalf("scenario rule leaked: %s", saved)
	}
	if !strings.Contains(saved, "/v1/kept") {
		t.Fatalf("durable rule missing: %s", saved)
	}
	ping, ok := store.FindByAPIName("ping")
	if !ok || !ping.Temporary {
		t.Fatalf("in-memory scenario rule = %+v ok=%v", ping, ok)
	}
	if _, err := store.Update(ping.ID, Draft{
		Name:        "Ping edited",
		APIName:     "ping",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/ping",
		StatusCode:  201,
		Body:        `{"ok":false}`,
	}); err != nil {
		t.Fatal(err)
	}
	saved = readRulesFile(t, path)
	if strings.Contains(saved, "ping") || strings.Contains(saved, "Ping edited") || strings.Contains(saved, "/v1/ping") {
		t.Fatalf("edited temporary rule was saved: %s", saved)
	}
	edited, ok := store.Get(ping.ID)
	if !ok || !edited.Temporary || edited.StatusCode != 201 {
		t.Fatalf("edited rule = %+v ok=%v", edited, ok)
	}
}

func TestPersistedScenarioIsSaved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyDrafts([]Draft{{
		Name:        "Ping",
		APIName:     "ping",
		Enabled:     true,
		Method:      "GET",
		PathPattern: "/v1/ping",
		StatusCode:  200,
		Body:        `{"ok":true}`,
	}}, false, true); err != nil {
		t.Fatal(err)
	}
	saved := readRulesFile(t, path)
	if !strings.Contains(saved, "ping") || !strings.Contains(saved, "/v1/ping") {
		t.Fatalf("file = %s", saved)
	}
	ping, ok := store.FindByAPIName("ping")
	if !ok || ping.Temporary {
		t.Fatalf("rule = %+v ok=%v", ping, ok)
	}
}

func readRulesFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
