// Package proxy forwards unmatched HTTP requests to the upstream server.
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

const noUpstreamMessage = "no upstream matches this path"

const hostRejectedMessage = "this Host is not allowed on the proxy; add it to PROXY_ALLOWED_HOSTS"

var errResponseTooLarge = errors.New("upstream response too large")

// Options configures the proxy handler.
type Options struct {
	Upstream  *url.URL
	Upstreams *upstreams.Store
	Timeout   time.Duration
	// Transport sends upstream requests. Nil uses one shared pool that keeps
	// up to 256 idle connections per upstream host.
	Transport http.RoundTripper
	Rules     *rules.Store
	History   *history.Log
	// MaxBody is the proxy buffer for request bodies and fully buffered upstream
	// responses. Zero uses config.MaxBufferedBody. It does not change rule bodies.
	MaxBody int
	// HostAllowed checks the request Host. A request it refuses gets 403 and
	// is recorded as rejected. Nil allows every Host. See LocalHosts.
	HostAllowed func(host string) bool
}

// Handler is the local proxy. /_admin is never forwarded.
type Handler struct {
	upstream  *url.URL
	upstreams *upstreams.Store
	timeout   time.Duration
	transport http.RoundTripper
	rules     *rules.Store
	history   *history.Log
	maxBody   int
	wait      func(context.Context, time.Duration) error
	hostOK    func(string) bool
}

// New returns a handler that forwards every non-admin request.
func New(opts Options) *Handler {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = config.DefaultUpstreamTimeout
	}
	maxBody := opts.MaxBody
	if maxBody <= 0 {
		maxBody = config.MaxBufferedBody
	}
	transport := opts.Transport
	if transport == nil {
		transport = sharedTransport
	}
	return &Handler{
		upstream:  opts.Upstream,
		upstreams: opts.Upstreams,
		timeout:   timeout,
		transport: transport,
		rules:     opts.Rules,
		history:   opts.History,
		maxBody:   maxBody,
		hostOK:    opts.HostAllowed,
	}
}

// ServeHTTP reserves /_admin, serves an enabled rule, and forwards the rest.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.hostOK != nil && !h.hostOK(r.Host) {
		if r.Body != nil {
			_ = r.Body.Close()
		}
		h.finish(w, r, time.Now().UTC(), r.Header.Clone(), nil, history.SourceRejected, "", "", "", http.StatusForbidden, hostRejectedMessage)
		return
	}
	if isAdmin(r.URL.Path) {
		http.Error(w, "the dashboard is not served on the proxy port", http.StatusNotFound)
		return
	}
	if isAPI(r.URL.Path) {
		http.Error(w, "the API is not served on the proxy port", http.StatusNotFound)
		return
	}
	h.serveAPI(w, r)
}

func (h *Handler) serveAPI(w http.ResponseWriter, r *http.Request) {
	started := time.Now().UTC()
	reqHeaders := r.Header.Clone()
	payload, err := readUpTo(r.Body, h.maxBody)
	if r.Body != nil {
		_ = r.Body.Close()
	}
	if err != nil {
		if errors.Is(err, errBodyTooLarge) {
			h.finish(w, r, started, reqHeaders, payload, history.SourceRejected, "", "", "", http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		h.finish(w, r, started, reqHeaders, nil, history.SourceRejected, "", "", "", http.StatusBadRequest, "could not read request body")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(payload))
	r.ContentLength = int64(len(payload))
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}

	t := h.resolve(r)
	name, id, forwarded, dest, hasTarget := t.name, t.id, t.forwarded, t.dest, t.ok
	capw := &bodyCapture{ResponseWriter: w, limit: h.maxBody}
	if id != "" {
		setStickyUpstream(capw, r, id)
	}
	if h.rules != nil {
		if rule, ok := h.rules.MatchHTTP(r.Method, forwarded, id, r.URL.RawQuery, r.Header, payload); ok {
			capw.step = rule.AppliedStep
			if err := h.pause(r.Context(), rule.DelayMS); err != nil {
				capw.unwritten = true
				h.record(started, r, reqHeaders, payload, capw, history.SourceRejected, rule.ID, id, name, forwarded, rule.DelayMS, t.via)
				return
			}
			if rule.Fault != "" {
				h.waitFault(capw, r, rule.Fault)
				h.record(started, r, reqHeaders, payload, capw, history.SourceFault, rule.ID, id, name, forwarded, rule.DelayMS, t.via)
				h.closeFault(capw)
				return
			}
			if rule.FreshRequest {
				if !hasTarget {
					h.rejectUnmatched(capw, started, r, reqHeaders, payload, rule.ID, forwarded, rule.DelayMS)
					return
				}
				prepareFreshRequest(r.Header)
				failed := h.forward(capw, r, dest, t)
				source := history.SourceUpstream
				if failed {
					source = history.SourceUpstreamError
				}
				h.record(started, r, reqHeaders, payload, capw, source, rule.ID, id, name, forwarded, rule.DelayMS, t.via)
				return
			}
			writeMock(capw, rule)
			h.record(started, r, reqHeaders, payload, capw, history.SourceMock, rule.ID, id, name, forwarded, rule.DelayMS, t.via)
			return
		}
	}
	if !hasTarget {
		h.rejectUnmatched(capw, started, r, reqHeaders, payload, "", forwarded, 0)
		return
	}
	failed := h.forward(capw, r, dest, t)
	source := history.SourceUpstream
	if failed {
		source = history.SourceUpstreamError
	}
	h.record(started, r, reqHeaders, payload, capw, source, "", id, name, forwarded, 0, t.via)
}

func (h *Handler) waitFault(w *bodyCapture, r *http.Request, fault string) {
	w.fault = fault
	if fault != "hang" {
		return
	}
	timer := time.NewTimer(h.timeout)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
	case <-timer.C:
	}
}

