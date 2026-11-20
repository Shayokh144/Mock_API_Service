package admin

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
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
	APIName      string
	Enabled      bool
	FreshRequest bool
	UpstreamID   string
	Method       string
	PathPattern  string
	Query        string
	BodyMatch    string
	HeaderMatch  string
	StatusCode   string
	DelayMS      string
	Priority     string
	Fault        string
	MaxUses      string
	Sequence     string
	Responses    string
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
		APIName:      r.FormValue("api_name"),
		Enabled:      r.FormValue("enabled") == "on",
		FreshRequest: r.FormValue("fresh_request") == "on",
		UpstreamID:   strings.TrimSpace(r.FormValue("upstream_id")),
		Method:       strings.ToUpper(strings.TrimSpace(r.FormValue("method"))),
		PathPattern:  strings.TrimSpace(r.FormValue("path_pattern")),
		Query:        r.FormValue("query_conditions"),
		BodyMatch:    r.FormValue("body_conditions"),
		HeaderMatch:  r.FormValue("header_conditions"),
		StatusCode:   strings.TrimSpace(r.FormValue("status_code")),
		DelayMS:      strings.TrimSpace(r.FormValue("delay_ms")),
		Priority:     strings.TrimSpace(r.FormValue("priority")),
		Fault:        strings.TrimSpace(r.FormValue("fault")),
		MaxUses:      strings.TrimSpace(r.FormValue("max_uses")),
		Sequence:     strings.TrimSpace(r.FormValue("sequence")),
		Responses:    r.FormValue("responses"),
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
	priority, err := parsePriority(view.Priority)
	if err != nil {
		return rules.Draft{}, view, err
	}
	maxUses, err := parseMaxUses(view.MaxUses)
	if err != nil {
		return rules.Draft{}, view, err
	}
	responses, err := parseResponseSteps(view.Responses)
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
	headerMatch, err := rules.ParseConditions(view.HeaderMatch, "header")
	if err != nil {
		return rules.Draft{}, view, err
	}
	return rules.Draft{
		Name:         view.Name,
		APIName:      view.APIName,
		Enabled:      view.Enabled,
		FreshRequest: view.FreshRequest,
		UpstreamID:   view.UpstreamID,
		Method:       view.Method,
		PathPattern:  view.PathPattern,
		Match:        rules.MatchSpec{Query: query, Body: bodyMatch, Headers: headerMatch},
		StatusCode:   status,
		DelayMS:      delay,
		Priority:     priority,
		Fault:        view.Fault,
		MaxUses:      maxUses,
		Sequence:     view.Sequence,
		Responses:    responses,
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

func usesLabel(store *rules.Store, rule rules.Rule) string {
	if rule.MaxUses <= 0 || store == nil {
		return ""
	}
	left, limited := store.UsesLeft(rule.ID)
	if !limited {
		return ""
	}
	return strconv.Itoa(left) + " of " + strconv.Itoa(rule.MaxUses) + " left"
}

func formatResponses(steps []rules.ResponseStep) string {
	if len(steps) == 0 {
		return ""
	}
	raw, err := json.MarshalIndent(steps, "", "  ")
	if err != nil {
		return ""
	}
	return string(raw)
}

func parseMaxUses(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("max_uses must be zero or greater")
	}
	return n, nil
}

func parseResponseSteps(raw string) ([]rules.ResponseStep, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var steps []rules.ResponseStep
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return nil, fmt.Errorf("responses must be a JSON list")
	}
	return steps, nil
}

func parsePriority(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("priority must be an integer")
	}
	return n, nil
}

func parseDelayMS(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	delay, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && numErr.Err == strconv.ErrRange {
			return 0, fmt.Errorf("delay_ms is too large")
		}
		return 0, fmt.Errorf("delay_ms must be zero or greater")
	}
	if delay > int64(math.MaxInt) {
		return 0, fmt.Errorf("delay_ms is too large")
	}
	if err := rules.CheckDelayMS(int(delay)); err != nil {
		return 0, err
	}
	return int(delay), nil
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

