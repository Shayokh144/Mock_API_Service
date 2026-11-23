package proxy

import (
	"net/http"
	"net/url"
	"strings"
)

// forwardingHeaders would make the upstream build its own URL from values the
// client chose. Rails, for example, reads X-Forwarded-Host for base_url.
var forwardingHeaders = []string{"Forwarded", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port", "X-Forwarded-Prefix", "X-Forwarded-Ssl"}

// sentHeaderNames are copied into History as the headers sent upstream.
// Cookie values and Authorization are not copied. Cookie-Names lists only
// the cookie names, so a missing session cookie is visible without its value.
var sentHeaderNames = []string{"Host", "Origin", "Referer"}

// rewriteOutbound makes a same-origin browser request look same-origin to the
// upstream. A Rails app compares Origin with the scheme and host it sees, so
// Origin http://10.0.2.2:8080 fails its CSRF check with a 422.
// proxyHost is the Host the client used. Values for other hosts stay as sent.
func rewriteOutbound(header http.Header, proxyHost string, upstream *url.URL, prefix, upstreamID string) {
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
	rewriteRequestCookies(header, upstreamID)
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
	if names := cookieNames(header); names != "" {
		out.Set("Cookie-Names", names)
	}
	return out
}
