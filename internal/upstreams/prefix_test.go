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
	shop, err := store.Create("shop", "https://shop.example", " /shop ")
	if err != nil {
		t.Fatal(err)
	}
	if shop.Prefix != "/shop" {
		t.Fatalf("prefix = %q", shop.Prefix)
	}
	otherCase, err := store.Create("shop-upper", "https://shop.example/v2", "/Shop")
	if err != nil {
		t.Fatal(err)
	}
	if otherCase.Prefix != "/Shop" {
		t.Fatalf("case = %q", otherCase.Prefix)
	}
	if _, err := store.Create("again", "https://again.example", "/shop"); err == nil || !strings.Contains(err.Error(), "/shop") {
		t.Fatalf("duplicate = %v", err)
	}
	updated, err := store.Update(shop.ID, "shop", "https://shop.example", "/shop")
	if err != nil || updated.Prefix != "/shop" {
		t.Fatalf("same prefix update = %+v %v", updated, err)
	}
	if _, err := store.Update(otherCase.ID, "shop-upper", "https://shop.example/v2", "/shop"); err == nil {
		t.Fatal("update stole a prefix")
	}
	if _, err := store.Create("plain", "https://plain.example", ""); err == nil || !strings.Contains(err.Error(), "prefix is required") {
		t.Fatalf("empty prefix = %v", err)
	}
	if _, err := store.Create("plain-two", "https://plain.example/2", "  "); err == nil {
		t.Fatal("blank prefix was accepted")
	}

	bad := []string{"shop", "/", "/shop/", "/shop/api", "/ok ya", "/shop?", "/_admin", "/_admin/extra", "/_adminx", "/shop.", "/café"}
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
	shop, err := store.Create("shop", "https://shop.example/v1", "/shop")
	if err != nil {
		t.Fatal(err)
	}
	easy, err := store.Create("easy", "https://easy.example", "/easyhotel")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path      string
		id        string
		forwarded string
		prefixed  bool
		ok        bool
	}{
		{"/shop/api/v1/x", shop.ID, "/api/v1/x", true, true},
		{"/shop", shop.ID, "/", true, true},
		{"/shop/", shop.ID, "/", true, true},
		{"/easyhotel/v1/rooms", easy.ID, "/v1/rooms", true, true},
	}
	for _, tc := range cases {
		route, ok := store.Route(tc.path)
		if ok != tc.ok || route.Upstream.ID != tc.id || route.ForwardedPath != tc.forwarded || route.Prefixed != tc.prefixed || route.ViaReferer {
			t.Fatalf("path %q route=%+v ok=%v", tc.path, route, ok)
		}
	}
	for _, path := range []string{"/shopx", "/shopX", "/other", ""} {
		if _, ok := store.Route(path); ok {
			t.Fatalf("unmatched path %q routed", path)
		}
	}
}

