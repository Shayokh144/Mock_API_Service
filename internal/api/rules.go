package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

const maxRulesPerSession = 500

type ruleBody struct {
	Description  string          `json:"description"`
	Upstream     string          `json:"upstream"`
	Method       string          `json:"method"`
	Path         string          `json:"path"`
	Query        []string        `json:"query_conditions"`
	BodyMatch    []string        `json:"body_conditions"`
	Status       int             `json:"status"`
	Headers      json.RawMessage `json:"headers"`
	Body         json.RawMessage `json:"body"`
	RawResponse  string          `json:"raw_response"`
	DelayMS      int             `json:"delay_ms"`
	Enabled      *bool           `json:"enabled"`
	FreshRequest bool            `json:"fresh_request"`
	MockHeader   bool            `json:"mock_header"`
}

type ruleResponse struct {
	ID           string            `json:"id"`
	Description  string            `json:"description"`
	Upstream     string            `json:"upstream"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Query        []string          `json:"query_conditions"`
	BodyMatch    []string          `json:"body_conditions"`
	Status       int               `json:"status"`
	Headers      map[string]string `json:"headers"`
	Body         json.RawMessage   `json:"body"`
	RawResponse  string            `json:"raw_response,omitempty"`
	DelayMS      int               `json:"delay_ms"`
	Enabled      bool              `json:"enabled"`
	FreshRequest bool              `json:"fresh_request"`
	MockHeader   bool              `json:"mock_header,omitempty"`
}

func (s *Server) rulesRoute(w http.ResponseWriter, r *http.Request, sessionID, rest string) {
	info, ok := s.sessions.Get(sessionID)
	if !ok || info.Rules == nil {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	s.sessions.Touch(sessionID)
	rest = strings.Trim(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			s.listRules(w, r, info)
		case http.MethodPost:
			s.createRule(w, r, info)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	id, action, _ := strings.Cut(rest, "/")
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.getRule(w, info, id)
	case action == "" && r.Method == http.MethodPut:
		s.putRule(w, r, info, id)
	case action == "" && r.Method == http.MethodDelete:
		s.deleteRule(w, info, id)
	case (action == "enable" || action == "disable") && r.Method == http.MethodPost:
		s.setRuleEnabled(w, r, info, id, action == "enable")
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown API path")
	}
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request, info sessions.Info) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	key := strings.TrimSpace(r.URL.Query().Get("upstream"))
	namedID, label, ok := upstreamFilter(s.sessions.Upstreams(), key)
	if key != "" && !ok {
		writeJSON(w, http.StatusOK, map[string]any{"rules": []ruleResponse{}, "upstream": "All upstreams"})
		return
	}
	var out []ruleResponse
	for _, rule := range info.Rules.List() {
		if !ruleVisible(rule, key, namedID) {
			continue
		}
		out = append(out, toRuleResponse(rule, s.sessions.Upstreams()))
	}
	if out == nil {
		out = []ruleResponse{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": out, "upstream": label})
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request, info sessions.Info) {
	if len(info.Rules.List()) >= maxRulesPerSession {
		writeError(w, http.StatusTooManyRequests, "too_many", "a session can hold 500 rules")
		return
	}
	draft, err := s.draftFromJSON(w, r)
	if err != nil {
		return
	}
	saved, err := info.Rules.Create(draft)
	if err != nil {
		writeRuleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toRuleResponse(saved, s.sessions.Upstreams()))
}

func (s *Server) getRule(w http.ResponseWriter, info sessions.Info, id string) {
	rule, ok := info.Rules.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	writeJSON(w, http.StatusOK, toRuleResponse(rule, s.sessions.Upstreams()))
}

func (s *Server) putRule(w http.ResponseWriter, r *http.Request, info sessions.Info, id string) {
	draft, err := s.draftFromJSON(w, r)
	if err != nil {
		return
	}
	saved, err := info.Rules.Update(id, draft)
	if err != nil {
		writeRuleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRuleResponse(saved, s.sessions.Upstreams()))
}

func (s *Server) deleteRule(w http.ResponseWriter, info sessions.Info, id string) {
	if err := info.Rules.Delete(id); err != nil {
		writeRuleError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setRuleEnabled(w http.ResponseWriter, r *http.Request, info sessions.Info, id string, enabled bool) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	rule, ok := info.Rules.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
		return
	}
	if rule.Enabled == enabled {
		writeJSON(w, http.StatusOK, toRuleResponse(rule, s.sessions.Upstreams()))
		return
	}
	draft := draftFromRule(rule)
	draft.Enabled = enabled
	saved, err := info.Rules.Update(id, draft)
	if err != nil {
		writeRuleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRuleResponse(saved, s.sessions.Upstreams()))
}

func (s *Server) draftFromJSON(w http.ResponseWriter, r *http.Request) (rules.Draft, error) {
	var body ruleBody
	if err := readJSON(w, r, &body); err != nil {
		return rules.Draft{}, err
	}
	upstreamID, err := resolveUpstream(s.sessions.Upstreams(), body.Upstream)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return rules.Draft{}, errStop
	}
	query, err := rules.ParseConditions(strings.Join(body.Query, "\n"), "query")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return rules.Draft{}, errStop
	}
	matchBody, err := rules.ParseConditions(strings.Join(body.BodyMatch, "\n"), "body")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return rules.Draft{}, errStop
	}
	headers, err := parseHeaderField(body.Headers)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return rules.Draft{}, errStop
	}
	mode := ""
	raw := strings.TrimSpace(body.RawResponse)
	payload := ""
	if raw != "" {
		mode = rules.ModeRaw
	} else {
		payload, err = parseBodyField(body.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return rules.Draft{}, errStop
		}
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	return rules.Draft{
		Name:         body.Description,
		Enabled:      enabled,
		FreshRequest: body.FreshRequest,
		UpstreamID:   upstreamID,
		Method:       body.Method,
		PathPattern:  body.Path,
		Match:        rules.MatchSpec{Query: query, Body: matchBody},
		StatusCode:   body.Status,
		DelayMS:      body.DelayMS,
		ResponseMode: mode,
		RawResponse:  raw,
		MockHeader:   body.MockHeader,
		Headers:      headers,
		Body:         payload,
	}, nil
}

func draftFromRule(rule rules.Rule) rules.Draft {
	body := ""
	if len(rule.Body) > 0 && string(rule.Body) != "null" {
		body = string(rule.Body)
	}
	return rules.Draft{
		Name:         rule.Name,
		Enabled:      rule.Enabled,
		FreshRequest: rule.FreshRequest,
		UpstreamID:   rule.UpstreamID,
		Method:       rule.Method,
		PathPattern:  rule.PathPattern,
		Match:        rule.Match,
		StatusCode:   rule.StatusCode,
		DelayMS:      rule.DelayMS,
		ResponseMode: rule.ResponseMode,
		RawResponse:  rule.RawResponse,
		MockHeader:   rule.MockHeader,
		Headers:      rule.Headers,
		Body:         body,
	}
}

func toRuleResponse(rule rules.Rule, ups *upstreams.Store) ruleResponse {
	headers := rule.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	body := rule.Body
	if len(body) == 0 {
		body = json.RawMessage("null")
	}
	query := conditionLines(rule.Match.Query)
	matchBody := conditionLines(rule.Match.Body)
	return ruleResponse{
		ID:           rule.ID,
		Description:  rule.Name,
		Upstream:     upstreamName(ups, rule.UpstreamID),
		Method:       rule.Method,
		Path:         rule.PathPattern,
		Query:        query,
		BodyMatch:    matchBody,
		Status:       rule.StatusCode,
		Headers:      headers,
		Body:         body,
		RawResponse:  rule.RawResponse,
		DelayMS:      rule.DelayMS,
		Enabled:      rule.Enabled,
		FreshRequest: rule.FreshRequest,
		MockHeader:   rule.MockHeader,
	}
}

func conditionLines(list []rules.Condition) []string {
	text := rules.FormatConditions(list)
	if strings.TrimSpace(text) == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func upstreamName(ups *upstreams.Store, id string) string {
	if id == "" {
		return "Any"
	}
	if ups == nil {
		return id
	}
	item, ok := ups.Get(id)
	if !ok {
		return id
	}
	return item.Name
}

func resolveUpstream(ups *upstreams.Store, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "any") {
		return "", nil
	}
	if ups == nil {
		return "", errors.New("choose an upstream or Any upstream")
	}
	for _, item := range ups.List() {
		if item.Name == name {
			return item.ID, nil
		}
	}
	return "", errors.New("choose an upstream or Any upstream")
}

func upstreamFilter(ups *upstreams.Store, key string) (string, string, bool) {
	switch {
	case key == "":
		return "", "All upstreams", true
	case strings.EqualFold(key, "any"):
		return "", "Any", true
	default:
		id, err := resolveUpstream(ups, key)
		if err != nil {
			return "", "", false
		}
		return id, key, true
	}
}

func ruleVisible(rule rules.Rule, key, namedID string) bool {
	switch {
	case key == "":
		return true
	case strings.EqualFold(key, "any"):
		return rule.UpstreamID == ""
	default:
		return rule.UpstreamID == namedID || rule.UpstreamID == ""
	}
}

func parseHeaderField(raw json.RawMessage) (map[string]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var object map[string]string
	if err := json.Unmarshal(raw, &object); err == nil {
		return object, nil
	}
	var lines []string
	if err := json.Unmarshal(raw, &lines); err == nil {
		out := map[string]string{}
		for _, line := range lines {
			name, value, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(name) == "" {
				return nil, errors.New("headers must be Name: value")
			}
			out[strings.TrimSpace(name)] = strings.TrimSpace(value)
		}
		return out, nil
	}
	var pairs []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &pairs); err == nil {
		out := map[string]string{}
		for _, pair := range pairs {
			if strings.TrimSpace(pair.Name) == "" {
				return nil, errors.New("headers need a name")
			}
			out[pair.Name] = pair.Value
		}
		return out, nil
	}
	return nil, errors.New("headers must be an object or a list of pairs")
}

func parseBodyField(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	if trimmed[0] == '"' {
		var text string
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return "", errors.New("body must be JSON")
		}
		return text, nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, trimmed); err != nil {
		return "", errors.New("body must be JSON")
	}
	return buf.String(), nil
}

func writeRuleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rules.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "rule not found")
	case errors.Is(err, rules.ErrDuplicate):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	}
}
