package sessions

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTestSessionUsesConfiguredBodyCap(t *testing.T) {
	manager := New(Options{
		ProxyAddr: "127.0.0.1:1", AdminAddr: "127.0.0.1:2",
		HistoryLimit: 5, MaxBody: 4, Timeout: time.Second,
	})
	defer manager.Close()
	info, err := manager.Create("cap", 0, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(info.ProxyURL+"/v1/in", "text/plain", strings.NewReader("12345"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
