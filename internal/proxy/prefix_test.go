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
	var okyaPath, okyaQuery, easyPath string
	okyaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		okyaPath = r.URL.Path
		okyaQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, "okya-real")
	}))
	defer okyaSrv.Close()
	easySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		easyPath = r.URL.Path
		_, _ = io.WriteString(w, "easy-real")
	}))
	defer easySrv.Close()

	ups := upstreams.NewMemory()
	okya, err := ups.Create("OKYA", okyaSrv.URL+"/v1", "/okya")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ups.Create("easyHotel", easySrv.URL, "/easyhotel"); err != nil {
		t.Fatal(err)
	}
	rulesStore := rules.NewMemory()
	if _, err := rulesStore.Create(rules.Draft{
		Name: "okya items", Enabled: true, Method: "GET", PathPattern: "/users",
		StatusCode: 201, UpstreamID: okya.ID, Body: `{"from":"okya-rule"}`,
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

	got := getBody(t, proxySrv.URL+"/okya/users?page=2")
	if got != `{"from":"okya-rule"}` || okyaPath != "" {
		t.Fatalf("okya rule body=%s upstream path=%s", got, okyaPath)
	}
	got = getBody(t, proxySrv.URL+"/easyhotel/users?page=2")
	if got != "easy-real" || easyPath != "/users" {
		t.Fatalf("easy passthrough body=%s path=%s", got, easyPath)
	}
	got = getBody(t, proxySrv.URL+"/okya/health")
	if got != `{"from":"any"}` {
		t.Fatalf("okya any = %s", got)
	}
	got = getBody(t, proxySrv.URL+"/easyhotel/health")
	if got != `{"from":"any"}` {
		t.Fatalf("easy any = %s", got)
	}

	got = getBody(t, proxySrv.URL+"/okya/api/v1/x?page=2")
	if got != "okya-real" || okyaPath != "/v1/api/v1/x" || okyaQuery != "page=2" {
		t.Fatalf("joined path=%s query=%s body=%s", okyaPath, okyaQuery, got)
	}
	resp, err := http.Get(proxySrv.URL + "/okya")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if okyaPath != "/v1/" {
		t.Fatalf("exact prefix forwarded path = %s", okyaPath)
	}
	missEarly, err := http.Get(proxySrv.URL + "/okyax")
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
		if entry.Path == "/okya/api/v1/x" {
			found = entry
			break
		}
	}
	if found.Path != "/okya/api/v1/x" || found.ForwardedPath != "/api/v1/x" || found.RawQuery != "page=2" || found.UpstreamID != okya.ID || found.UpstreamName != "OKYA" {
		t.Fatalf("history = %+v", found)
	}
	filtered := history.FilterUpstream(log.List(), okya.ID)
	if len(filtered) == 0 {
		t.Fatal("upstream filter missed prefixed traffic")
	}
	for _, entry := range filtered {
		if entry.UpstreamID != okya.ID {
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
	still := getBody(t, proxySrv.URL+"/okya/users")
	if still != `{"from":"okya-rule"}` {
		t.Fatalf("prefix still matches = %s", still)
	}
}
