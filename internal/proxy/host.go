package proxy

import (
	"net"
	"strings"
)

// emulatorHosts are the addresses an Android emulator uses for the Mac's
// loopback: 10.0.2.2 on the stock emulator, 10.0.3.2 on Genymotion.
var emulatorHosts = []string{"10.0.2.2", "10.0.3.2"}

// LocalHosts returns the Host check for a proxy listener. It allows
// localhost, *.localhost, any loopback IP, the Android emulator aliases, and
// each name in extra. Ports are ignored.
//
// A page on another site can point its own domain at 127.0.0.1 (DNS
// rebinding) and then read proxy responses as same-origin. Its requests carry
// that domain as Host, so checking Host stops them.
func LocalHosts(extra []string) func(string) bool {
	allowed := map[string]struct{}{}
	for _, name := range emulatorHosts {
		allowed[name] = struct{}{}
	}
	for _, name := range extra {
		if name = normalizeHost(name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	return func(hostport string) bool {
		host := normalizeHost(hostOnly(hostport))
		if host == "" {
			return false
		}
		if host == "localhost" || strings.HasSuffix(host, ".localhost") {
			return true
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return true
		}
		_, ok := allowed[host]
		return ok
	}
}

func hostOnly(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.TrimSuffix(host, ".")
}
