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

func TestSequenceAndMaxUsesOnTheProxy(t *testing.T) {
	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Name: "Seq", Enabled: true, Method: "GET", PathPattern: "/v1/seq", StatusCode: 200, MaxUses: 2,
		Responses: []rules.ResponseStep{
			{StatusCode: 200, Body: []byte(`{"n":1}`)},
			{StatusCode: 201, Body: []byte(`{"n":2}`)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(5)
	srv := httptest.NewServer(New(Options{Rules: store, History: log, Timeout: time.Second}))
	defer srv.Close()
	first := readProxyBody(t, srv.URL+"/v1/seq")
	second := readProxyBody(t, srv.URL+"/v1/seq")
	third, err := http.Get(srv.URL + "/v1/seq")
	if err != nil {
		t.Fatal(err)
	}
	third.Body.Close()
	if !strings.Contains(first, `"n":1`) || !strings.Contains(second, `"n":2`) {
		t.Fatalf("bodies %s %s", first, second)
	}
	if third.StatusCode == http.StatusOK {
		t.Fatalf("third still mocked: %d", third.StatusCode)
	}
	var steps []int
	for _, entry := range log.List() {
		if entry.Source == history.SourceMock {
			steps = append(steps, entry.Step)
		}
	}
	if len(steps) != 2 || steps[0] != 2 || steps[1] != 1 {
		t.Fatalf("steps = %v", steps)
	}
}

func readProxyBody(t *testing.T, rawURL string) string {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("%s = %d %s", rawURL, resp.StatusCode, body)
	}
	return string(body)
}
