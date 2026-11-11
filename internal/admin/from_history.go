package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
)

func (s *Server) newRule(w http.ResponseWriter, r *http.Request) {
	view := emptyRuleForm()
	message := ""
	if from := strings.TrimSpace(r.URL.Query().Get("from")); from != "" {
		var entry history.Entry
		ok := false
		if s.history != nil {
			entry, ok = s.history.Get(from)
		}
		if !ok {
			message = "That history entry is no longer available. The form is empty."
		} else {
			view = s.prefillFromHistory(entry)
		}
	}
	s.render(w, "rule_form", page{
		Title:           "New rule",
		Methods:         methods,
		Form:            view,
		UpstreamChoices: s.upstreamChoices(view.UpstreamID),
		Error:           message,
	}, http.StatusOK)
}

func emptyRuleForm() formView {
	return formView{
		Action:     "/_admin/rules",
		Submit:     "Create rule",
		Method:     http.MethodGet,
		StatusCode: "200",
		DelayMS:    "0",
		Enabled:    true,
	}
}

func (s *Server) prefillFromHistory(entry history.Entry) formView {
	path := s.rulePath(entry)
	method := strings.ToUpper(strings.TrimSpace(entry.Method))
	if !knownMethod(method) {
		method = http.MethodGet
	}
	view := emptyRuleForm()
	view.Name = ruleName(method, path)
	view.Method = method
	view.PathPattern = path
	view.Query = queryConditionLines(entry.RawQuery)
	view.UpstreamID = s.knownUpstreamID(entry.UpstreamID)
	view.Enabled = true
	view.DelayMS = "0"
	if !historyHasResponse(entry) {
		return view
	}
	text := responseText(entry)
	view.StatusCode = strconv.Itoa(entry.Status)
	view.Headers = ruleHeaderLines(entry.ResponseHeaders, entry.ResponseDecoded)
	view.Body = compactJSON(text)
	view.RawResponse = rawResponse(entry.Status, entry.ResponseHeaders, text, entry.ResponseDecoded)
	view.MockHeader = strings.EqualFold(entry.ResponseHeaders.Get("X-Mock-Response"), "true")
	return view
}

func (s *Server) rulePath(entry history.Entry) string {
	path := entry.ForwardedPath
	if path == "" {
		path = entry.Path
		if s.upstreams != nil && entry.UpstreamID != "" && entry.UpstreamID != history.UpstreamAny {
			if item, ok := s.upstreams.Get(entry.UpstreamID); ok {
				path = stripUpstreamPrefix(path, item.Prefix)
			}
		}
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func stripUpstreamPrefix(path, prefix string) string {
	if prefix == "" {
		return path
	}
	if path == prefix {
		return "/"
	}
	if strings.HasPrefix(path, prefix+"/") {
		return path[len(prefix):]
	}
	return path
}

func (s *Server) knownUpstreamID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || id == history.UpstreamAny || s.upstreams == nil {
		return ""
	}
	if _, ok := s.upstreams.Get(id); !ok {
		return ""
	}
	return id
}

func knownMethod(method string) bool {
	for _, allowed := range methods {
		if method == allowed {
			return true
		}
	}
	return false
}

func ruleName(method, path string) string {
	name := method + " " + path
	if utf8.RuneCountInString(name) <= 80 {
		return name
	}
	runes := []rune(name)
	return string(runes[:80])
}

func historyHasResponse(entry history.Entry) bool {
	switch entry.Source {
	case history.SourceUpstreamError, history.SourceRejected:
		return false
	default:
		return entry.Status >= 100 && entry.Status <= 599
	}
}

func responseText(entry history.Entry) string {
	if entry.ResponsePlain != "" {
		return entry.ResponsePlain
	}
	body := entry.ResponseBody
	if body == "" || strings.HasPrefix(body, "binary body") || strings.HasPrefix(body, "could not decode") || strings.HasPrefix(body, "body exceeds") {
		return ""
	}
	return body
}

func compactJSON(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" || !json.Valid([]byte(trimmed)) {
		return body
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(trimmed)); err != nil {
		return body
	}
	return buf.String()
}

func queryConditionLines(raw string) string {
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		key, value, exact := strings.Cut(part, "=")
		key = unescapeQuery(key)
		if key == "" {
			continue
		}
		if !exact {
			fmt.Fprintf(&b, "%s\n", key)
			continue
		}
		fmt.Fprintf(&b, "%s=%s\n", key, unescapeQuery(value))
	}
	return b.String()
}

func unescapeQuery(raw string) string {
	value, err := url.QueryUnescape(raw)
	if err != nil {
		return raw
	}
	return value
}

func ruleHeaderLines(header http.Header, dropEncoding bool) string {
	if len(header) == 0 {
		return ""
	}
	names := make([]string, 0, len(header))
	for name := range header {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		if skipRuleHeader(name, dropEncoding) {
			continue
		}
		for _, value := range header[name] {
			if strings.ContainsAny(value, "\r\n") {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", name, value)
		}
	}
	return b.String()
}

func skipRuleHeader(name string, dropEncoding bool) bool {
	canon := http.CanonicalHeaderKey(name)
	switch canon {
	case "Content-Length", "Transfer-Encoding", "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Upgrade", "X-Mock-Response":
		return true
	case "Content-Encoding":
		return dropEncoding
	default:
		return false
	}
}

func rawResponse(status int, header http.Header, body string, dropEncoding bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP/1.1 %d", status)
	if reason := http.StatusText(status); reason != "" {
		b.WriteByte(' ')
		b.WriteString(reason)
	}
	b.WriteByte('\n')
	if len(header) > 0 {
		names := make([]string, 0, len(header))
		for name := range header {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if skipRawHeader(name, dropEncoding) {
				continue
			}
			for _, value := range header[name] {
				if strings.ContainsAny(value, "\r\n") {
					continue
				}
				fmt.Fprintf(&b, "%s: %s\n", name, value)
			}
		}
	}
	b.WriteByte('\n')
	b.WriteString(body)
	return b.String()
}

func skipRawHeader(name string, dropEncoding bool) bool {
	canon := http.CanonicalHeaderKey(name)
	switch canon {
	case "Content-Length", "Transfer-Encoding", "Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Upgrade":
		return true
	case "Content-Encoding":
		return dropEncoding
	case "X-Mock-Response":
		return true
	default:
		return false
	}
}
