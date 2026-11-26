package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

// redirectTarget is one upstream a redirect may point at, and the proxy
// prefix that reaches it.
type redirectTarget struct {
	prefix string
	base   *url.URL
}

// rewriteUpstreamResponse points upstream redirects back at this proxy and
// strips cookie attributes that do not work on plain HTTP. targets[0] is the
// upstream that answered. The rest are the other configured upstreams, so a
// sign-in server that redirects to another configured host stays on the proxy.
// Cookies are stored per upstream on Path=/; see rewriteSetCookies.
func rewriteUpstreamResponse(resp *http.Response, proxyHost string, targets []redirectTarget, upstreamID string) {
	if resp == nil || len(targets) == 0 || targets[0].base == nil {
		return
	}
	proxyHost = strings.TrimSpace(proxyHost)
	if proxyHost == "" {
		return
	}
	rewriteHeaderURL(resp.Header, "Location", proxyHost, targets)
	rewriteHeaderURL(resp.Header, "Content-Location", proxyHost, targets)
	rewriteSetCookies(resp.Header, upstreamID)
}

func rewriteHeaderURL(header http.Header, name, proxyHost string, targets []redirectTarget) {
	values := header.Values(name)
	if len(values) == 0 {
		return
	}
	next := make([]string, len(values))
	changed := false
	for i, value := range values {
		rewritten, ok := rewriteLocationTo(value, proxyHost, targets)
		next[i] = rewritten
		if ok {
			changed = true
		}
	}
	if !changed {
		return
	}
	header.Del(name)
	for _, value := range next {
		header.Add(name, value)
	}
}

// rewriteLocation rewrites raw for a single upstream. Tests use it.
func rewriteLocation(raw, proxyHost string, upstream *url.URL, prefix string) (string, bool) {
	return rewriteLocationTo(raw, proxyHost, []redirectTarget{{prefix: prefix, base: upstream}})
}

func rewriteLocationTo(raw, proxyHost string, targets []redirectTarget) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if len(targets) == 0 || targets[0].base == nil || trimmed == "" {
		return raw, false
	}
	if strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "//") {
		return rewriteRootRelative(trimmed, targets[0])
	}
	return rewriteAbsoluteTo(trimmed, proxyHost, targets)
}

// rewriteRootRelative maps a path on the answering upstream to the proxy.
// The upstream never sees the prefix, so its paths get it added back. The
// base URL path is removed first, because the proxy adds it when forwarding.
func rewriteRootRelative(raw string, target redirectTarget) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return raw, false
	}
	rel, _ := underBase(parsed.Path, target.base.Path)
	parsed.Path = joinRedirectPath(target.prefix, rel)
	parsed.RawPath = ""
	rewritten := parsed.String()
	if rewritten == raw {
		return raw, false
	}
	return rewritten, true
}

func rewriteAbsoluteURL(raw, proxyHost string, upstream *url.URL, prefix string) (string, bool) {
	return rewriteAbsoluteTo(raw, proxyHost, []redirectTarget{{prefix: prefix, base: upstream}})
}

// rewriteAbsoluteTo handles https://host/path and //host/path. It picks the
// upstream with the same origin whose base path holds the path, preferring
// the answering upstream, then the longest base path. A URL for a host that
// is not configured stays as the upstream sent it.
func rewriteAbsoluteTo(raw, proxyHost string, targets []redirectTarget) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Opaque != "" || !webScheme(parsed.Scheme) {
		return raw, false
	}
	best := -1
	bestRel := ""
	fallback := -1
	for i, target := range targets {
		if target.base == nil {
			continue
		}
		candidate := *parsed
		if candidate.Scheme == "" {
			candidate.Scheme = target.base.Scheme
		}
		if !sameUpstreamOrigin(&candidate, target.base) {
			continue
		}
		if fallback < 0 {
			fallback = i
		}
		rel, ok := underBase(parsed.Path, target.base.Path)
		if !ok {
			continue
		}
		if best < 0 || (best != 0 && len(target.base.Path) > len(targets[best].base.Path)) {
			best, bestRel = i, rel
		}
	}
	if best < 0 {
		if fallback < 0 {
			return raw, false
		}
		best, bestRel = fallback, parsed.Path
	}
	parsed.Scheme = "http"
	parsed.Host = proxyHost
	parsed.Path = joinRedirectPath(targets[best].prefix, bestRel)
	parsed.RawPath = ""
	return parsed.String(), true
}

// webScheme is true for http, https, and a scheme-relative URL. An app
// callback such as com.example.pos.dev://callback, intent://, or mailto: is never
// rewritten, so the browser can hand it to the app unchanged.
func webScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "", "http", "https":
		return true
	default:
		return false
	}
}

// underBase removes the upstream base URL path. ok is false when path is
// outside it, and path is then returned unchanged.
func underBase(path, base string) (string, bool) {
	base = strings.TrimSuffix(base, "/")
	if base == "" {
		return path, true
	}
	if path == base {
		return "/", true
	}
	if strings.HasPrefix(path, base+"/") {
		return path[len(base):], true
	}
	return path, false
}

func sameUpstreamOrigin(candidate, upstream *url.URL) bool {
	if candidate == nil || upstream == nil || candidate.Scheme == "" || candidate.Host == "" {
		return false
	}
	if !strings.EqualFold(candidate.Scheme, upstream.Scheme) {
		return false
	}
	if !strings.EqualFold(candidate.Hostname(), upstream.Hostname()) {
		return false
	}
	return urlPort(candidate) == urlPort(upstream)
}

func urlPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func joinRedirectPath(prefix, path string) string {
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if prefix == "" {
		return path
	}
	if path == prefix || strings.HasPrefix(path, prefix+"/") {
		return path
	}
	if path == "/" {
		return prefix
	}
	return strings.TrimSuffix(prefix, "/") + path
}
