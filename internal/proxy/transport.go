package proxy

import "net/http"

const (
	// upstreamIdlePerHost is how many idle connections each upstream keeps.
	// http.DefaultTransport keeps 2, so several simulator apps calling at
	// once made the proxy open and close a connection for most requests.
	// At about 200 requests in flight that filled the macOS local port range
	// with TIME_WAIT sockets and new upstream calls failed with 502.
	upstreamIdlePerHost = 256
	// upstreamIdleTotal caps idle connections across every upstream.
	upstreamIdleTotal = 1024
)

// sharedTransport is used by every proxy handler that is not given a
// Transport, including test-session proxies, so they share one pool.
var sharedTransport = newUpstreamTransport()

// newUpstreamTransport is http.DefaultTransport with a larger idle pool.
// Proxy settings, dial and TLS timeouts, and HTTP/2 stay as the default.
func newUpstreamTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = upstreamIdleTotal
	t.MaxIdleConnsPerHost = upstreamIdlePerHost
	return t
}
