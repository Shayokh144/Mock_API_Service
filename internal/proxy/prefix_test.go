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

func TestPrefixRoutesStripQueryAndScopeRules(t *testing.T) {
	var shopPath, shopQuery, easyPath string
	shopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		shopPath = r.URL.Path
		shopQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, "shop-real")
	}))
	defer shopSrv.Close()
	easySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		easyPath = r.URL.Path
		_, _ = io.WriteString(w, "easy-real")
	}))
	defer easySrv.Close()

	ups := upstreams.NewMemory()
	shop, err := ups.Create("SHOP", shopSrv.URL+"/v1", "/shop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("easyHotel", easySrv.URL, "/easyhotel"); err != nil {
		t.Fatal(err)
	}
	rulesStore := rules.NewMemory()
	if _, err := rulesStore.Create(rules.Draft{
		Name: "shop items", Enabled: true, Method: "GET", PathPattern: "/users",
		StatusCode: 201, UpstreamID: shop.ID, Body: `{"from":"shop-rule"}`,
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
	handler := New(Options{Upstreams: ups, Rules: rulesStore, History: log, Timeout: time.Second})
	proxySrv := httptest.NewServer(handler)
	defer proxySrv.Close()

	got := getBody(t, proxySrv.URL+"/shop/users?page=2")
	if got != `{"from":"shop-rule"}` || shopPath != "" {
		t.Fatalf("shop rule body=%s upstream path=%s", got, shopPath)
	}
	got = getBody(t, proxySrv.URL+"/easyhotel/users?page=2")
	if got != "easy-real" || easyPath != "/users" {
		t.Fatalf("easy passthrough body=%s path=%s", got, easyPath)
	}
	got = getBody(t, proxySrv.URL+"/shop/health")
	if got != `{"from":"any"}` {
		t.Fatalf("shop any = %s", got)
	}
	got = getBody(t, proxySrv.URL+"/easyhotel/health")
	if got != `{"from":"any"}` {
		t.Fatalf("easy any = %s", got)
	}

	got = getBody(t, proxySrv.URL+"/shop/api/v1/x?page=2")
	if got != "shop-real" || shopPath != "/v1/api/v1/x" || shopQuery != "page=2" {
		t.Fatalf("joined path=%s query=%s body=%s", shopPath, shopQuery, got)
	}
	resp, err := http.Get(proxySrv.URL + "/shop")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if shopPath != "/v1/" {
		t.Fatalf("exact prefix forwarded path = %s", shopPath)
	}
	missEarly, err := http.Get(proxySrv.URL + "/shopx")
	if err != nil {
		t.Fatal(err)
	}
	missEarlyBody, _ := io.ReadAll(missEarly.Body)
	missEarly.Body.Close()
	if missEarly.StatusCode != http.StatusNotFound || !strings.Contains(string(missEarlyBody), noUpstreamMessage) {
		t.Fatalf("segment boundary status=%d body=%s", missEarly.StatusCode, missEarlyBody)
	}

	entries := log.List()
	var found history.Entry
	for _, entry := range entries {
		if entry.Path == "/shop/api/v1/x" {
			found = entry
			break
		}
	}
	if found.Path != "/shop/api/v1/x" || found.ForwardedPath != "/api/v1/x" || found.RawQuery != "page=2" || found.UpstreamID != shop.ID || found.UpstreamName != "SHOP" {
		t.Fatalf("history = %+v", found)
	}
	filtered := history.FilterUpstream(log.List(), shop.ID)
	if len(filtered) == 0 {
		t.Fatal("upstream filter missed prefixed traffic")
	}
	for _, entry := range filtered {
		if entry.UpstreamID != shop.ID {
			t.Fatalf("filter row = %+v", entry)
		}
	}

	miss, err := http.Get(proxySrv.URL + "/other")
	if err != nil {
		t.Fatal(err)
	}
	missBody, _ := io.ReadAll(miss.Body)
	miss.Body.Close()
	if miss.StatusCode != http.StatusNotFound || !strings.Contains(string(missBody), noUpstreamMessage) {
		t.Fatalf("unmatched status=%d body=%s", miss.StatusCode, missBody)
	}
	rejected := log.List()[0]
	if rejected.Path != "/other" || rejected.Source != history.SourceRejected || rejected.UpstreamID != "" || rejected.UpstreamName != "" || rejected.ViaReferer {
		t.Fatalf("rejected = %+v", rejected)
	}
	still := getBody(t, proxySrv.URL+"/shop/users")
	if still != `{"from":"shop-rule"}` {
		t.Fatalf("prefix still matches = %s", still)
	}
}