func (h *Handler) closeFault(w *bodyCapture) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	_ = conn.Close()
}

func (h *Handler) pause(ctx context.Context, delayMS int) error {
	if delayMS <= 0 {
		return nil
	}
	if int64(delayMS) > int64(math.MaxInt64)/int64(time.Millisecond) {
		return fmt.Errorf("delay_ms is too large")
	}
	wait := h.wait
	if wait == nil {
		wait = sleepContext
	}
	return wait(ctx, time.Duration(delayMS)*time.Millisecond)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (h *Handler) rejectUnmatched(w *bodyCapture, started time.Time, r *http.Request, reqHeaders http.Header, payload []byte, ruleID, forwarded string, delayMS int) {
	writeJSONError(w, http.StatusNotFound, noUpstreamMessage)
	h.record(started, r, reqHeaders, payload, w, history.SourceRejected, ruleID, "", "", forwarded, delayMS, viaPrefix)
}

func (h *Handler) finish(w http.ResponseWriter, r *http.Request, started time.Time, reqHeaders http.Header, payload []byte, source history.Source, ruleID, upstreamID, upstreamName string, status int, reason string) {
	capw := &bodyCapture{ResponseWriter: w, limit: h.maxBody}
	writeJSONError(capw, status, reason)
	h.record(started, r, reqHeaders, payload, capw, source, ruleID, upstreamID, upstreamName, r.URL.Path, 0, viaPrefix)
}

// target is the upstream chosen for one request.
type target struct {
	name, id, forwarded, prefix string
	via                         routeVia
	dest                        *url.URL
	ok                          bool
	// prefixed is true when forwarded is the request path without prefix.
	prefixed bool
}

// resolve tries the path prefix, then the Referer path, then the sticky
// upstream cookie. The last two forward the path unchanged.
func (h *Handler) resolve(r *http.Request) target {
	path := r.URL.Path
	if path == "" {
		path = "/"
	}
	if h.upstreams != nil {
		route, found := h.upstreams.Route(path)
		if !found {
			route, found = h.upstreams.MatchReferer(path, r.Referer())
		}
		if !found {
			if id, ok := stickyUpstream(r); ok {
				route, found = h.upstreams.MatchID(path, id)
			}
		}
		if !found {
			return target{forwarded: path}
		}
		t := target{
			name:      route.Upstream.Name,
			id:        route.Upstream.ID,
			forwarded: route.ForwardedPath,
			prefix:    route.Upstream.Prefix,
			prefixed:  route.Prefixed,
		}
		switch {
		case route.ViaReferer:
			t.via = viaReferer
		case route.ViaCookie:
			t.via = viaCookie
		}
		parsed, err := url.Parse(route.Upstream.BaseURL)
		if err != nil || parsed.Host == "" {
			return t
		}
		t.dest, t.ok = parsed, true
		return t
	}
	if h.upstream != nil {
		return target{forwarded: path, dest: h.upstream, ok: true}
	}
	return target{forwarded: path}
}

func writeMock(w http.ResponseWriter, rule rules.Rule) {
	if rule.ResponseMode == rules.ModeRaw {
		writeRaw(w, rule)
		return
	}
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

func writeRaw(w http.ResponseWriter, rule rules.Rule) {
	parsed, err := rules.ParseRawResponse(rule.RawResponse)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "saved raw response is invalid")
		return
	}
	if rules.WantsClose(parsed) && writeRawClosed(w, parsed, rule.MockHeader) {
		return
	}
	setRawHeaders(w, parsed, rule.MockHeader)
	w.WriteHeader(parsed.Code)
	if len(parsed.Body) > 0 {
		_, _ = w.Write(parsed.Body)
	}
}

