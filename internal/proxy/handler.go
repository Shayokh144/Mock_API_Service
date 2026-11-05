// Package proxy forwards unmatched HTTP requests to the upstream Ruby server.
package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

// Options configures the proxy handler.
type Options struct {
	Upstream  *url.URL
	Timeout   time.Duration
	Transport http.RoundTripper
	Rules     *rules.Store
}

// Handler is the local proxy. /_admin is never forwarded.
type Handler struct {
	upstream  *url.URL
	timeout   time.Duration
	transport http.RoundTripper
	rules     *rules.Store
}

// New returns a handler that forwards every non-admin request.
func New(opts Options) *Handler {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = config.DefaultUpstreamTimeout
	}
	return &Handler{
		upstream:  opts.Upstream,
		timeout:   timeout,
		transport: opts.Transport,
		rules:     opts.Rules,
	}
}

// ServeHTTP reserves /_admin, serves an enabled rule, and forwards the rest.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isAdmin(r.URL.Path) {
		http.Error(w, "admin is not ready", http.StatusNotFound)
		return
	}
	if h.rules != nil {
		if rule, ok := h.rules.Match(r.Method, r.URL.Path); ok {
			writeMock(w, rule)
			return
		}
	}
	h.forward(w, r)
}

func writeMock(w http.ResponseWriter, rule rules.Rule) {
	for name, value := range rule.Headers {
		canon := http.CanonicalHeaderKey(name)
		if canon == "X-Mock-Response" || isHopByHop(canon) {
			continue
		}
		w.Header().Set(canon, value)
	}
	body := responseBody(rule)
	if len(body) > 0 && w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	// Set this last so a saved rule cannot turn the Sentry marker off.
	w.Header().Set("X-Mock-Response", "true")
	w.WriteHeader(rule.StatusCode)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

func isHopByHop(canon string) bool {
	switch canon {
	case "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
		return true
	default:
		return false
	}
}

func responseBody(rule rules.Rule) []byte {
	if len(rule.Body) == 0 || string(rule.Body) == "null" {
		return nil
	}
	return rule.Body
}

func isAdmin(path string) bool {
	return path == "/_admin" || strings.HasPrefix(path, "/_admin/")
}

func (h *Handler) forward(w http.ResponseWriter, r *http.Request) {
	incomingPath := r.URL.Path
	incomingQuery := r.URL.RawQuery
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	outbound := r.WithContext(ctx)

	upstream := h.upstream
	reverse := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.URL.Path = joinPath(upstream.Path, incomingPath)
			req.URL.RawPath = ""
			req.URL.RawQuery = incomingQuery
			req.Host = upstream.Host
		},
		Transport: h.transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeJSONError(w, http.StatusBadGateway, "upstream unavailable")
		},
	}
	reverse.ServeHTTP(w, outbound)
}

func writeJSONError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
}
