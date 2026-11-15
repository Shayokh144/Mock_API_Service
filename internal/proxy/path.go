package proxy

import "strings"

// joinPath appends the incoming request path to the upstream base path.
// A trailing slash on the request path is preserved. path.Clean is not used.
func joinPath(basePath, reqPath string) string {
	if reqPath == "" {
		reqPath = "/"
	}
	if !strings.HasPrefix(reqPath, "/") {
		reqPath = "/" + reqPath
	}
	if basePath == "" || basePath == "/" {
		return reqPath
	}
	return strings.TrimSuffix(basePath, "/") + reqPath
}
