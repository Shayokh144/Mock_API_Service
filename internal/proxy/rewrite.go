package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

// rewriteUpstreamResponse points absolute upstream redirects back at this proxy
// and strips cookie attributes that do not work on plain HTTP. cookiePrefix is
// added to cookie paths. It is empty when the page was not served under the
// prefix, so a cookie set by /login is still sent to the form post on /login.
func rewriteUpstreamResponse(resp *http.Response, proxyHost string, upstream *url.URL, prefix, cookiePrefix string) {
	if resp == nil || upstream == nil {
		return
	}
	proxyHost = strings.TrimSpace(proxyHost)
	if proxyHost == "" {
		return
	}
	rewriteHeaderURL(resp.Header, "Location", proxyHost, upstream, prefix)
	rewriteHeaderURL(resp.Header, "Content-Location", proxyHost, upstream, prefix)
	rewriteSetCookies(resp.Header, upstream.Hostname(), cookiePrefix)
}

func rewriteHeaderURL(header http.Header, name, proxyHost string, upstream *url.URL, prefix string) {
	values := header.Values(name)
	if len(values) == 0 {
		return
	}
	next := make([]string, len(values))
	changed := false
	for i, value := range values {
		rewritten, ok := rewriteLocation(value, proxyHost, upstream, prefix)
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

func rewriteLocation(raw, proxyHost string, upstream *url.URL, prefix string) (string, bool) {
	if rewritten, ok := rewriteRootRelative(strings.TrimSpace(raw), prefix); ok {
		return rewritten, true
	}
	return rewriteAbsoluteURL(raw, proxyHost, upstream, prefix)
}

func rewriteRootRelative(raw, prefix string) (string, bool) {
	if prefix == "" || raw == "" || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") {
		return raw, false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" {
		return raw, false
	}
	parsed.Path = joinRedirectPath(prefix, parsed.Path)
	parsed.RawPath = ""
	rewritten := parsed.String()
	if rewritten == raw {
		return raw, false
	}
	return rewritten, true
}

func rewriteAbsoluteURL(raw, proxyHost string, upstream *url.URL, prefix string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !sameUpstreamOrigin(parsed, upstream) {
		return raw, false
	}
	parsed.Scheme = "http"
	parsed.Host = proxyHost
	parsed.Path = joinRedirectPath(prefix, parsed.Path)
	parsed.RawPath = ""
	return parsed.String(), true
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

func rewriteSetCookies(header http.Header, upstreamHost, prefix string) {
	values := header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	next := make([]string, len(values))
	changed := false
	for i, value := range values {
		rewritten := rewriteSetCookie(value, upstreamHost, prefix)
		next[i] = rewritten
		if rewritten != value {
			changed = true
		}
	}
	if !changed {
		return
	}
	header.Del("Set-Cookie")
	for _, value := range next {
		header.Add("Set-Cookie", value)
	}
}

func rewriteSetCookie(raw, upstreamHost, prefix string) string {
	cookie, err := http.ParseSetCookie(raw)
	if err != nil || cookie.Name == "" {
		return raw
	}
	if domainMatchesUpstream(cookie.Domain, upstreamHost) {
		cookie.Domain = ""
	}
	cookie.Secure = false
	cookie.Path = prefixCookiePath(prefix, cookie.Path)
	serialized := cookie.String()
	if serialized == "" {
		return raw
	}
	return serialized
}

func domainMatchesUpstream(domain, host string) bool {
	domain = strings.TrimPrefix(strings.TrimSpace(domain), ".")
	host = strings.TrimPrefix(strings.TrimSpace(host), ".")
	return domain != "" && strings.EqualFold(domain, host)
}

func prefixCookiePath(prefix, cookiePath string) string {
	if prefix == "" || cookiePath == "" || !strings.HasPrefix(cookiePath, "/") {
		return cookiePath
	}
	if cookiePath == prefix || strings.HasPrefix(cookiePath, prefix+"/") {
		return cookiePath
	}
	if cookiePath == "/" {
		return prefix
	}
	return strings.TrimSuffix(prefix, "/") + cookiePath
}
