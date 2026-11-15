// Package rules stores mock rules and chooses which one matches a request.
package rules

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
)

const maxPatternLength = 2048

// maxDelayMS is the longest pause a rule may add before the response is written.
const maxDelayMS = 60000

// Rule is one saved mock. Body is null when the response has no body.
type Rule struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	APIName      string            `json:"api_name,omitempty"`
	Enabled      bool              `json:"enabled"`
	FreshRequest bool              `json:"fresh_request"`
	UpstreamID   string            `json:"upstream_id,omitempty"`
	Method       string            `json:"method"`
	PathPattern  string            `json:"path_pattern"`
	Match        MatchSpec         `json:"match,omitempty"`
	StatusCode   int               `json:"status_code"`
	DelayMS      int               `json:"delay_ms,omitempty"`
	ResponseMode string            `json:"response_mode,omitempty"`
	RawResponse  string            `json:"raw_response,omitempty"`
	MockHeader   bool              `json:"mock_header,omitempty"`
	Headers      map[string]string `json:"headers"`
	Body         json.RawMessage   `json:"body"`
	CreatedAt    timeValue         `json:"created_at"`
	UpdatedAt    timeValue         `json:"updated_at"`
}

// Draft is the input for creating or updating a rule.
type Draft struct {
	Name         string
	APIName      string
	Enabled      bool
	FreshRequest bool
	UpstreamID   string
	Method       string
	PathPattern  string
	Match        MatchSpec
	StatusCode   int
	DelayMS      int
	ResponseMode string
	RawResponse  string
	MockHeader   bool
	Headers      map[string]string
	Body         string
}

var allowedMethods = map[string]struct{}{
	http.MethodGet:     {},
	http.MethodPost:    {},
	http.MethodPut:     {},
	http.MethodPatch:   {},
	http.MethodDelete:  {},
	http.MethodHead:    {},
	http.MethodOptions: {},
}

var hopByHop = map[string]struct{}{
	"Connection":          {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

func normalize(d Draft, allowWildcard bool) (Rule, error) {
	name := strings.TrimSpace(d.Name)
	if utf8.RuneCountInString(name) > 80 {
		return Rule{}, fmt.Errorf("name must be 80 characters or fewer")
	}
	apiName, err := NormalizeAPIName(d.APIName)
	if err != nil {
		return Rule{}, err
	}
	upstreamID, err := normalizeUpstreamID(d.UpstreamID)
	if err != nil {
		return Rule{}, err
	}
	method := strings.ToUpper(strings.TrimSpace(d.Method))
	if _, ok := allowedMethods[method]; !ok {
		return Rule{}, fmt.Errorf("method must be one of GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS")
	}
	if d.DelayMS < 0 || d.DelayMS > maxDelayMS {
		return Rule{}, fmt.Errorf("delay_ms must be from 0 through %d", maxDelayMS)
	}
	mode, err := responseMode(d.ResponseMode)
	if err != nil {
		return Rule{}, err
	}
	if err := validatePattern(d.PathPattern, allowWildcard); err != nil {
		return Rule{}, err
	}
	match, err := canonicalMatch(d.Match)
	if err != nil {
		return Rule{}, err
	}
	if mode == ModeRaw {
		parsed, err := ParseRawResponse(d.RawResponse)
		if err != nil {
			return Rule{}, err
		}
		return Rule{
			Name:         name,
			APIName:      apiName,
			Enabled:      d.Enabled,
			FreshRequest: d.FreshRequest,
			UpstreamID:   upstreamID,
			Method:       method,
			PathPattern:  d.PathPattern,
			Match:        match,
			StatusCode:   parsed.Code,
			DelayMS:      d.DelayMS,
			ResponseMode: ModeRaw,
			RawResponse:  d.RawResponse,
			MockHeader:   d.MockHeader,
			Headers:      map[string]string{},
		}, nil
	}
	if d.StatusCode < 100 || d.StatusCode > 599 {
		return Rule{}, fmt.Errorf("status_code must be from 100 through 599")
	}
	headers, err := normalizeHeaders(d.Headers)
	if err != nil {
		return Rule{}, err
	}
	body, err := normalizeBody(d.Body)
	if err != nil {
		return Rule{}, err
	}
	return Rule{
		Name:         name,
		APIName:      apiName,
		Enabled:      d.Enabled,
		FreshRequest: d.FreshRequest,
		UpstreamID:   upstreamID,
		Method:       method,
		PathPattern:  d.PathPattern,
		Match:        match,
		StatusCode:   d.StatusCode,
		DelayMS:      d.DelayMS,
		Headers:      headers,
		Body:         body,
	}, nil
}

func normalizeUpstreamID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", nil
	}
	if _, err := hex.DecodeString(id); err != nil || len(id) != 16 {
		return "", fmt.Errorf("upstream_id is invalid")
	}
	return id, nil
}

