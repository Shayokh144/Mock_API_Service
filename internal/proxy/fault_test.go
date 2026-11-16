package proxy

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestResetClosesWithoutAStatus(t *testing.T) {
	store := rules.NewMemory()
	rule, err := store.Create(rules.Draft{
		Name: "Drop", Enabled: true, Method: "GET", PathPattern: "/v1/drop",
		StatusCode: 200, Fault: "reset", DelayMS: 80, Body: `{"no":true}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	srv := httptest.NewServer(New(Options{Rules: store, History: log, Timeout: time.Second}))
	defer srv.Close()
	start := time.Now()
	resp, err := http.Get(srv.URL + "/v1/drop")
	if err == nil {
		resp.Body.Close()
		t.Fatalf("reset returned %d", resp.StatusCode)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatalf("delay did not run: %s", time.Since(start))
	}
	entries := log.List()
	if len(entries) != 1 || entries[0].Source != history.SourceFault || entries[0].Fault != "reset" || entries[0].Status != 0 || entries[0].RuleID != rule.ID || entries[0].RuleName != "Drop" || entries[0].DelayMS != 80 {
		t.Fatalf("history = %+v", entries)
	}
}

func TestHangClosesAtUpstreamTimeout(t *testing.T) {
	store := rules.NewMemory()
	rule, err := store.Create(rules.Draft{
		Name: "Stuck", Enabled: true, Method: "GET", PathPattern: "/v1/hang",
		StatusCode: 200, Fault: "hang",
	})
	if err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	srv := httptest.NewServer(New(Options{Rules: store, History: log, Timeout: 80 * time.Millisecond}))
	defer srv.Close()
	start := time.Now()
	resp, err := http.Get(srv.URL + "/v1/hang")
	elapsed := time.Since(start)
	if err == nil {
		resp.Body.Close()
		t.Fatalf("hang returned %d", resp.StatusCode)
	}
	if elapsed < 50*time.Millisecond || elapsed > time.Second {
		t.Fatalf("hang lasted %s", elapsed)
	}
	entries := log.List()
	if len(entries) != 1 || entries[0].Fault != "hang" || entries[0].RuleID != rule.ID || entries[0].Status != 0 {
		t.Fatalf("history = %+v", entries)
	}
}
