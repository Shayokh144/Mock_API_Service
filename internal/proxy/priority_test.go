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
)

func TestHigherPriorityMockWinsAndHistoryNamesIt(t *testing.T) {
	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Name: "Exact", Enabled: true, Method: "GET", PathPattern: "/v1/items/1", StatusCode: 200, Body: `{"which":"exact"}`,
	}); err != nil {
		t.Fatal(err)
	}
	preferred, err := store.Create(rules.Draft{
		Name: "Preferred", Enabled: true, Method: "GET", PathPattern: "/v1/items/*",
		StatusCode: 201, Priority: 5, Body: `{"which":"wild"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	proxySrv := httptest.NewServer(New(Options{Rules: store, History: log, Timeout: time.Second}))
	defer proxySrv.Close()
	resp, err := http.Get(proxySrv.URL + "/v1/items/1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated || !strings.Contains(string(body), "wild") {
		t.Fatalf("response = %d %s", resp.StatusCode, body)
	}
	entries := log.List()
	if len(entries) != 1 || entries[0].RuleID != preferred.ID || entries[0].RuleName != "Preferred" {
		t.Fatalf("history = %+v", entries)
	}
}
