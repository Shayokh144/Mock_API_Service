package upstreams

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrefixValidationAndUniqueness(t *testing.T) {
	store := NewMemory()
	okya, err := store.Create("okya", "https://okya.example", " /okya ")
	if err != nil {
		t.Fatal(err)
	}
	if okya.Prefix != "/okya" {
		t.Fatalf("prefix = %q", okya.Prefix)
	}
	otherCase, err := store.Create("okya-upper", "https://okya.example/v2", "/Okya")
	if err != nil {
		t.Fatal(err)
	}
	if otherCase.Prefix != "/Okya" {
		t.Fatalf("case = %q", otherCase.Prefix)
	}
	if _, err := store.Create("again", "https://again.example", "/okya"); err == nil || !strings.Contains(err.Error(), "/okya") {
		t.Fatalf("duplicate = %v", err)
	}
	updated, err := store.Update(okya.ID, "okya", "https://okya.example", "/okya")
	if err != nil || updated.Prefix != "/okya" {
		t.Fatalf("same prefix update = %+v %v", updated, err)
	}
	if _, err := store.Update(otherCase.ID, "okya-upper", "https://okya.example/v2", "/okya"); err == nil {
		t.Fatal("update stole a prefix")
	}
	empty, err := store.Create("plain", "https://plain.example", "")
	if err != nil || empty.Prefix != "" {
		t.Fatalf("empty prefix = %+v %v", empty, err)
	}
	secondEmpty, err := store.Create("plain-two", "https://plain.example/2", "  ")
	if err != nil || secondEmpty.Prefix != "" {
		t.Fatalf("second empty = %+v %v", secondEmpty, err)
	}

	bad := []string{"okya", "/", "/okya/", "/okya/api", "/ok ya", "/okya?", "/_admin", "/_admin/extra", "/_adminx", "/okya.", "/café"}
	for _, prefix := range bad {
		if _, err := store.Create("bad-"+prefix, "https://bad.example", prefix); err == nil {
			t.Fatalf("accepted %q", prefix)
		}
	}
	good, err := store.Create("hyphen", "https://hyphen.example", "/ok-ya_1")
	if err != nil || good.Prefix != "/ok-ya_1" {
		t.Fatalf("good = %+v %v", good, err)
	}
}

func TestRouteStripsOnSegmentBoundary(t *testing.T) {
	store := NewMemory()
	okya, err := store.Create("okya", "https://okya.example/v1", "/okya")
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := store.Create("easy", "https://easy.example", "/easyhotel")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Activate(fallback.ID); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path      string
		id        string
		forwarded string
		prefixed  bool
	}{
		{"/okya/api/v1/x", okya.ID, "/api/v1/x", true},
		{"/okya", okya.ID, "/", true},
		{"/okya/", okya.ID, "/", true},
		{"/okyax", fallback.ID, "/okyax", false},
		{"/okyaX", fallback.ID, "/okyaX", false},
		{"/easyhotel/v1/rooms", fallback.ID, "/v1/rooms", true},
		{"/other", fallback.ID, "/other", false},
		{"", fallback.ID, "/", false},
	}
	for _, tc := range cases {
		route, ok := store.Route(tc.path)
		if !ok || route.Upstream.ID != tc.id || route.ForwardedPath != tc.forwarded || route.Prefixed != tc.prefixed {
			t.Fatalf("path %q route=%+v ok=%v", tc.path, route, ok)
		}
	}

	store.ClearActiveForTest()
	if _, ok := store.Route("/other"); ok {
		t.Fatal("unmatched path routed with no active upstream")
	}
	route, ok := store.Route("/okya/api/v1/x")
	if !ok || route.Upstream.ID != okya.ID || route.ForwardedPath != "/api/v1/x" {
		t.Fatalf("prefix still routes: %+v ok=%v", route, ok)
	}
}

func TestLoadOldUpstreamsFileWithoutPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upstreams.json")
	raw := `{
	  "active_id": "0123456789abcdef",
	  "upstreams": [
	    {
	      "id": "0123456789abcdef",
	      "name": "staging",
	      "base_url": "https://staging.example.com",
	      "created_at": "2026-11-05T05:00:00Z",
	      "updated_at": "2026-11-05T05:00:00Z"
	    }
	  ]
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	item, ok := store.Get("0123456789abcdef")
	if !ok || item.Prefix != "" || item.Name != "staging" {
		t.Fatalf("loaded = %+v ok=%v", item, ok)
	}
	route, ok := store.Route("/v1/health")
	if !ok || route.Prefixed || route.ForwardedPath != "/v1/health" || route.Upstream.ID != item.ID {
		t.Fatalf("route = %+v ok=%v", route, ok)
	}
	if item.CreatedAt.Location() != time.UTC {
		t.Fatalf("created location = %s", item.CreatedAt.Location())
	}
}