func (f formView) NameCount() int {
	return filledCount(f.Name) + filledCount(f.APIName)
}

func (f formView) NameOpen(err string) bool {
	return f.NameCount() > 0 || mentions(err, "80 characters", "must match [a-z0-9]", "this name already exists")
}

func (f formView) UpstreamSet() bool {
	return strings.TrimSpace(f.UpstreamID) != ""
}

func (f formView) UpstreamOpen(err string) bool {
	return f.UpstreamSet() || mentions(err, "choose an upstream")
}

func (f formView) QueryCount() int { return filledLines(f.Query) }

func (f formView) QueryOpen(err string) bool {
	return f.QueryCount() > 0 || mentions(err, "query condition")
}

func (f formView) HeaderMatchCount() int { return filledLines(f.HeaderMatch) }

func (f formView) HeaderMatchOpen(err string) bool {
	return f.HeaderMatchCount() > 0 || mentions(err, "header condition")
}

func (f formView) BodyMatchCount() int { return filledLines(f.BodyMatch) }

func (f formView) BodyMatchOpen(err string) bool {
	return f.BodyMatchCount() > 0 || mentions(err, "body condition")
}

func (f formView) TimingCount() int {
	n := 0
	if nonzeroField(f.Priority) {
		n++
	}
	if nonzeroField(f.MaxUses) {
		n++
	}
	if strings.EqualFold(strings.TrimSpace(f.Sequence), "loop") {
		n++
	}
	if strings.TrimSpace(f.Responses) != "" {
		n++
	}
	if strings.TrimSpace(f.Fault) != "" {
		n++
	}
	if nonzeroField(f.DelayMS) {
		n++
	}
	return n
}

func (f formView) TimingOpen(err string) bool {
	return f.TimingCount() > 0 || mentions(err, "delay_ms", "priority", "max_uses", "responses", "sequence", "fault", "reset or hang")
}

func (f formView) HeaderCount() int { return filledLines(f.Headers) }

func (f formView) HeadersOpen(err string) bool {
	return f.HeaderCount() > 0 || mentions(err, "header line", "header name")
}

func filledCount(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	return 1
}

func filledLines(raw string) int {
	n := 0
	for _, line := range strings.Split(raw, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func nonzeroField(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return true
	}
	return n != 0
}

func mentions(err string, parts ...string) bool {
	if strings.TrimSpace(err) == "" {
		return false
	}
	err = strings.ToLower(err)
	for _, part := range parts {
		if strings.Contains(err, strings.ToLower(part)) {
			return true
		}
	}
	return false
}

func viewFromRule(rule rules.Rule, action, submit string) formView {
	return formView{
		Action:       action,
		Submit:       submit,
		Name:         rule.Name,
		APIName:      rule.APIName,
		Enabled:      rule.Enabled,
		FreshRequest: rule.FreshRequest,
		UpstreamID:   rule.UpstreamID,
		Method:       rule.Method,
		PathPattern:  rule.PathPattern,
		Query:        rules.FormatConditions(rule.Match.Query),
		BodyMatch:    rules.FormatConditions(rule.Match.Body),
		HeaderMatch:  rules.FormatConditions(rule.Match.Headers),
		StatusCode:   strconv.Itoa(rule.StatusCode),
		DelayMS:      strconv.Itoa(rule.DelayMS),
		Priority:     strconv.Itoa(rule.Priority),
		Fault:        rule.Fault,
		MaxUses:      strconv.Itoa(rule.MaxUses),
		Sequence:     rule.Sequence,
		Responses:    formatResponses(rule.Responses),
		ResponseMode: rule.ResponseMode,
		RawResponse:  rule.RawResponse,
		MockHeader:   rule.MockHeader,
		Headers:      formatHeaders(rule.Headers),
		Body:         string(rule.Body),
	}
}
