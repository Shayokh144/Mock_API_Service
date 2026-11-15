package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

type resetBody struct {
	Rules   *bool `json:"rules"`
	History *bool `json:"history"`
}

type historyItem struct {
	ID              string              `json:"id"`
	Time            string              `json:"time"`
	Method          string              `json:"method"`
	Path            string              `json:"path"`
	Query           string              `json:"query"`
	Upstream        string              `json:"upstream"`
	Status          int                 `json:"status"`
	DurationMS      int                 `json:"duration_ms"`
	Mocked          bool                `json:"mocked"`
	RuleID          string              `json:"rule_id,omitempty"`
	RuleName        string              `json:"rule_name,omitempty"`
	RequestHeaders  map[string][]string `json:"request_headers"`
	RequestBody     string              `json:"request_body"`
	ResponseHeaders map[string][]string `json:"response_headers"`
	ResponseBody    string              `json:"response_body"`
	Truncated       bool                `json:"truncated"`
}

func (s *Server) resetSession(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	info, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	if id == sessions.DefaultID && r.URL.Query().Get("confirm") != sessions.DefaultID {
		writeError(w, http.StatusConflict, "conflict", "reset of the default session needs ?confirm=default")
		return
	}
	var body resetBody
	if err := readJSON(w, r, &body); err != nil {
		return
	}
	clearRules := body.Rules == nil || *body.Rules
	clearHistory := body.History == nil || *body.History
	if clearRules && info.Rules != nil {
		if err := info.Rules.Clear(); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "could not reset rules")
			return
		}
	}
	if clearHistory && info.History != nil {
		info.History.Clear()
	}
	s.sessions.Touch(id)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) historyRoute(w http.ResponseWriter, r *http.Request, id string) {
	info, ok := s.sessions.Get(id)
	if !ok || info.History == nil {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	s.sessions.Touch(id)
	switch r.Method {
	case http.MethodGet:
		s.listHistory(w, r, info)
	case http.MethodDelete:
		if err := rejectBody(w, r); err != nil {
			return
		}
		info.History.Clear()
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) listHistory(w http.ResponseWriter, r *http.Request, info sessions.Info) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	query := r.URL.Query()
	limit := 100
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			writeError(w, http.StatusBadRequest, "bad_request", "limit must be from 1 through 500")
			return
		}
		limit = parsed
	}
	mocked, mockedSet, ok := parseBoolQuery(query.Get("mocked"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "mocked must be true or false")
		return
	}
	status, statusSet := 0, false
	if raw := query.Get("status"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "status must be a number")
			return
		}
		status, statusSet = parsed, true
	}
	since := strings.TrimSpace(query.Get("since"))
	sinceTime, sinceIsTime := time.Time{}, false
	if since != "" {
		if parsed, err := time.Parse(time.RFC3339, since); err == nil {
			sinceTime, sinceIsTime = parsed, true
		}
	}
	reveal := query.Get("reveal") == "1"
	entries := info.History.List()
	if since != "" && !sinceIsTime {
		found := false
		for i, entry := range entries {
			if entry.ID == since {
				entries = entries[:i]
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusBadRequest, "bad_request", "since must be an RFC3339 time or an entry id")
			return
		}
	}
	var out []historyItem
	for _, entry := range entries {
		if sinceIsTime && !entry.StartedAt.After(sinceTime) {
			continue
		}
		if !historyMatch(entry, info, query.Get("method"), query.Get("path"), query.Get("path_exact"), query.Get("upstream"), query.Get("rule_id"), query.Get("rule_name"), status, statusSet, mocked, mockedSet) {
			continue
		}
		out = append(out, toHistoryItem(entry, info, reveal))
		if len(out) >= limit {
			break
		}
	}
	if out == nil {
		out = []historyItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func historyMatch(entry history.Entry, info sessions.Info, method, path, exact, upstream, ruleID, ruleName string, status int, statusSet, mocked, mockedSet bool) bool {
	if method != "" && !strings.EqualFold(entry.Method, method) {
		return false
	}
	if path != "" && !strings.Contains(entry.Path, path) {
		return false
	}
	if exact != "" && entry.Path != exact {
		return false
	}
	if upstream != "" && !upstreamMatches(entry, upstream) {
		return false
	}
	if statusSet && entry.Status != status {
		return false
	}
	isMock := entry.Source == history.SourceMock
	if mockedSet && isMock != mocked {
		return false
	}
	if ruleID != "" && entry.RuleID != ruleID {
		return false
	}
	if ruleName != "" {
		rule, ok := info.Rules.FindByAPIName(ruleName)
		if !ok || rule.ID != entry.RuleID {
			return false
		}
	}
	return true
}

func upstreamMatches(entry history.Entry, want string) bool {
	want = strings.TrimSpace(want)
	if strings.EqualFold(want, "any") {
		return entry.UpstreamID == "" || entry.UpstreamName == history.UpstreamAny
	}
	return entry.UpstreamID == want || entry.UpstreamName == want
}

func toHistoryItem(entry history.Entry, info sessions.Info, reveal bool) historyItem {
	name := ""
	if entry.RuleID != "" && info.Rules != nil {
		if rule, ok := info.Rules.Get(entry.RuleID); ok {
			name = rule.APIName
		}
	}
	upstream := entry.UpstreamName
	if upstream == "" {
		upstream = "Any"
	}
	return historyItem{
		ID:              entry.ID,
		Time:            entry.StartedAt.Format(time.RFC3339),
		Method:          entry.Method,
		Path:            entry.Path,
		Query:           entry.RawQuery,
		Upstream:        upstream,
		Status:          entry.Status,
		DurationMS:      entry.DurationMS,
		Mocked:          entry.Source == history.SourceMock,
		RuleID:          entry.RuleID,
		RuleName:        name,
		RequestHeaders:  maskHeaders(entry.RequestHeaders, reveal),
		RequestBody:     entry.RequestBody,
		ResponseHeaders: maskHeaders(entry.ResponseHeaders, reveal),
		ResponseBody:    entry.ResponseBody,
		Truncated:       entry.RequestBodyTruncated || entry.ResponseBodyTruncated,
	}
}

func maskHeaders(header http.Header, reveal bool) map[string][]string {
	out := map[string][]string{}
	for key, values := range header {
		if !reveal && secretHistoryHeader(key) {
			masked := make([]string, len(values))
			for i := range masked {
				masked[i] = "••••••••"
			}
			out[key] = masked
			continue
		}
		out[key] = append([]string(nil), values...)
	}
	return out
}

func secretHistoryHeader(name string) bool {
	switch http.CanonicalHeaderKey(name) {
	case "Authorization", "Cookie":
		return true
	default:
		return false
	}
}

func parseBoolQuery(raw string) (bool, bool, bool) {
	switch raw {
	case "":
		return false, false, true
	case "true":
		return true, true, true
	case "false":
		return false, true, true
	default:
		return false, false, false
	}
}