func setRawHeaders(w http.ResponseWriter, parsed rules.ParsedRaw, mockHeader bool) {
	for _, header := range parsed.Headers {
		if strings.EqualFold(header.Name, "Content-Length") || strings.EqualFold(header.Name, "Transfer-Encoding") {
			continue
		}
		w.Header().Add(header.Name, header.Value)
	}
	if mockHeader {
		w.Header().Set("X-Mock-Response", "true")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(parsed.Body)))
}

func writeRawClosed(w http.ResponseWriter, parsed rules.ParsedRaw, mockHeader bool) bool {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return false
	}
	conn, bufrw, err := hj.Hijack()
	if err != nil {
		return false
	}
	setRawHeaders(w, parsed, mockHeader)
	if capture, ok := w.(*bodyCapture); ok {
		capture.note(parsed.Code, parsed.Body)
	}
	_, _ = bufrw.Write(rules.WireBytes(parsed, mockHeader))
	_ = bufrw.Flush()
	_ = conn.Close()
	return true
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

func isAPI(path string) bool {
	return path == "/_api" || strings.HasPrefix(path, "/_api/")
}

func (h *Handler) forward(w http.ResponseWriter, r *http.Request, upstream *url.URL, t target) bool {
	incomingPath := t.forwarded
	if incomingPath == "" {
		incomingPath = r.URL.Path
	}
	incomingQuery := r.URL.RawQuery
	// A WebSocket (or other) upgrade must not be buffered, and the upstream
	// timeout must not cancel the tunneled connection.
	upgrade := isProtocolUpgrade(r.Header)
	if capture, ok := w.(*bodyCapture); ok && upgrade {
		capture.upgrade = true
	}
	ctx := r.Context()
	var cancel context.CancelFunc
	// answered is set once response headers arrive. UPSTREAM_TIMEOUT covers
	// only the wait for headers, so a slow body is not cut off.
	var gate sync.Mutex
	answered := false
	var timer *time.Timer
	if !upgrade {
		var stop context.CancelCauseFunc
		ctx, stop = context.WithCancelCause(r.Context())
		defer stop(nil)
		cancel = func() { stop(context.DeadlineExceeded) }
		timer = time.AfterFunc(h.timeout, func() {
			gate.Lock()
			defer gate.Unlock()
			if !answered {
				cancel()
			}
		})
		defer timer.Stop()
	}
	outbound := r.WithContext(ctx)

	failed := false
	reverse := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.URL.Path = joinPath(upstream.Path, incomingPath)
			req.URL.RawPath = escapedForwardPath(r.URL, upstream, t, req.URL.Path)
			req.URL.RawQuery = incomingQuery
			req.Host = upstream.Host
			rewriteOutbound(req.Header, r.Host, upstream, t.prefix, t.id)
			if capture, ok := w.(*bodyCapture); ok {
				capture.sent = sentHeaders(req.Header, req.Host)
			}
		},
		Transport: h.transport,
		ModifyResponse: func(resp *http.Response) error {
			gate.Lock()
			answered = true
			gate.Unlock()
			if timer != nil {
				timer.Stop()
			}
			rewriteUpstreamResponse(resp, r.Host, h.redirectTargets(upstream, t), t.id)
			if upgrade || resp.StatusCode == http.StatusSwitchingProtocols {
				return nil
			}
			if isStreamResponse(resp) {
				if capture, ok := w.(*bodyCapture); ok {
					capture.previewLimit = config.HistoryBodyPreview
				}
				return nil
			}
			body, err := readUpTo(resp.Body, h.maxBody)
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
			status := http.StatusBadGateway
			reason := "upstream unavailable"
			switch {
			case errors.Is(err, errResponseTooLarge):
				reason = "upstream response too large"
			case upstreamTimedOut(r, err):
				status = http.StatusGatewayTimeout
				reason = "upstream timed out"
			}
			writeJSONError(w, status, reason)
		},
	}
	reverse.ServeHTTP(w, outbound)
	return failed
}

