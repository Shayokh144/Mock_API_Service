package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPriorityBeatsPathOrderAndDefaultsToZero(t *testing.T) {
	store := NewMemory()
	exact, err := store.Create(Draft{
		Name: "exact", Enabled: true, Method: "GET", PathPattern: "/v1/items/1", StatusCode: 200,
	})
	if err != nil {
		t.Fatal(err)
	}
	wild, err := store.Create(Draft{
		Name: "wild", Enabled: true, Method: "GET", PathPattern: "/v1/items/*", StatusCode: 201, Priority: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := store.Match("GET", "/v1/items/1")
	if !ok || got.ID != wild.ID {
		t.Fatalf("higher priority = %+v ok=%v", got, ok)
	}
	if _, err := store.Update(wild.ID, Draft{
		Name: "wild", Enabled: true, Method: "GET", PathPattern: "/v1/items/*", StatusCode: 201, Priority: -1,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok = store.Match("GET", "/v1/items/1")
	if !ok || got.ID != exact.ID {
		t.Fatalf("negative priority should lose to 0, got %+v", got)
	}
	if _, err := store.Update(wild.ID, Draft{
		Name: "wild", Enabled: true, Method: "GET", PathPattern: "/v1/items/*", StatusCode: 201,
	}); err != nil {
		t.Fatal(err)
	}
	got, ok = store.Match("GET", "/v1/items/1")
	if !ok || got.ID != exact.ID || got.Priority != 0 {
		t.Fatalf("equal priority should keep the exact path, got %+v", got)
	}
}

func TestMissingPriorityLoadsAsZero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rules.json")
	raw := `{"rules":[{"id":"0123456789abcdef","name":"Old","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"headers":{},"body":null,"created_at":"2026-11-05T12:00:00Z","updated_at":"2026-11-05T12:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	list := store.List()
	if len(list) != 1 || list[0].Priority != 0 {
		t.Fatalf("loaded = %+v", list)
	}
	if _, err := store.Update(list[0].ID, Draft{
		Name: "Old", Enabled: true, Method: "GET", PathPattern: "/v1/old", StatusCode: 200, Priority: -1,
	}); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(again), `"priority": -1`) {
		t.Fatalf("saved = %s", again)
	}
}
