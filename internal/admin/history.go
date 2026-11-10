package admin

import (
	_ "embed"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
)

//go:embed history.js
var historyJS []byte

type historyPageData struct {
	Title           string
	Entries         []historyRow
	Entry           *historyDetail
	Error           string
	FilterKey       string
	FilterLabel     string
	FilterQuery     template.URL
	SourceAll       template.URL
	SourceMock      template.URL
	SourceReal      template.URL
	UpstreamKey     string
	UpstreamLabel   string
	UpstreamChoices []historyUpstreamChoice
	Shown           int
	Total           int
}

type historyUpstreamChoice struct {
	ID       string
	Label    string
	Selected bool
}

type historyRow struct {
	ID       string
	Method   string
	URL      string
	Status   int
	Source   string
	Upstream string
	Delay    string
	When     string
}

type historyDetail struct {
	historyRow
	Forwarded         string
	RuleID            string
	RequestHeaders    []headerLine
	ResponseHeaders   []headerLine
	RequestBody       string
	ResponseBody      string
	RequestTruncated  bool
	ResponseTruncated bool
}

type headerLine struct {
	Name  string
	Value string
}

func (s *Server) historyPage(w http.ResponseWriter, r *http.Request) {
	shown, data := s.historyView(r)
	data.Title = "History"
	data.Entries = make([]historyRow, 0, len(shown))
	for _, entry := range shown {
		data.Entries = append(data.Entries, historyRowFrom(entry))
	}
	s.renderHistory(w, "history", data, http.StatusOK, true)
}

func (s *Server) clearConfirm(w http.ResponseWriter, r *http.Request) {
	_, data := s.historyView(r)
	data.Title = "Clear history"
	s.renderHistory(w, "history_clear", data, http.StatusOK, false)
}

func (s *Server) clearHistory(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if s.history != nil {
		s.history.Clear()
	}
	http.Redirect(w, r, historyReturnPath(r), http.StatusSeeOther)
}

