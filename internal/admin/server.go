// Package admin serves the local browser dashboard under /_admin.
package admin

import (
	"embed"
	"encoding/hex"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/history"
	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/scenario"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

//go:embed templates/*.html
var templateFiles embed.FS

// Server renders the dashboard.
type Server struct {
	rules      *rules.Store
	history    *history.Log
	upstreams  *upstreams.Store
	tmpl       *template.Template
	listenPort string
	proxyPort  string
	sessions   *sessions.Manager
}

// UseListenAddr pins the dashboard Host check to this loopback address's port.
func (s *Server) UseListenAddr(addr string) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		s.listenPort = ""
		return
	}
	s.listenPort = port
}

// UseProxyAddr is the proxy listener. The Upstreams page copies this port into app URLs.
func (s *Server) UseProxyAddr(addr string) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return
	}
	s.proxyPort = port
}

// UseSessions lets the dashboard show test sessions. They are read-only here.
func (s *Server) UseSessions(m *sessions.Manager) {
	s.sessions = m
}

// New parses the embedded templates. A parse error should stop startup.
func New(store *rules.Store, log *history.Log, ups *upstreams.Store) (*Server, error) {
	tmpl, err := template.ParseFS(templateFiles, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{rules: store, history: log, upstreams: ups, tmpl: tmpl}, nil
}

type page struct {
	Title               string
	Rules               []ruleRow
	Form                formView
	Methods             []string
	Error               string
	ActiveUpstream      string
	Upstreams           []upstreamRow
	UpstreamForm        upstreamForm
	UpstreamChoices     []upstreamChoice
	RulesAction         string
	RuleUpstreamKey     string
	RuleUpstreamLabel   string
	RuleUpstreamChoices []upstreamChoice
	RuleShown           int
	RuleTotal           int
	RuleGroups          []ruleGroup
	RuleFilterQuery     string
	Sessions            []sessionChoice
	SessionAction       string
	SessionID           string
	ReadOnly            bool
}

type sessionChoice struct {
	ID       string
	Name     string
	Selected bool
}

type ruleGroup struct {
	Heading string
	Count   string
	Rules   []ruleRow
}

type ruleRow struct {
	ID         string
	Name       string
	APIName    string
	Method     string
	Pattern    string
	Conditions string
	Kind       string
	Enabled    bool
	Fresh      bool
	Status     string
	Delay      string
	Mode       string
	Upstream   string
	UpstreamID string
}

type upstreamRow struct {
	ID         string
	Name       string
	BaseURL    string
	Prefix     string
	LocalURL   string
	AndroidURL string
	Routing    string
	Active     bool
}

type upstreamForm struct {
	Action  string
	Submit  string
	Name    string
	BaseURL string
	Prefix  string
}

type upstreamChoice struct {
	ID       string
	Name     string
	Selected bool
}

// ServeHTTP routes the dashboard. API traffic never reaches this handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !allowedAdminHost(r.Host, s.listenPort) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	path := r.URL.Path
	switch {
	case path == "/_admin/rules" && r.Method == http.MethodPost:
		s.createRule(w, r)
	case path == "/_admin" || path == "/_admin/" || path == "/_admin/rules":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.rulesPage(w, r, http.StatusOK)
	case path == "/_admin/scenario" && r.Method == http.MethodGet:
		s.exportScenario(w, r)
	case path == "/_admin/rules/new" && r.Method == http.MethodGet:
		s.newRule(w, r)
	case strings.HasPrefix(path, "/_admin/rules/"):
		s.ruleRoute(w, r, strings.TrimPrefix(path, "/_admin/rules/"))
	case path == "/_admin/upstreams" && r.Method == http.MethodPost:
		s.createUpstream(w, r)
	case path == "/_admin/upstreams" && r.Method == http.MethodGet:
		s.upstreamsPage(w, r, http.StatusOK, "")
	case path == "/_admin/upstreams/new" && r.Method == http.MethodGet:
		s.render(w, r, "upstream_form", page{
			Title:        "New upstream",
			UpstreamForm: upstreamForm{Action: "/_admin/upstreams", Submit: "Create upstream"},
		}, http.StatusOK)
	case strings.HasPrefix(path, "/_admin/upstreams/"):
		s.upstreamRoute(w, r, strings.TrimPrefix(path, "/_admin/upstreams/"))
	case path == "/_admin/history" && r.Method == http.MethodGet:
		s.historyPage(w, r)
	case path == "/_admin/history/feed" && r.Method == http.MethodGet:
		s.historyFeed(w, r)
	case path == "/_admin/history.js" && r.Method == http.MethodGet:
		s.historyScript(w, r)
	case path == "/_admin/history/clear" && r.Method == http.MethodGet:
		s.clearConfirm(w, r)
	case path == "/_admin/history/clear" && r.Method == http.MethodPost:
		s.clearHistory(w, r)
	case strings.HasPrefix(path, "/_admin/history/"):
		s.historyRoute(w, r, strings.TrimPrefix(path, "/_admin/history/"))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) ruleRoute(w http.ResponseWriter, r *http.Request, rest string) {
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || !validID(parts[0]) {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	if len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	switch {
	case action == "edit" && r.Method == http.MethodGet:
		s.editForm(w, r, id, formView{}, "")
	case action == "toggle" && r.Method == http.MethodPost:
		s.toggleRule(w, r, id)
	case action == "delete" && r.Method == http.MethodPost:
		s.deleteRule(w, r, id)
	case action == "" && r.Method == http.MethodPost:
		s.updateRule(w, r, id)
	default:
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.NotFound(w, r)
	}
}

func (s *Server) rulesPage(w http.ResponseWriter, r *http.Request, status int) {
	store, _, _, readOnly, found := s.scope(r)
	if !found {
		s.render(w, r, "rules", page{Title: "Rules", Error: "That session is not open."}, http.StatusNotFound)
		return
	}
	all := []rules.Rule{}
	if store != nil {
		all = store.List()
	}
	key, label := s.parseRuleUpstream(r.URL.Query().Get("upstream"))
	rows := make([]ruleRow, 0)
	for _, rule := range all {
		if !ruleMatchesUpstream(rule, key, s.upstreamIDByName(key)) {
			continue
		}
		name := rule.Name
		if name == "" {
			name = "Untitled"
		}
		kind := "exact"
		if strings.Contains(rule.PathPattern, "*") {
			kind = "wildcard"
		}
		rows = append(rows, ruleRow{
			ID:         rule.ID,
			Name:       name,
			APIName:    rule.APIName,
			Method:     rule.Method,
			Pattern:    rule.PathPattern,
			Conditions: rules.MatchSummary(rule.Match),
			Kind:       kind,
			Enabled:    rule.Enabled,
			Fresh:      rule.FreshRequest,
			Status:     strconv.Itoa(rule.StatusCode),
			Delay:      delayLabel(rule.DelayMS),
			Mode:       modeLabel(rule.ResponseMode),
			Upstream:   s.upstreamLabel(rule.UpstreamID),
			UpstreamID: rule.UpstreamID,
		})
	}
	action := r.URL.Path
	if action == "/_admin/" {
		action = "/_admin"
	}
	s.render(w, r, "rules", page{
		Title:               "Rules",
		Rules:               rows,
		ActiveUpstream:      s.activeUpstreamName(),
		RulesAction:         action,
		RuleUpstreamKey:     key,
		RuleUpstreamLabel:   label,
		RuleUpstreamChoices: s.ruleUpstreamChoices(key),
		RuleShown:           len(rows),
		RuleTotal:           len(all),
		RuleGroups:          s.ruleGroups(rows),
		RuleFilterQuery:     s.upstreamQuery(r),
		ReadOnly:            readOnly,
	}, status)
}

func (s *Server) parseRuleUpstream(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", "All upstreams"
	}
	if name == "any" {
		return "any", "Any"
	}
	if s.upstreamIDByName(name) != "" {
		return name, name
	}
	return "", "All upstreams"
}

func (s *Server) upstreamIDByName(name string) string {
	if name == "" || name == "any" || s.upstreams == nil {
		return ""
	}
	for _, item := range s.upstreams.List() {
		if item.Name == name {
			return item.ID
		}
	}
	return ""
}

func (s *Server) ruleUpstreamChoices(selected string) []upstreamChoice {
	if s.upstreams == nil {
		return nil
	}
	choices := make([]upstreamChoice, 0)
	for _, item := range s.upstreams.List() {
		choices = append(choices, upstreamChoice{
			ID:       item.ID,
			Name:     item.Name,
			Selected: item.Name == selected,
		})
	}
	return choices
}

func (s *Server) ruleGroups(rows []ruleRow) []ruleGroup {
	anyRows := []ruleRow{}
	byID := map[string][]ruleRow{}
	unknown := map[string][]ruleRow{}
	var unknownIDs []string
	known := map[string]upstreams.Upstream{}
	activeID := ""
	if s.upstreams != nil {
		if active, ok := s.upstreams.Active(); ok {
			activeID = active.ID
		}
		for _, item := range s.upstreams.List() {
			known[item.ID] = item
		}
	}
	for _, row := range rows {
		if row.UpstreamID == "" {
			anyRows = append(anyRows, row)
			continue
		}
		if _, ok := known[row.UpstreamID]; ok {
			byID[row.UpstreamID] = append(byID[row.UpstreamID], row)
			continue
		}
		if _, seen := unknown[row.UpstreamID]; !seen {
			unknownIDs = append(unknownIDs, row.UpstreamID)
		}
		unknown[row.UpstreamID] = append(unknown[row.UpstreamID], row)
	}
	groups := []ruleGroup{}
	if s.upstreams != nil {
		for _, item := range s.upstreams.List() {
			block := byID[item.ID]
			if len(block) == 0 {
				continue
			}
			groups = append(groups, ruleGroup{
				Heading: upstreamBlockHeading(item, item.ID == activeID),
				Count:   ruleCountLabel(len(block)),
				Rules:   block,
			})
		}
	}
	for _, id := range unknownIDs {
		block := unknown[id]
		groups = append(groups, ruleGroup{
			Heading: "Unknown upstream: " + id,
			Count:   ruleCountLabel(len(block)),
			Rules:   block,
		})
	}
	if len(anyRows) > 0 {
		groups = append(groups, ruleGroup{
			Heading: "Any upstream",
			Count:   ruleCountLabel(len(anyRows)),
			Rules:   anyRows,
		})
	}
	return groups
}

func upstreamBlockHeading(item upstreams.Upstream, fallback bool) string {
	heading := item.Name
	if item.Prefix != "" {
		heading += " · " + item.Prefix
	}
	if fallback {
		heading += " · fallback"
	}
	return heading
}

func ruleCountLabel(n int) string {
	if n == 1 {
		return "1 rule"
	}
	return strconv.Itoa(n) + " rules"
}

func (s *Server) upstreamQuery(r *http.Request) string {
	key, _ := s.parseRuleUpstream(r.URL.Query().Get("upstream"))
	if key == "" {
		return ""
	}
	return "?" + url.Values{"upstream": {key}}.Encode()
}

func (s *Server) rulesHome(r *http.Request) string {
	query := s.upstreamQuery(r)
	if query == "" {
		return "/_admin"
	}
	return "/_admin" + query
}

func ruleMatchesUpstream(rule rules.Rule, key, namedID string) bool {
	switch key {
	case "":
		return true
	case "any":
		return rule.UpstreamID == ""
	default:
		return rule.UpstreamID == namedID || rule.UpstreamID == ""
	}
}

func (s *Server) exportScenario(w http.ResponseWriter, r *http.Request) {
	store, _, _, _, found := s.scope(r)
	if !found || store == nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	format := "yaml"
	if strings.EqualFold(r.URL.Query().Get("format"), "json") {
		format = "json"
	}
	doc := scenario.FromRules("exported", "", store.List(), func(id string) string {
		if id == "" {
			return "Any"
		}
		if s.upstreams == nil {
			return id
		}
		if item, ok := s.upstreams.Get(id); ok {
			return item.Name
		}
		return id
	})
	payload, err := scenario.Marshal(doc, format)
	if err != nil {
		http.Error(w, "could not export the scenario", http.StatusInternalServerError)
		return
	}
	if format == "json" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	}
	w.Header().Set("Content-Disposition", "attachment; filename=scenario."+format)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) createRule(w http.ResponseWriter, r *http.Request) {
	if s.readOnlyPost(w, r) {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	draft, view, err := draftFromForm(r)
	view.Action = "/_admin/rules"
	view.Submit = "Create rule"
	if err != nil {
		s.formError(w, r, "New rule", view, err)
		return
	}
	if err := s.knownUpstream(draft.UpstreamID); err != nil {
		s.formError(w, r, "New rule", view, err)
		return
	}
	if _, err := s.rules.Create(draft); err != nil {
		s.formError(w, r, "New rule", view, err)
		return
	}
	http.Redirect(w, r, "/_admin", http.StatusSeeOther)
}

func (s *Server) editForm(w http.ResponseWriter, r *http.Request, id string, posted formView, message string) {
	rule, ok := s.rules.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	action := "/_admin/rules/" + id + s.upstreamQuery(r)
	view := viewFromRule(rule, action, "Save rule")
	if message != "" {
		view = posted
		view.Action = action
		view.Submit = "Save rule"
	}
	s.render(w, r, "rule_form", page{
		Title:           "Edit rule",
		Form:            view,
		Methods:         methods,
		UpstreamChoices: s.upstreamChoices(view.UpstreamID),
		Error:           message,
	}, statusFor(message))
}

func (s *Server) updateRule(w http.ResponseWriter, r *http.Request, id string) {
	if s.readOnlyPost(w, r) {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if _, ok := s.rules.Get(id); !ok {
		http.NotFound(w, r)
		return
	}
	draft, view, err := draftFromForm(r)
	if err != nil {
		s.editForm(w, r, id, view, err.Error())
		return
	}
	if err := s.knownUpstream(draft.UpstreamID); err != nil {
		s.editForm(w, r, id, view, err.Error())
		return
	}
	if _, err := s.rules.Update(id, draft); err != nil {
		s.editForm(w, r, id, view, err.Error())
		return
	}
	http.Redirect(w, r, s.rulesHome(r), http.StatusSeeOther)
}

func (s *Server) toggleRule(w http.ResponseWriter, r *http.Request, id string) {
	if s.readOnlyPost(w, r) {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	rule, ok := s.rules.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	draft := rules.Draft{
		Name:         rule.Name,
		APIName:      rule.APIName,
		Enabled:      !rule.Enabled,
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
		Body:         string(rule.Body),
	}
	if _, err := s.rules.Update(id, draft); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, s.rulesHome(r), http.StatusSeeOther)
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request, id string) {
	if s.readOnlyPost(w, r) {
		return
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if err := s.rules.Delete(id); err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, s.rulesHome(r), http.StatusSeeOther)
}

func (s *Server) formError(w http.ResponseWriter, r *http.Request, title string, view formView, err error) {
	s.render(w, r, "rule_form", page{
		Title:           title,
		Form:            view,
		Methods:         methods,
		UpstreamChoices: s.upstreamChoices(view.UpstreamID),
		Error:           err.Error(),
	}, http.StatusBadRequest)
}

func (s *Server) scope(r *http.Request) (*rules.Store, *history.Log, string, bool, bool) {
	id := ""
	if r != nil {
		id = strings.TrimSpace(r.URL.Query().Get("session"))
	}
	if s.sessions == nil || id == "" || id == sessions.DefaultID {
		return s.rules, s.history, sessions.DefaultID, false, true
	}
	info, ok := s.sessions.Get(id)
	if !ok {
		return nil, nil, id, true, false
	}
	s.sessions.Touch(id)
	return info.Rules, info.History, info.ID, info.ReadOnly, true
}

func (s *Server) sessionChrome(r *http.Request) ([]sessionChoice, string, bool) {
	_, _, id, readOnly, found := s.scope(r)
	if s.sessions == nil {
		return nil, id, false
	}
	if !found {
		id = ""
	}
	var choices []sessionChoice
	for _, item := range s.sessions.List() {
		choices = append(choices, sessionChoice{
			ID:       item.ID,
			Name:     item.Name,
			Selected: item.ID == id || (id == "" && item.ID == sessions.DefaultID),
		})
	}
	return choices, id, readOnly
}

func (s *Server) decorate(r *http.Request, data page) page {
	data.Sessions, data.SessionID, data.ReadOnly = s.sessionChrome(r)
	if r != nil {
		data.SessionAction = r.URL.Path
	}
	return data
}

func (s *Server) readOnlyPost(w http.ResponseWriter, r *http.Request) bool {
	_, _, _, readOnly, found := s.scope(r)
	if !found {
		http.Error(w, "session not found", http.StatusNotFound)
		return true
	}
	if readOnly {
		http.Error(w, "test sessions are read-only in the dashboard", http.StatusForbidden)
		return true
	}
	return false
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, data page, status int) {
	data = s.decorate(r, data)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Frame-Options", "DENY")
	csp := "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	if name == "rule_form" {
		csp = "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"
	}
	w.Header().Set("Content-Security-Policy", csp)
	w.WriteHeader(status)
	if err := s.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func validID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}

func statusFor(message string) int {
	if message == "" {
		return http.StatusOK
	}
	return http.StatusBadRequest
}