func TestRefererMatchesPrefixAndIgnoresHost(t *testing.T) {
	store := NewMemory()
	shop, err := store.Create("SHOP_AUTH_DEV", "https://shop.example", "/shop-auth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("easy", "https://easy.example", "/easyhotel"); err != nil {
		t.Fatal(err)
	}

	route, ok := store.MatchReferer("/login", "http://10.0.2.2:8080/shop-auth/login/otp")
	if !ok || route.Upstream.ID != shop.ID || route.ForwardedPath != "/login" || route.Prefixed || !route.ViaReferer {
		t.Fatalf("emulator referer = %+v ok=%v", route, ok)
	}
	route, ok = store.MatchReferer("/favicon.ico", "http://evil.example/shop-auth/login/otp")
	if !ok || route.Upstream.ID != shop.ID || route.ForwardedPath != "/favicon.ico" {
		t.Fatalf("other host with matching path = %+v ok=%v", route, ok)
	}
	route, ok = store.MatchReferer("/assets/app.js", "http://127.0.0.1:8080/shop-auth")
	if !ok || route.ForwardedPath != "/assets/app.js" || route.Upstream.Prefix != "/shop-auth" {
		t.Fatalf("exact prefix referer = %+v ok=%v", route, ok)
	}

	for _, referer := range []string{
		"",
		"http://10.0.2.2:8080/favicon.ico",
		"http://evil.example/unrelated/page",
		"http://[",
		"not a url",
		"http://127.0.0.1:8080/shop-auth-extra/login",
	} {
		if _, ok := store.MatchReferer("/login", referer); ok {
			t.Fatalf("referer %q routed", referer)
		}
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
	if !ok || item.Prefix != "/staging" || item.Name != "staging" {
		t.Fatalf("loaded = %+v ok=%v", item, ok)
	}
	assigned := store.AssignedPrefixes()
	if len(assigned) != 1 || assigned[0].Name != "staging" || assigned[0].Prefix != "/staging" {
		t.Fatalf("assigned = %+v", assigned)
	}
	if got := PrefixWarning(assigned[0].Name, assigned[0].Prefix, "127.0.0.1:8080"); got != "warning: upstream staging had no prefix; assigned /staging. App base URL is http://127.0.0.1:8080/staging" {
		t.Fatalf("warning = %q", got)
	}
	if _, ok := store.Route("/v1/health"); ok {
		t.Fatal("unprefixed path still routed")
	}
	route, ok := store.Route("/staging/v1/health")
	if !ok || !route.Prefixed || route.ForwardedPath != "/v1/health" || route.Upstream.ID != item.ID {
		t.Fatalf("route = %+v ok=%v", route, ok)
	}
	saved := mustRead(t, path)
	if !strings.Contains(saved, `"/staging"`) || strings.Contains(saved, "active_id") {
		t.Fatalf("file = %s", saved)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.AssignedPrefixes()) != 0 {
		t.Fatal("assigned the prefix again on the next load")
	}
	if item.CreatedAt.Location() != time.UTC {
		t.Fatalf("created location = %s", item.CreatedAt.Location())
	}
}

func TestLoadAssignsPrefixFromName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "upstreams.json")
	raw := `{
	  "active_id": "does-not-matter",
	  "upstreams": [
	    {
	      "id": "0123456789abcdef",
	      "name": "EasyHotel_STAGING",
	      "base_url": "https://easy.example",
	      "created_at": "2026-11-05T05:00:00Z",
	      "updated_at": "2026-11-05T05:00:00Z"
	    },
	    {
	      "id": "fedcba9876543210",
	      "name": "EasyHotel-STAGING",
	      "base_url": "https://easy-2.example",
	      "created_at": "2026-11-05T05:00:00Z",
	      "updated_at": "2026-11-05T05:00:00Z"
	    },
	    {
	      "id": "0011223344556677",
	      "name": "kept",
	      "base_url": "https://kept.example",
	      "prefix": "/easyhotel-staging-2",
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
	first, ok := store.Get("0123456789abcdef")
	if !ok || first.Prefix != "/easyhotel-staging" {
		t.Fatalf("first = %+v ok=%v", first, ok)
	}
	second, ok := store.Get("fedcba9876543210")
	if !ok || second.Prefix != "/easyhotel-staging-3" {
		t.Fatalf("second = %+v ok=%v", second, ok)
	}
	kept, ok := store.Get("0011223344556677")
	if !ok || kept.Prefix != "/easyhotel-staging-2" {
		t.Fatalf("kept = %+v ok=%v", kept, ok)
	}
	assigned := store.AssignedPrefixes()
	if len(assigned) != 2 || assigned[0].Prefix != "/easyhotel-staging" || assigned[1].Prefix != "/easyhotel-staging-3" {
		t.Fatalf("assigned = %+v", assigned)
	}
	warning := PrefixWarning(first.Name, first.Prefix, "[::1]:8080")
	if warning != "warning: upstream EasyHotel_STAGING had no prefix; assigned /easyhotel-staging. App base URL is http://[::1]:8080/easyhotel-staging" {
		t.Fatalf("warning = %q", warning)
	}
}
