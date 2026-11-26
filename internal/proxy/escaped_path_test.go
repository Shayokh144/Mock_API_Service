package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func TestEncodedSlashReachesUpstreamEscaped(t *testing.T) {
	var got []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.RequestURI)
	}))
	defer upstream.Close()

	ups := upstreams.NewMemory()
	if _, err := ups.Create("files", upstream.URL+"/v1", "/app"); err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(New(Options{Upstreams: ups, Timeout: time.Second}))
	defer proxySrv.Close()

	single, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	singleSrv := httptest.NewServer(New(Options{Upstream: single, Timeout: time.Second}))
	defer singleSrv.Close()

	cases := []struct {
		base, path, want string
	}{
		{singleSrv.URL, "/files/a%2Fb/meta", "/files/a%2Fb/meta"},
		{singleSrv.URL, "/files/a%2Fb/meta?x=1", "/files/a%2Fb/meta?x=1"},
		{singleSrv.URL, "/files/plain", "/files/plain"},
		{proxySrv.URL, "/app/files/a%2Fb", "/v1/files/a%2Fb"},
		{proxySrv.URL, "/app/a%2F", "/v1/a%2F"},
		{proxySrv.URL, "/app/files/a%20b", "/v1/files/a%20b"},
		// An escaped prefix still routes. The decoded path is sent.
		{proxySrv.URL, "/%61pp/files/x", "/v1/files/x"},
	}
	for _, tc := range cases {
		got = nil
		resp, err := http.Get(tc.base + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if len(got) != 1 || got[0] != tc.want {
			t.Errorf("%s reached upstream as %v, want %s", tc.path, got, tc.want)
		}
	}
}
