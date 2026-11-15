package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

type writeSniff struct {
	http.ResponseWriter
	wrote bool
}

func (s *writeSniff) WriteHeader(code int) {
	s.wrote = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *writeSniff) Write(p []byte) (int, error) {
	s.wrote = true
	return s.ResponseWriter.Write(p)
}

func TestRuleDelayWaitsBeforeTheResponse(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "fresh")
	}))
	defer upstream.Close()
	base, err := http.NewRequest(http.MethodGet, upstream.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Name: "slow mock", Enabled: true, Method: "GET", PathPattern: "/v1/slow",
		StatusCode: 201, DelayMS: 1500, Body: `{"slow":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(rules.Draft{
		Name: "slow fresh", Enabled: true, FreshRequest: true, Method: "GET", PathPattern: "/v1/fresh",
		StatusCode: 500, DelayMS: 250, Body: `{"unused":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	var waited []time.Duration
	var wroteDuringWait bool
	sniff := &writeSniff{ResponseWriter: httptest.NewRecorder()}
	handler := New(Options{
		Upstream: base.URL,
		Timeout:  time.Second,
		Rules:    store,
		History:  log,
	})
	handler.wait = func(ctx context.Context, d time.Duration) error {
		waited = append(waited, d)
		wroteDuringWait = sniff.wrote
		return nil
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/slow", nil)
	handler.ServeHTTP(sniff, req)
	if !sniff.wrote || sniff.ResponseWriter.(*httptest.ResponseRecorder).Code != http.StatusCreated {
		t.Fatalf("mock write=%v code=%d", sniff.wrote, sniff.ResponseWriter.(*httptest.ResponseRecorder).Code)
	}
	if wroteDuringWait || len(waited) != 1 || waited[0] != 1500*time.Millisecond {
		t.Fatalf("waited=%v wroteDuring=%v", waited, wroteDuringWait)
	}
	entries := log.List()
	if len(entries) != 1 || entries[0].DelayMS != 1500 || entries[0].Source != history.SourceMock {
		t.Fatalf("history = %+v", entries)
	}

	freshSniff := &writeSniff{ResponseWriter: httptest.NewRecorder()}
	handler.wait = func(ctx context.Context, d time.Duration) error {
		if hits.Load() != 0 {
			t.Errorf("upstream ran before the delay")
		}
		if d != 250*time.Millisecond {
			t.Errorf("fresh delay = %s", d)
		}
		return nil
	}
	freshReq := httptest.NewRequest(http.MethodGet, "/v1/fresh", nil)
	handler.ServeHTTP(freshSniff, freshReq)
	if hits.Load() != 1 || freshSniff.ResponseWriter.(*httptest.ResponseRecorder).Body.String() != "fresh" {
		t.Fatalf("hits=%d body=%s", hits.Load(), freshSniff.ResponseWriter.(*httptest.ResponseRecorder).Body.String())
	}
	entries = log.List()
	if entries[0].Path != "/v1/fresh" || entries[0].DelayMS != 250 {
		t.Fatalf("fresh history = %+v", entries[0])
	}
}

func TestDelayStopsWhenTheClientCancels(t *testing.T) {
	store := rules.NewMemory()
	if _, err := store.Create(rules.Draft{
		Name: "slow", Enabled: true, Method: "GET", PathPattern: "/v1/slow",
		StatusCode: 200, DelayMS: 5000, Body: `{"slow":true}`,
	}); err != nil {
		t.Fatal(err)
	}
	log := history.New(10)
	handler := New(Options{Rules: store, History: log, Timeout: time.Second})
	started := make(chan struct{})
	handler.wait = func(ctx context.Context, d time.Duration) error {
		if d != 5*time.Second {
			t.Errorf("delay = %s", d)
		}
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/slow", nil)
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	sniff := &writeSniff{ResponseWriter: httptest.NewRecorder()}
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(sniff, req)
		close(done)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delay did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after cancel")
	}
	if sniff.wrote {
		t.Fatal("cancelled delay still wrote a response")
	}
	if len(log.List()) != 0 {
		t.Fatalf("history = %+v", log.List())
	}
}
