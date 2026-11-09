package upstreams

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateActivateAndRejectBadURLs(t *testing.T) {
	store := NewMemory()
	store.now = func() time.Time {
		return time.Date(2026, 11, 5, 12, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	}
	staging, err := store.Create(" staging ", "HTTPS://staging.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if staging.Name != "staging" || staging.BaseURL != "https://staging.example.com/v1" {
		t.Fatalf("staging = %+v", staging)
	}
	if staging.CreatedAt.Location() != time.UTC || staging.CreatedAt.Format(time.RFC3339) != "2026-11-05T05:00:00Z" {
		t.Fatalf("created = %s", staging.CreatedAt)
	}
	active, ok := store.Active()
	if !ok || active.ID != staging.ID {
		t.Fatalf("active = %+v ok=%v", active, ok)
	}

	develop, err := store.Create("develop", "http://develop.example.com")
	if err != nil {
		t.Fatal(err)
	}
	active, ok = store.Active()
	if !ok || active.ID != staging.ID {
		t.Fatal("creating a second upstream changed the active one")
	}
	if err := store.Activate(develop.ID); err != nil {
		t.Fatal(err)
	}
	active, ok = store.Active()
	if !ok || active.Name != "develop" {
		t.Fatalf("active = %+v", active)
	}

	if _, err := store.Create("develop", "http://other.example.com"); err == nil {
		t.Fatal("duplicate name was accepted")
	}
	for _, raw := range []string{"", "staging.example.com", "ftp://files.example.com", "http://staging.example.com/?q=1", "http://staging.example.com/#x", "/v1"} {
		if _, err := store.Create("bad", raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if err := store.Delete(develop.ID); err != ErrActive {
		t.Fatalf("delete active = %v", err)
	}
	if err := store.Delete(staging.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(staging.ID); ok {
		t.Fatal("deleted upstream still present")
	}
	active, ok = store.Active()
	if !ok || active.ID != develop.ID {
		t.Fatal("active upstream was removed")
	}
}

func TestSeedOnlyWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upstreams.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Seed(""); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Active(); ok {
		t.Fatal("empty seed created an upstream")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("seed created a file without an upstream")
	}
	if err := store.Seed("https://staging.example.com/api"); err != nil {
		t.Fatal(err)
	}
	active, ok := store.Active()
	if !ok || active.Name != "staging.example.com" || active.BaseURL != "https://staging.example.com/api" {
		t.Fatalf("seeded = %+v ok=%v", active, ok)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if err := store.Seed("https://other.example.com"); err != nil {
		t.Fatal(err)
	}
	if got := store.List(); len(got) != 1 || got[0].BaseURL != "https://staging.example.com/api" {
		t.Fatalf("seed replaced stored upstreams: %+v", got)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	again, ok := reloaded.Active()
	if !ok || again.ID != active.ID || again.BaseURL != active.BaseURL {
		t.Fatalf("reloaded = %+v", again)
	}
}

func TestLoadRejectsBrokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upstreams.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
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

func TestUpdateKeepsIDAndActiveFlag(t *testing.T) {
	store := NewMemory()
	first, err := store.Create("staging", "https://old.example.com")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(first.ID, "staging", "https://new.example.com/v1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || updated.BaseURL != "https://new.example.com/v1" {
		t.Fatalf("updated = %+v", updated)
	}
	active, ok := store.Active()
	if !ok || active.BaseURL != "https://new.example.com/v1" {
		t.Fatalf("active = %+v", active)
	}
}

func TestSwitchIsSafeAcrossGoroutines(t *testing.T) {
	store := NewMemory()
	a, err := store.Create("a", "http://a.example")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Create("b", "http://b.example")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Activate(a.ID); err != nil {
				t.Errorf("activate a: %v", err)
			}
			if _, ok := store.Active(); !ok {
				t.Error("missing active")
			}
			if err := store.Activate(b.ID); err != nil {
				t.Errorf("activate b: %v", err)
			}
		}()
	}
	wg.Wait()
	active, ok := store.Active()
	if !ok || (active.ID != a.ID && active.ID != b.ID) {
		t.Fatalf("active = %+v ok=%v", active, ok)
	}
}
