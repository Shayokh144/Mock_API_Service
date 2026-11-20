package upstreams

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateRequiresPrefixAndRejectsBadURLs(t *testing.T) {
	store := NewMemory()
	store.now = func() time.Time {
		return time.Date(2026, 11, 5, 12, 0, 0, 0, time.FixedZone("ICT", 7*3600))
	}
	staging, err := store.Create(" staging ", "HTTPS://staging.example.com/v1", "/staging")
	if err != nil {
		t.Fatal(err)
	}
	if staging.Name != "staging" || staging.BaseURL != "https://staging.example.com/v1" || staging.Prefix != "/staging" {
		t.Fatalf("staging = %+v", staging)
	}
	if staging.CreatedAt.Location() != time.UTC || staging.CreatedAt.Format(time.RFC3339) != "2026-11-05T05:00:00Z" {
		t.Fatalf("created = %s", staging.CreatedAt)
	}

	develop, err := store.Create("develop", "http://develop.example.com", "/develop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("develop", "http://other.example.com", "/other"); err == nil {
		t.Fatal("duplicate name was accepted")
	}
	if _, err := store.Create("plain", "https://plain.example", ""); err == nil || !strings.Contains(err.Error(), "prefix is required") {
		t.Fatalf("empty prefix = %v", err)
	}
	if _, err := store.Create("spaces", "https://plain.example", "  "); err == nil {
		t.Fatal("blank prefix was accepted")
	}
	for _, raw := range []string{"", "staging.example.com", "ftp://files.example.com", "http://staging.example.com/?q=1", "http://staging.example.com/#x", "/v1"} {
		if _, err := store.Create("bad", raw, "/bad"); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if err := store.Delete(develop.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get(develop.ID); ok {
		t.Fatal("deleted upstream still present")
	}
	if err := store.Delete(staging.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 {
		t.Fatal("store was not emptied")
	}
}

func TestSeedOnlyWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upstreams.json")
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Seed("", ""); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 {
		t.Fatal("empty seed created an upstream")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("seed created a file without an upstream")
	}
	if err := store.Seed("https://staging.example.com/api", ""); err != nil {
		t.Fatal(err)
	}
	seeded := store.List()
	if len(seeded) != 1 || seeded[0].Name != "staging.example.com" || seeded[0].BaseURL != "https://staging.example.com/api" || seeded[0].Prefix != "/app" {
		t.Fatalf("seeded = %+v", seeded)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	if err := store.Seed("https://other.example.com", "/other"); err != nil {
		t.Fatal(err)
	}
	if got := store.List(); len(got) != 1 || got[0].BaseURL != "https://staging.example.com/api" || got[0].Prefix != "/app" {
		t.Fatalf("seed replaced stored upstreams: %+v", got)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	again := reloaded.List()
	if len(again) != 1 || again[0].ID != seeded[0].ID || again[0].BaseURL != seeded[0].BaseURL || again[0].Prefix != "/app" {
		t.Fatalf("reloaded = %+v", again)
	}
	if strings.Contains(mustRead(t, path), "active_id") {
		t.Fatal("seed wrote active_id")
	}
}

func TestSeedUsesPrefix(t *testing.T) {
	store := NewMemory()
	if err := store.Seed("https://staging.example.com", "/easyhotel"); err != nil {
		t.Fatal(err)
	}
	got := store.List()
	if len(got) != 1 || got[0].Prefix != "/easyhotel" {
		t.Fatalf("seeded = %+v", got)
	}
	if _, ok := store.Route("/easyhotel/v1/rooms"); !ok {
		t.Fatal("seeded prefix did not route")
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

func TestUpdateKeepsID(t *testing.T) {
	store := NewMemory()
	first, err := store.Create("staging", "https://old.example.com", "/staging")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.Update(first.ID, "staging", "https://new.example.com/v1", "/staging")
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || updated.BaseURL != "https://new.example.com/v1" || updated.Prefix != "/staging" {
		t.Fatalf("updated = %+v", updated)
	}
	if _, err := store.Update(first.ID, "staging", "https://new.example.com/v1", ""); err == nil {
		t.Fatal("update cleared the prefix")
	}
}

func TestRouteIsSafeAcrossGoroutines(t *testing.T) {
	store := NewMemory()
	a, err := store.Create("a", "http://a.example", "/a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Create("b", "http://b.example", "/b")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			route, ok := store.Route("/a/x")
			if !ok || route.Upstream.ID != a.ID || route.ForwardedPath != "/x" || !route.Prefixed {
				t.Errorf("a = %+v ok=%v", route, ok)
			}
			if _, ok := store.Route("/missing"); ok {
				t.Error("missing path routed")
			}
			route, ok = store.MatchReferer("/login", "http://10.0.2.2:8080/b/otp")
			if !ok || route.Upstream.ID != b.ID || route.ForwardedPath != "/login" || route.Prefixed || !route.ViaReferer {
				t.Errorf("referer = %+v ok=%v", route, ok)
			}
		}()
	}
	wg.Wait()
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