// escapedForwardPath keeps the client's escaping, such as %2F inside a
// segment, on the path sent upstream. It returns "" when the default escaping
// of path is already right, or when the escaped form cannot be lined up with
// the decoded one. url.URL then falls back to escaping path itself.
func escapedForwardPath(in *url.URL, upstream *url.URL, t target, path string) string {
	if in == nil || in.RawPath == "" {
		return ""
	}
	escaped := in.EscapedPath()
	if t.prefixed {
		switch {
		case escaped == t.prefix:
			escaped = "/"
		case strings.HasPrefix(escaped, t.prefix+"/"):
			escaped = escaped[len(t.prefix):]
		default:
			// The prefix itself was escaped, such as /%61pp. Use the decoded path.
			return ""
		}
	}
	raw := joinPath(upstream.EscapedPath(), escaped)
	if unescaped, err := url.PathUnescape(raw); err != nil || unescaped != path {
		return ""
	}
	return raw
}

// isStreamResponse reports a body that should be forwarded as it arrives.
// That is an event stream, a chunked body, or any response of unknown length.
func isStreamResponse(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	if media, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil && media == "text/event-stream" {
		return true
	}
	if resp.ContentLength < 0 {
		return true
	}
	for _, value := range resp.Header.Values("Transfer-Encoding") {
		if strings.Contains(strings.ToLower(value), "chunked") {
			return true
		}
	}
	return false
}

// isProtocolUpgrade reports a Connection: Upgrade request with an Upgrade header.
func isProtocolUpgrade(h http.Header) bool {
	if strings.TrimSpace(h.Get("Upgrade")) == "" {
		return false
	}
	for _, value := range h.Values("Connection") {
		for _, part := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
				return true
			}
		}
	}
	return false
}

func (h *Handler) record(started time.Time, r *http.Request, reqHeaders http.Header, payload []byte, capw *bodyCapture, source history.Source, ruleID, upstreamID, upstreamName, forwardedPath string, delayMS int, via routeVia) {
	if h.history == nil {
		return
	}
	status, respHeaders, respRaw, respCut := capw.snapshot()
	var respBody, respPlain string
	var respTrunc, respDecoded bool
	switch {
	case capw.upgrade && status == http.StatusSwitchingProtocols && len(respRaw) == 0 && !respCut:
		respBody = "protocol upgrade; messages were not captured"
	case respCut:
		respBody = fmt.Sprintf("body exceeds %d bytes and was not decoded", h.maxBody)
		respTrunc = true
	default:
		respBody, respPlain, respTrunc, respDecoded = historyCopy(respHeaders, respRaw)
		if capw.previewCut {
			respTrunc = true
		}
	}
	reqBody, reqTrunc := displayBody(reqHeaders, payload)
	ruleName := ""
	if ruleID != "" && h.rules != nil {
		if matched, ok := h.rules.Get(ruleID); ok {
			ruleName = matched.Name
		}
	}
	h.history.Add(history.Entry{
		StartedAt:             started,
		DurationMS:            int(time.Since(started).Milliseconds()),
		Method:                r.Method,
		Path:                  r.URL.Path,
		RawQuery:              r.URL.RawQuery,
		ForwardedPath:         forwardedPath,
		RequestHeaders:        reqHeaders,
		UpstreamHeaders:       capw.sent,
		RequestBody:           reqBody,
		Status:                status,
		ResponseHeaders:       respHeaders,
		ResponseBody:          respBody,
		ResponsePlain:         respPlain,
		ResponseDecoded:       respDecoded,
		Source:                source,
		RuleID:                ruleID,
		RuleName:              ruleName,
		Fault:                 capw.fault,
		Step:                  capw.step,
		DelayMS:               delayMS,
		UpstreamID:            upstreamID,
		UpstreamName:          upstreamName,
		ViaReferer:            via == viaReferer && upstreamName != "",
		ViaCookie:             via == viaCookie && upstreamName != "",
		RequestBodyTruncated:  reqTrunc,
		ResponseBodyTruncated: respTrunc,
	})
}

// redirectTargets lists the answering upstream first, then every other
// configured upstream, for rewriting redirects.
func (h *Handler) redirectTargets(dest *url.URL, t target) []redirectTarget {
	targets := []redirectTarget{{prefix: t.prefix, base: dest}}
	if h.upstreams == nil {
		return targets
	}
	for _, item := range h.upstreams.List() {
		if item.ID == t.id {
			continue
		}
		parsed, err := url.Parse(item.BaseURL)
		if err != nil || parsed.Host == "" {
			continue
		}
		targets = append(targets, redirectTarget{prefix: item.Prefix, base: parsed})
	}
	return targets
}

func upstreamTimedOut(r *http.Request, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(context.Cause(r.Context()), context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func writeJSONError(w http.ResponseWriter, status int, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
}