func (s *Server) historyRoute(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || !validID(parts[0]) {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	if len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	switch {
	case action == "" && r.Method == http.MethodGet:
		s.historyDetail(w, r, id)
	case action == "delete" && r.Method == http.MethodPost:
		s.deleteHistory(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) historyDetail(w http.ResponseWriter, r *http.Request, id string) {
	if s.history == nil {
		http.NotFound(w, r)
		return
	}
	entry, ok := s.history.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, data := s.historyView(r)
	row := historyRowFrom(entry)
	detail := historyDetail{
		historyRow:        row,
		Forwarded:         forwardedTarget(entry),
		RuleID:            entry.RuleID,
		RequestHeaders:    headerLines(entry.RequestHeaders),
		ResponseHeaders:   headerLines(entry.ResponseHeaders),
		RequestBody:       entry.RequestBody,
		ResponseBody:      entry.ResponseBody,
		RequestTruncated:  entry.RequestBodyTruncated,
		ResponseTruncated: entry.ResponseBodyTruncated,
	}
	data.Title = "History entry"
	data.Entry = &detail
	s.renderHistory(w, "history_detail", data, http.StatusOK, false)
}

func (s *Server) historyFeed(w http.ResponseWriter, r *http.Request) {
	shown, data := s.historyView(r)
	payload := historyFeed{
		Filter:        data.FilterKey,
		Label:         data.FilterLabel,
		Upstream:      data.UpstreamKey,
		UpstreamLabel: data.UpstreamLabel,
		Shown:         data.Shown,
		Total:         data.Total,
		Entries:       make([]historyFeedRow, 0, len(shown)),
	}
	for _, entry := range shown {
		row := historyRowFrom(entry)
		payload.Entries = append(payload.Entries, historyFeedRow{
			ID:       row.ID,
			Method:   row.Method,
			URL:      row.URL,
			Status:   row.Status,
			Source:   row.Source,
			Upstream: row.Upstream,
			Delay:    row.Delay,
			When:     row.When,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) historyScript(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(historyJS)
}

func (s *Server) deleteHistory(w http.ResponseWriter, r *http.Request, id string) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if s.history == nil || !s.history.Delete(id) {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, historyReturnPath(r), http.StatusSeeOther)
}

func (s *Server) historyView(r *http.Request) ([]history.Entry, historyPageData) {
	data := historyPageData{FilterKey: "all", FilterLabel: "All", UpstreamLabel: "All upstreams"}
	var entries []history.Entry
	if s.history != nil {
		entries = s.history.List()
	}
	data.Total = len(entries)
	sourceKey := ""
	if source, ok := history.ParseSource(r.URL.Query().Get("source")); ok {
		sourceKey = string(source)
		data.FilterKey = sourceKey
		data.FilterLabel = historyFilterLabel(source)
	}
	upstreamKey := ""
	if upstream, ok := history.ParseUpstream(r.URL.Query().Get("upstream")); ok {
		upstreamKey = upstream
		data.UpstreamKey = upstreamKey
		data.UpstreamLabel = s.upstreamFilterLabel(entries, upstreamKey)
	}
	shown := entries
	if sourceKey != "" {
		shown = history.Filter(shown, history.Source(sourceKey))
	}
	if upstreamKey != "" {
		shown = history.FilterUpstream(shown, upstreamKey)
	}
	data.Shown = len(shown)
	data.SourceAll = template.URL(historyQueryString("", upstreamKey))
	data.SourceMock = template.URL(historyQueryString(string(history.SourceMock), upstreamKey))
	data.SourceReal = template.URL(historyQueryString(string(history.SourceUpstream), upstreamKey))
	data.FilterQuery = template.URL(historyQueryString(sourceKey, upstreamKey))
	data.UpstreamChoices = s.upstreamFilterChoices(entries, upstreamKey)
	return shown, data
}

func historyReturnPath(r *http.Request) string {
	sourceKey := ""
	if source, ok := history.ParseSource(r.URL.Query().Get("source")); ok {
		sourceKey = string(source)
	}
	upstreamKey := ""
	if upstream, ok := history.ParseUpstream(r.URL.Query().Get("upstream")); ok {
		upstreamKey = upstream
	}
	query := historyQueryString(sourceKey, upstreamKey)
	if query == "" {
		return "/_admin/history"
	}
	return "/_admin/history" + query
}

func historyQueryString(source, upstream string) string {
	values := url.Values{}
	if source != "" {
		values.Set("source", source)
	}
	if upstream != "" {
		values.Set("upstream", upstream)
	}
	encoded := values.Encode()
	if encoded == "" {
		return ""
	}
	return "?" + encoded
}

func (s *Server) upstreamFilterLabel(entries []history.Entry, id string) string {
	if id == history.UpstreamAny {
		return "Any upstream"
	}
	if s.upstreams != nil {
		if item, ok := s.upstreams.Get(id); ok {
			return item.Name
		}
	}
	for _, entry := range entries {
		if entry.UpstreamID == id && entry.UpstreamName != "" && entry.UpstreamName != history.UpstreamAny {
			return entry.UpstreamName + " (deleted)"
		}
	}
	return "Deleted upstream"
}

func (s *Server) upstreamFilterChoices(entries []history.Entry, selected string) []historyUpstreamChoice {
	choices := []historyUpstreamChoice{{
		ID:       history.UpstreamAny,
		Label:    "Any upstream (mock rule not linked)",
		Selected: selected == history.UpstreamAny,
	}}
	seen := map[string]bool{history.UpstreamAny: true}
	if s.upstreams != nil {
		for _, item := range s.upstreams.List() {
			seen[item.ID] = true
			choices = append(choices, historyUpstreamChoice{
				ID:       item.ID,
				Label:    item.Name,
				Selected: item.ID == selected,
			})
		}
	}
	if selected != "" && !seen[selected] {
		choices = append(choices, historyUpstreamChoice{
			ID:       selected,
			Label:    s.upstreamFilterLabel(entries, selected),
			Selected: true,
		})
	}
	return choices
}

func historyFilterLabel(source history.Source) string {
	switch source {
	case history.SourceMock:
		return "Mocked"
	case history.SourceUpstream:
		return "Real server"
	default:
		return string(source)
	}
}

func (s *Server) renderHistory(w http.ResponseWriter, name string, data historyPageData, status int, live bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	csp := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	if live {
		csp = "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	}
	w.Header().Set("Content-Security-Policy", csp)
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// historyTimeLayout is local clock time plus a numeric offset.
// Abbreviations from the layout "MST" are not stable across operating systems.
// Asia/Bangkok is "+07" on macOS and "ICT" in some other zone databases.
const historyTimeLayout = "2006-01-02 15:04:05 -07:00"

func formatHistoryTime(t time.Time) string {
	return t.In(time.Local).Format(historyTimeLayout)
}

func historyRowFrom(entry history.Entry) historyRow {
	target := entry.Path
	if entry.RawQuery != "" {
		target += "?" + entry.RawQuery
	}
	return historyRow{
		ID:       entry.ID,
		Method:   entry.Method,
		URL:      target,
		Status:   entry.Status,
		Source:   historySourceLabel(entry),
		Upstream: entry.UpstreamName,
		Delay:    delayLabel(entry.DelayMS),
		When:     formatHistoryTime(entry.StartedAt),
	}
}

func forwardedTarget(entry history.Entry) string {
	path := entry.ForwardedPath
	if path == "" {
		path = entry.Path
	}
	if entry.RawQuery != "" {
		path += "?" + entry.RawQuery
	}
	return path
}

func historySourceLabel(entry history.Entry) string {
	if entry.RuleID != "" && (entry.Source == history.SourceUpstream || entry.Source == history.SourceUpstreamError) {
		return string(entry.Source) + " (fresh rule)"
	}
	return string(entry.Source)
}

type historyFeed struct {
	Filter        string           `json:"filter"`
	Label         string           `json:"label"`
	Upstream      string           `json:"upstream"`
	UpstreamLabel string           `json:"upstream_label"`
	Shown         int              `json:"shown"`
	Total         int              `json:"total"`
	Entries       []historyFeedRow `json:"entries"`
}

type historyFeedRow struct {
	ID       string `json:"id"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Status   int    `json:"status"`
	Source   string `json:"source"`
	Upstream string `json:"upstream"`
	Delay    string `json:"delay"`
	When     string `json:"when"`
}

func headerLines(header map[string][]string) []headerLine {
	keys := make([]string, 0, len(header))
	for key := range header {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var lines []headerLine
	for _, key := range keys {
		for _, value := range header[key] {
			lines = append(lines, headerLine{Name: key, Value: value})
		}
	}
	return lines
}
