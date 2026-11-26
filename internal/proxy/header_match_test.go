package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestHeaderRuleSeesPathAfterPrefix(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "real")
	}))
	defer upstream.Close()
	ups := upstreams.NewMemory()
	shop, err := ups.Create("SHOP", upstream.URL, "/shop")
	if err != nil {
		t.Fatal(err)
	}
	store := rules.NewMemory()
	headers, err := rules.ParseConditions("Authorization:absent\nAccept-Language:prefix=th", "header")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rules.Draft{
		Name: "token", Enabled: true, Method: "POST", PathPattern: "/v1/oauth/token",
		StatusCode: 401, UpstreamID: shop.ID, Body: `{"error":"unauthorized"}`,
		Match: rules.MatchSpec{Headers: headers},
	}); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, Rules: store, Timeout: time.Second}))
	defer proxySrv.Close()

	req, err := http.NewRequest(http.MethodPost, proxySrv.URL+"/shop/v1/oauth/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Language", "th-TH")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(body), "unauthorized") {
		t.Fatalf("match = %d %s", resp.StatusCode, body)
	}

	req.Header.Set("Authorization", "Bearer x")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "real" {
		t.Fatalf("miss = %d %s", resp.StatusCode, body)
	}
}
