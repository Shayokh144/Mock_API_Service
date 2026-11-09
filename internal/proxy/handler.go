// Package proxy forwards unmatched HTTP requests to the upstream server.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

const noUpstreamMessage = "no upstream is configured; add one at /_admin"

var errResponseTooLarge = errors.New("upstream response too large")

// Options configures the proxy handler.
type Options struct {
	Upstream  *url.URL
	Upstreams *upstreams.Store
	Timeout   time.Duration
	Transport http.RoundTripper
	Rules     *rules.Store
	History   *history.Log
	Admin     http.Handler
}

// Handler is the local proxy. /_admin is never forwarded.
type Handler struct {
	upstream  *url.URL
	upstreams *upstreams.Store
	timeout   time.Duration
	transport http.RoundTripper
	rules     *rules.Store
	history   *history.Log
	admin     http.Handler
}

// New returns a handler that forwards every non-admin request.
func New(opts Options) *Handler {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = config.DefaultUpstreamTimeout
	}
	return &Handler{
		upstream:  opts.Upstream,
		upstreams: opts.Upstreams,
		timeout:   timeout,
		transport: opts.Transport,
		rules:     opts.Rules,
		history:   opts.History,
		admin:     opts.Admin,
	}
}

// ServeHTTP reserves /_admin, serves an enabled rule, and forwards the rest.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isAdmin(r.URL.Path) {
		if h.admin != nil {
			h.admin.ServeHTTP(w, r)
			return
		}
		http.Error(w, "admin is not ready", http.StatusNotFound)
		return
	}
	h.serveAPI(w, r)
}

func (h *Handler) serveAPI(w http.ResponseWriter, r *http.Request) {
	started := time.Now().UTC()
	reqHeaders := r.Header.Clone()
	payload, err := readUpTo(r.Body, config.MaxBufferedBody)
	if r.Body != nil {
		_ = r.Body.Close()
	}
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			h.finish(w, r, started, reqHeaders, payload, history.SourceRejected, "", "", http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		h.finish(w, r, started, reqHeaders, nil, history.SourceRejected, "", "", http.StatusBadRequest, "could not read request body")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))
	r.ContentLength = int64(len(payload))
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	activeName, activeID, dest, hasTarget := h.activeTarget()
	capw := &bodyCapture{ResponseWriter: w}
	if h.rules != nil {
		if rule, ok := h.rules.MatchActive(r.Method, r.URL.Path, activeID); ok {
			if rule.FreshRequest {
				if !hasTarget {
					h.unavailable(capw, started, r, reqHeaders, payload, rule.ID)
					return
				}
				prepareFreshRequest(r.Header)
				failed := h.forward(capw, r, dest)
				source := history.SourceUpstream
				if failed {
					source = history.SourceUpstreamError
				}
				h.record(started, r, reqHeaders, payload, capw, source, rule.ID, activeName)
				return
			}
			writeMock(capw, rule)
			h.record(started, r, reqHeaders, payload, capw, history.SourceMock, rule.ID, h.mockUpstreamName(rule))
			return
		}
	}
	if !hasTarget {
		h.unavailable(capw, started, r, reqHeaders, payload, "")
		return
	}
	failed := h.forward(capw, r, dest)
	source := history.SourceUpstream
	if failed {
		source = history.SourceUpstreamError
	}
	h.record(started, r, reqHeaders, payload, capw, source, "", activeName)
}

func (h *Handler) unavailable(w *bodyCapture, started time.Time, r *http.Request, reqHeaders http.Header, payload []byte, ruleID string) {
	writeJSONError(w, http.StatusServiceUnavailable, noUpstreamMessage)
	h.record(started, r, reqHeaders, payload, w, history.SourceUpstreamError, ruleID, "none")
}

func (h *Handler) finish(w http.ResponseWriter, r *http.Request, started time.Time, reqHeaders http.Header, payload []byte, source history.Source, ruleID, upstreamName string, status int, reason string) {
	capw := &bodyCapture{ResponseWriter: w}
	writeJSONError(capw, status, reason)
	h.record(started, r, reqHeaders, payload, capw, source, ruleID, upstreamName)
}

func (h *Handler) activeTarget() (name, id string, dest *url.URL, ok bool) {
	if h.upstreams != nil {
		item, found := h.upstreams.Active()
		if !found {
			return "none", "", nil, false
		}
		parsed, err := url.Parse(item.BaseURL)
		if err != nil || parsed.Host == "" {
			return "none", item.ID, nil, false
		}
		return item.Name, item.ID, parsed, true
	}
	if h.upstream != nil {
		return "", "", h.upstream, true
	}
	return "none", "", nil, false
}

func (h *Handler) mockUpstreamName(rule rules.Rule) string {
	if rule.UpstreamID == "" {
		return "any"
	}
	if h.upstreams != nil {
		if item, ok := h.upstreams.Get(rule.UpstreamID); ok {
			return item.Name
		}
	}
	return "unknown"
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

func (h *Handler) forward(w http.ResponseWriter, r *http.Request, upstream *url.URL) bool {
	incomingPath := r.URL.Path
	incomingQuery := r.URL.RawQuery
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	outbound := r.WithContext(ctx)

	failed := false
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
		ModifyResponse: func(resp *http.Response) error {
			body, err := readUpTo(resp.Body, config.MaxBufferedBody)
			if resp.Body != nil {
				_ = resp.Body.Close()
			}
			if errors.Is(err, errBodyTooLarge) {
				return errResponseTooLarge
			}
			if err != nil {
				return err
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
			resp.ContentLength = int64(len(body))
			resp.Header.Del("Content-Length")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			failed = true
			reason := "upstream unavailable"
			if errors.Is(err, errResponseTooLarge) {
				reason = "upstream response too large"
			}
			writeJSONError(w, http.StatusBadGateway, reason)
		},
	}
	reverse.ServeHTTP(w, outbound)
	return failed
}

func (h *Handler) record(started time.Time, r *http.Request, reqHeaders http.Header, payload []byte, capw *bodyCapture, source history.Source, ruleID, upstreamName string) {
	if h.history == nil {
		return
	}
	status, respHeaders, respBody, respTrunc := capw.snapshot()
	reqBody, reqTrunc := preview(payload)
	h.history.Add(history.Entry{
		StartedAt:             started,
		Method:                r.Method,
		Path:                  r.URL.Path,
		RawQuery:              r.URL.RawQuery,
		RequestHeaders:        reqHeaders,
		RequestBody:           reqBody,
		Status:                status,
		ResponseHeaders:       respHeaders,
		ResponseBody:          respBody,
		Source:                source,
		RuleID:                ruleID,
		UpstreamName:          upstreamName,
		RequestBodyTruncated:  reqTrunc,
		ResponseBodyTruncated: respTrunc,
	})
}

func writeJSONError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
}
