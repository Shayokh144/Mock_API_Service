package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestRefererRoutesUnprefixedPath(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Path == "/login" {
			w.Header().Set("Location", "/login")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "shop-real")
	}))
	defer upstream.Close()

	ups := upstreams.NewMemory()
	shop, err := ups.Create("SHOP_AUTH_DEV", upstream.URL, "/shop-auth")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("EasyHotel_STAGING", "http://127.0.0.1:9", "/easyhotel"); err != nil {
		t.Fatal(err)
	}
	rulesStore := rules.NewMemory()
	if _, err := rulesStore.Create(rules.Draft{
		Name: "otp", Enabled: true, Method: "GET", PathPattern: "/login/otp",
		StatusCode: 200, UpstreamID: shop.ID, Body: `{"from":"shop-rule"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rulesStore.Create(rules.Draft{
		Name: "any health", Enabled: true, Method: "GET", PathPattern: "/health",
		StatusCode: 200, Body: `{"from":"any"}`,
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(20)
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, Rules: rulesStore, History: log, Timeout: time.Second}))
	defer proxySrv.Close()
	client := noRedirectClient()

	login := getReferer(t, client, proxySrv.URL+"/login", "http://10.0.2.2:8080/shop-auth/login/otp")
	if login.StatusCode != http.StatusFound || gotPath != "/login" {
		t.Fatalf("status=%d path=%s", login.StatusCode, gotPath)
	}
	if login.Header.Get("Location") != "/shop-auth/login" {
		t.Fatalf("Location = %s", login.Header.Get("Location"))
	}
	entry := log.List()[0]
	if entry.UpstreamID != shop.ID || entry.UpstreamName != "SHOP_AUTH_DEV" || !entry.ViaReferer || entry.UpstreamLabel() != "SHOP_AUTH_DEV (via Referer)" || entry.ForwardedPath != "/login" || entry.Path != "/login" {
		t.Fatalf("history = %+v label=%s", entry, entry.UpstreamLabel())
	}

	otp := getReferer(t, client, proxySrv.URL+"/login/otp", "http://127.0.0.1:8080/shop-auth/login")
	if otp.StatusCode != http.StatusOK {
		t.Fatalf("rule status = %d", otp.StatusCode)
	}
	body, _ := io.ReadAll(otp.Body)
	otp.Body.Close()
	if string(body) != `{"from":"shop-rule"}` || gotPath != "/login" {
		t.Fatalf("rule body=%s upstream path=%s", body, gotPath)
	}
	if label := log.List()[0].UpstreamLabel(); label != "SHOP_AUTH_DEV (via Referer)" || log.List()[0].Source != history.SourceMock {
		t.Fatalf("rule history = %+v", log.List()[0])
	}

	asset := getReferer(t, client, proxySrv.URL+"/assets/application.js", "http://evil.example/shop-auth/login/otp")
	assetBody, _ := io.ReadAll(asset.Body)
	asset.Body.Close()
	if asset.StatusCode != http.StatusOK || gotPath != "/assets/application.js" || string(assetBody) != "shop-real" {
		t.Fatalf("other host status=%d path=%s body=%s", asset.StatusCode, gotPath, assetBody)
	}

	for _, referer := range []string{"", "http://10.0.2.2:8080/nope", "http://evil.example/other/page", "http://["} {
		resp := getReferer(t, client, proxySrv.URL+"/favicon.ico", referer)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(raw), noUpstreamMessage) {
			t.Fatalf("referer %q status=%d body=%s", referer, resp.StatusCode, raw)
		}
		got := log.List()[0]
		if got.Source != history.SourceRejected || got.UpstreamID != "" || got.UpstreamName != "" || got.ViaReferer {
			t.Fatalf("rejected on %q = %+v", referer, got)
		}
	}

	anyResp := getReferer(t, client, proxySrv.URL+"/health", "")
	anyBody, _ := io.ReadAll(anyResp.Body)
	anyResp.Body.Close()
	if anyResp.StatusCode != http.StatusOK || !strings.Contains(string(anyBody), `"from":"any"`) {
		t.Fatalf("any status=%d body=%s", anyResp.StatusCode, anyBody)
	}
	if got := log.List()[0]; got.Source != history.SourceMock || got.UpstreamID != "" || got.UpstreamName != "" {
		t.Fatalf("any history = %+v", got)
	}
}

func getReferer(t *testing.T, client *http.Client, rawURL, referer string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
