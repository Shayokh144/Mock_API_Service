package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

// Cookie names with these prefixes need Secure, which a plain HTTP page cannot
// set. The proxy renames them for the browser and restores them upstream.
var cookieNamePrefixes = [][2]string{
	{"__Host-", "__mp_Host-"},
	{"__Secure-", "__mp_Secure-"},
}

// forwardingHeaders would make the upstream build its own URL from values the
// client chose. Rails, for example, reads X-Forwarded-Host for base_url.
var forwardingHeaders = []string{"Forwarded", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port", "X-Forwarded-Prefix", "X-Forwarded-Ssl"}

// sentHeaderNames are copied into History as the headers sent upstream.
// Cookie and Authorization are not copied.
var sentHeaderNames = []string{"Host", "Origin", "Referer"}

// rewriteOutbound makes a same-origin browser request look same-origin to the
// upstream. A Rails app compares Origin with the scheme and host it sees, so
// Origin http://10.0.2.2:8080 fails its CSRF check with a 422.
// proxyHost is the Host the client used. Values for other hosts stay as sent.
func rewriteOutbound(header http.Header, proxyHost string, upstream *url.URL, prefix string) {
	for _, name := range forwardingHeaders {
		header.Del(name)
	}
	base := upstreamOrigin(upstream)
	if origin := header.Get("Origin"); origin != "" {
		if parsed, err := url.Parse(origin); err == nil && sameHost(parsed.Host, proxyHost) {
			header.Set("Origin", base)
		}
	}
	if referer := header.Get("Referer"); referer != "" {
		if rewritten, ok := upstreamReferer(referer, proxyHost, upstream, prefix); ok {
			header.Set("Referer", rewritten)
		}
	}
	rewriteRequestCookies(header)
}

// upstreamOrigin is scheme://host, without a default port, the way a browser
// on the real site would send Origin.
func upstreamOrigin(upstream *url.URL) string {
	scheme := strings.ToLower(upstream.Scheme)
	host := strings.ToLower(upstream.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := upstream.Port(); port != "" && port != urlPort(&url.URL{Scheme: scheme}) {
		host += ":" + port
	}
	return scheme + "://" + host
}

func upstreamReferer(raw, proxyHost string, upstream *url.URL, prefix string) (string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || !sameHost(parsed.Host, proxyHost) {
		return raw, false
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	if prefix != "" {
		if path == prefix {
			path = "/"
		} else if strings.HasPrefix(path, prefix+"/") {
			path = path[len(prefix):]
		}
	}
	base, err := url.Parse(upstreamOrigin(upstream))
	if err != nil {
		return raw, false
	}
	base.Path = joinPath(upstream.Path, path)
	base.RawQuery = parsed.RawQuery
	return base.String(), true
}

func sameHost(a, b string) bool {
	return a != "" && strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// rewriteRequestCookies drops the proxy's own cookie and restores renamed
// __Host- and __Secure- cookie names.
func rewriteRequestCookies(header http.Header) {
	values := header.Values("Cookie")
	if len(values) == 0 {
		return
	}
	var kept []string
	changed := false
	for _, value := range values {
		for _, part := range strings.Split(value, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			name, rest, hasValue := strings.Cut(part, "=")
			name = strings.TrimSpace(name)
			if name == stickyCookieName {
				changed = true
				continue
			}
			if restored, ok := restoreCookieName(name); ok {
				changed = true
				part = restored
				if hasValue {
					part += "=" + rest
				}
			}
			kept = append(kept, part)
		}
	}
	if !changed {
		return
	}
	header.Del("Cookie")
	if len(kept) > 0 {
		header.Set("Cookie", strings.Join(kept, "; "))
	}
}

func restoreCookieName(name string) (string, bool) {
	for _, pair := range cookieNamePrefixes {
		if strings.HasPrefix(name, pair[1]) {
			return pair[0] + name[len(pair[1]):], true
		}
	}
	return name, false
}

func browserCookieName(name string) string {
	for _, pair := range cookieNamePrefixes {
		if len(name) >= len(pair[0]) && strings.EqualFold(name[:len(pair[0])], pair[0]) {
			return pair[1] + name[len(pair[0]):]
		}
	}
	return name
}

func sentHeaders(header http.Header, host string) http.Header {
	out := http.Header{}
	for _, name := range sentHeaderNames {
		if name == "Host" {
			if host != "" {
				out.Set("Host", host)
			}
			continue
		}
		if value := header.Get(name); value != "" {
			out.Set(name, value)
		}
	}
	return out
}