func validatePattern(pattern string, allowWildcard bool) error {
	if err := rejectQueryOrFragment(pattern); err != nil {
		return err
	}
	if pattern == "" || !strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("path pattern must start with /, for example /v1/items")
	}
	if len(pattern) > maxPatternLength {
		return fmt.Errorf("path pattern is too long")
	}
	if pattern == "/_admin" || strings.HasPrefix(pattern, "/_admin/") || pattern == "/_api" || strings.HasPrefix(pattern, "/_api/") {
		return fmt.Errorf("path pattern must not use the reserved /_admin or /_api prefix")
	}
	for _, r := range pattern {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("path pattern contains a control character")
		}
	}
	if pattern == "/" {
		return nil
	}
	parts := strings.Split(pattern, "/")
	for i := 1; i < len(parts); i++ {
		seg := parts[i]
		if seg == "" {
			if i == len(parts)-1 && !strings.Contains(pattern, "*") {
				continue
			}
			return fmt.Errorf("path pattern has an empty segment")
		}
		if seg == "*" {
			if !allowWildcard {
				return fmt.Errorf("path pattern must be an exact path")
			}
			continue
		}
		if strings.ContainsAny(seg, "*?") {
			return fmt.Errorf("path pattern must use * only as a whole segment")
		}
	}
	return nil
}

func rejectQueryOrFragment(pattern string) error {
	i := strings.IndexAny(pattern, "?#")
	if i < 0 {
		return nil
	}
	path := pattern[:i]
	if path == "" {
		path = "/"
	}
	kind := "a query string"
	if pattern[i] == '#' {
		kind = "a fragment"
	}
	return fmt.Errorf("path pattern can't include %s. Use %s. Rules match the path only, so any query matches.", kind, path)
}

func normalizeHeaders(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	if len(in) > 64 {
		return nil, fmt.Errorf("a rule may have at most 64 headers")
	}
	for name, value := range in {
		if len(name) == 0 || len(name) > 128 || !validHeaderToken(name) {
			return nil, fmt.Errorf("invalid header name %q", name)
		}
		if len(value) > 4096 || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("invalid header value for %s", name)
		}
		canon := http.CanonicalHeaderKey(name)
		if _, skip := hopByHop[canon]; skip {
			return nil, fmt.Errorf("header %s is hop-by-hop and cannot be saved", canon)
		}
		if _, exists := out[canon]; exists {
			return nil, fmt.Errorf("duplicate header %s", canon)
		}
		out[canon] = value
	}
	return out, nil
}

func validHeaderToken(name string) bool {
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c)):
		default:
			return false
		}
	}
	return true
}

func normalizeBody(raw string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	if len(trimmed) > config.MaxMockBody {
		return nil, fmt.Errorf("mock body must be 1 MiB or smaller")
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("mock body must be empty or JSON")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, []byte(trimmed)); err != nil {
		return nil, fmt.Errorf("mock body must be empty or JSON")
	}
	if buf.String() == "null" {
		return nil, nil
	}
	return json.RawMessage(buf.Bytes()), nil
}

var apiNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// NormalizeAPIName checks the optional stable name.
// An empty name is allowed. A set name is case-sensitive and matches [a-z0-9][a-z0-9._-]{0,63}.
func NormalizeAPIName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", nil
	}
	if !apiNamePattern.MatchString(name) {
		return "", fmt.Errorf("name must match [a-z0-9][a-z0-9._-]{0,63}")
	}
	return name, nil
}

func cloneRule(rule Rule) Rule {
	if rule.Headers != nil {
		headers := make(map[string]string, len(rule.Headers))
		for key, value := range rule.Headers {
			headers[key] = value
		}
		rule.Headers = headers
	}
	if rule.Body != nil {
		body := make([]byte, len(rule.Body))
		copy(body, rule.Body)
		rule.Body = body
	}
	rule.Match = cloneMatch(rule.Match)
	return rule
}
