package proxy

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Several apps calling at once must reuse upstream connections. With the
// default 2 idle connections per host, most of these requests opened a new one.
func TestConcurrentPassthroughReusesUpstreamConnections(t *testing.T) {
	var opened atomic.Int64
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	upstream.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	upstream.Start()
	defer upstream.Close()
	base, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstream: base, Timeout: 5 * time.Second, Transport: newUpstreamTransport()}))
	defer proxySrv.Close()

	const workers, rounds = 20, 15
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: workers}}
	var wg sync.WaitGroup
	var failed atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				resp, err := client.Get(proxySrv.URL + "/v1/items")
				if err != nil {
					failed.Add(1)
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					failed.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if failed.Load() != 0 {
		t.Fatalf("%d requests failed", failed.Load())
	}
	// Each worker needs at most one upstream connection at a time.
	if got := opened.Load(); got > 2*workers {
		t.Fatalf("proxy opened %d upstream connections for %d requests from %d workers", got, workers*rounds, workers)
	}
}

func TestNewUsesSharedPoolByDefault(t *testing.T) {
	a := New(Options{})
	b := New(Options{})
	if a.transport != sharedTransport || b.transport != sharedTransport {
		t.Fatal("handlers without a Transport do not share the upstream pool")
	}
	custom := http.DefaultTransport
	if New(Options{Transport: custom}).transport != custom {
		t.Fatal("an explicit Transport was replaced")
	}
	if sharedTransport.MaxIdleConnsPerHost != upstreamIdlePerHost {
		t.Fatalf("idle per host = %d", sharedTransport.MaxIdleConnsPerHost)
	}
}
