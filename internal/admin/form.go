package admin

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

var methods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodHead,
	http.MethodOptions,
}

type formView struct {
	Action       string
	Submit       string
	Name         string
	Enabled      bool
	FreshRequest bool
	UpstreamID   string
	Method       string
	PathPattern  string
	Query        string
	BodyMatch    string
	StatusCode   string
	DelayMS      string
	ResponseMode string
	RawResponse  string
	MockHeader   bool
	Headers      string
	Body         string
	SecretHint   string
}

func draftFromForm(r *http.Request) (rules.Draft, formView, error) {
	if err := r.ParseForm(); err != nil {
		return rules.Draft{}, formView{}, fmt.Errorf("could not read the form")
	}
	view := formView{
		Name:         r.FormValue("name"),
		Enabled:      r.FormValue("enabled") == "on",
		FreshRequest: r.FormValue("fresh_request") == "on",
		UpstreamID:   strings.TrimSpace(r.FormValue("upstream_id")),
		Method:       strings.ToUpper(strings.TrimSpace(r.FormValue("method"))),
		PathPattern:  strings.TrimSpace(r.FormValue("path_pattern")),
		Query:        r.FormValue("query_conditions"),
		BodyMatch:    r.FormValue("body_conditions"),
		StatusCode:   strings.TrimSpace(r.FormValue("status_code")),
		DelayMS:      strings.TrimSpace(r.FormValue("delay_ms")),
		ResponseMode: r.FormValue("response_mode"),
		RawResponse:  r.FormValue("raw_response"),
		MockHeader:   r.FormValue("mock_header") == "on",
		Headers:      r.FormValue("headers"),
		Body:         r.FormValue("body"),
	}
	rawMode := strings.EqualFold(strings.TrimSpace(view.ResponseMode), rules.ModeRaw)
	status := 200
	var headers map[string]string
	body := ""
	if !rawMode {
		parsedStatus, err := strconv.Atoi(view.StatusCode)
		if err != nil {
			return rules.Draft{}, view, fmt.Errorf("status_code must be from 100 through 599")
		}
		status = parsedStatus
		parsedHeaders, err := parseHeaderLines(view.Headers)
		if err != nil {
			return rules.Draft{}, view, err
		}
		headers = parsedHeaders
		body = view.Body
	}
	delay, err := parseDelayMS(view.DelayMS)
	if err != nil {
		return rules.Draft{}, view, err
	}
	query, err := rules.ParseConditions(view.Query, "query")
	if err != nil {
		return rules.Draft{}, view, err
	}
	bodyMatch, err := rules.ParseConditions(view.BodyMatch, "body")
	if err != nil {
		return rules.Draft{}, view, err
	}
	return rules.Draft{
		Name:         view.Name,
		Enabled:      view.Enabled,
		FreshRequest: view.FreshRequest,
		UpstreamID:   view.UpstreamID,
		Method:       view.Method,
		PathPattern:  view.PathPattern,
		Match:        rules.MatchSpec{Query: query, Body: bodyMatch},
		StatusCode:   status,
		DelayMS:      delay,
		ResponseMode: view.ResponseMode,
		RawResponse:  view.RawResponse,
		MockHeader:   view.MockHeader && rawMode,
		Headers:      headers,
		Body:         body,
	}, view, nil
}

func (formView) RawPlaceholder() string {
	return "HTTP/1.1 401 Unauthorized\nContent-Type: application/json\n\n{\"error\":\"unauthorized\"}"
}

func parseDelayMS(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	delay, err := strconv.Atoi(raw)
	if err != nil || delay < 0 || delay > 60000 {
		return 0, fmt.Errorf("delay_ms must be from 0 through 60000")
	}
	return delay, nil
}

func modeLabel(mode string) string {
	if mode == rules.ModeRaw {
		return "Raw"
	}
	return "Fields"
}

func delayLabel(ms int) string {
	if ms <= 0 {
		return ""
	}
	return strconv.Itoa(ms) + " ms"
}

func parseHeaderLines(text string) (map[string]string, error) {
	headers := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("header line %q must look like Name: value", line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" {
			return nil, fmt.Errorf("a header name is required")
		}
		headers[name] = value
	}
	return headers, nil
}

func formatHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s: %s\n", key, headers[key])
	}
	return b.String()
}

func viewFromRule(rule rules.Rule, action, submit string) formView {
	return formView{
		Action:       action,
		Submit:       submit,
		Name:         rule.Name,
		Enabled:      rule.Enabled,
		FreshRequest: rule.FreshRequest,
		UpstreamID:   rule.UpstreamID,
		Method:       rule.Method,
		PathPattern:  rule.PathPattern,
		Query:        rules.FormatConditions(rule.Match.Query),
		BodyMatch:    rules.FormatConditions(rule.Match.Body),
		StatusCode:   strconv.Itoa(rule.StatusCode),
		DelayMS:      strconv.Itoa(rule.DelayMS),
		ResponseMode: rule.ResponseMode,
		RawResponse:  rule.RawResponse,
		MockHeader:   rule.MockHeader,
		Headers:      formatHeaders(rule.Headers),
		Body:         string(rule.Body),
	}
}
