package proxy

import "net/http"

// conditionalRequestHeaders are validators that let an upstream answer 304.
// A fresh-request rule removes them so the upstream returns a full response.
var conditionalRequestHeaders = []string{
	"If-None-Match",
	"If-Modified-Since",
	"If-Match",
	"If-Unmodified-Since",
	"If-Range",
}

func prepareFreshRequest(header http.Header) {
	for _, name := range conditionalRequestHeaders {
		header.Del(name)
	}
	header.Set("Cache-Control", "no-cache")
	header.Set("Pragma", "no-cache")
}
