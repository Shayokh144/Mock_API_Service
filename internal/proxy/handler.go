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
)

// Options configures the proxy handler.
type Options struct {
	Upstream  *url.URL
	Timeout   time.Duration
	Transport http.RoundTripper
}

// Handler is the local proxy. /_admin is never forwarded.
type Handler struct {
	upstream  *url.URL
	timeout   time.Duration
	transport http.RoundTripper
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
	}
}

// ServeHTTP reserves /_admin and forwards every other request.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isAdmin(r.URL.Path) {
		http.Error(w, "admin is not ready", http.StatusNotFound)
		return
	}
	h.forward(w, r)
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
