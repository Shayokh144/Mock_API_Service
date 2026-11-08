package admin

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
)

type historyPageData struct {
	Title   string
	Entries []historyRow
	Entry   *historyDetail
	Error   string
}

type historyRow struct {
	ID     string
	Method string
	URL    string
	Status int
	Source string
	When   string
}

type historyDetail struct {
	historyRow
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
	rows := []historyRow{}
	if s.history != nil {
		for _, entry := range s.history.List() {
			rows = append(rows, historyRowFrom(entry))
		}
	}
	s.renderHistory(w, "history", historyPageData{Title: "History", Entries: rows}, http.StatusOK)
}

func (s *Server) clearConfirm(w http.ResponseWriter, r *http.Request) {
	s.renderHistory(w, "history_clear", historyPageData{Title: "Clear history"}, http.StatusOK)
}

func (s *Server) clearHistory(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if s.history != nil {
		s.history.Clear()
	}
	http.Redirect(w, r, "/_admin/history", http.StatusSeeOther)
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
	row := historyRowFrom(entry)
	detail := historyDetail{
		historyRow:        row,
		RuleID:            entry.RuleID,
		RequestHeaders:    headerLines(entry.RequestHeaders),
		ResponseHeaders:   headerLines(entry.ResponseHeaders),
		RequestBody:       entry.RequestBody,
		ResponseBody:      entry.ResponseBody,
		RequestTruncated:  entry.RequestBodyTruncated,
		ResponseTruncated: entry.ResponseBodyTruncated,
	}
	s.renderHistory(w, "history_detail", historyPageData{Title: "History entry", Entry: &detail}, http.StatusOK)
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
	http.Redirect(w, r, "/_admin/history", http.StatusSeeOther)
}

func (s *Server) renderHistory(w http.ResponseWriter, name string, data historyPageData, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
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
		ID:     entry.ID,
		Method: entry.Method,
		URL:    target,
		Status: entry.Status,
		Source: string(entry.Source),
		When:   formatHistoryTime(entry.StartedAt),
	}
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
