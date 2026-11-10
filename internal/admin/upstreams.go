package admin

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

func (s *Server) upstreamRoute(w http.ResponseWriter, r *http.Request, rest string) {
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
		s.editUpstream(w, r, id, upstreamForm{}, "")
	case action == "activate" && r.Method == http.MethodPost:
		s.activateUpstream(w, r, id)
	case action == "delete" && r.Method == http.MethodPost:
		s.deleteUpstream(w, r, id)
	case action == "" && r.Method == http.MethodPost:
		s.updateUpstream(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) upstreamsPage(w http.ResponseWriter, r *http.Request, status int, message string) {
	rows := []upstreamRow{}
	activeID := ""
	port := requestPort(r)
	if s.upstreams != nil {
		if active, ok := s.upstreams.Active(); ok {
			activeID = active.ID
		}
		for _, item := range s.upstreams.List() {
			localURL, androidURL := "", ""
			if item.Prefix != "" {
				localURL = "http://127.0.0.1:" + port + item.Prefix
				androidURL = "http://10.0.2.2:" + port + item.Prefix
			}
			fallback := item.ID == activeID
			rows = append(rows, upstreamRow{
				ID:         item.ID,
				Name:       item.Name,
				BaseURL:    item.BaseURL,
				Prefix:     item.Prefix,
				LocalURL:   localURL,
				AndroidURL: androidURL,
				Routing:    routingStatus(item.Prefix, fallback),
				Active:     fallback,
			})
		}
	}
	s.render(w, "upstreams", page{
		Title:          "Upstreams",
		Upstreams:      rows,
		ActiveUpstream: s.activeUpstreamName(),
		Error:          message,
	}, status)
}

func routingStatus(prefix string, fallback bool) string {
	if prefix != "" {
		status := "Always on via " + prefix
		if fallback {
			status += " and fallback"
		}
		return status
	}
	if fallback {
		return "Fallback"
	}
	return "Not used"
}

func requestPort(r *http.Request) string {
	if r == nil {
		return "8080"
	}
	_, port, err := net.SplitHostPort(r.Host)
	if err != nil || port == "" {
		return "8080"
	}
	return port
}

func (s *Server) createUpstream(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if s.upstreams == nil {
		http.Error(w, "upstreams are not available", http.StatusNotFound)
		return
	}
	name, baseURL, prefix, view := upstreamFromForm(r, "/_admin/upstreams", "Create upstream")
	if _, err := s.upstreams.Create(name, baseURL, prefix); err != nil {
		s.upstreamError(w, "New upstream", view, err)
		return
	}
	http.Redirect(w, r, "/_admin/upstreams", http.StatusSeeOther)
}

func (s *Server) editUpstream(w http.ResponseWriter, r *http.Request, id string, posted upstreamForm, message string) {
	if s.upstreams == nil {
		http.NotFound(w, r)
		return
	}
	item, ok := s.upstreams.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	view := upstreamForm{
		Action:  "/_admin/upstreams/" + id,
		Submit:  "Save upstream",
		Name:    item.Name,
		BaseURL: item.BaseURL,
		Prefix:  item.Prefix,
	}
	if message != "" {
		view = posted
		view.Action = "/_admin/upstreams/" + id
		view.Submit = "Save upstream"
	}
	s.render(w, "upstream_form", page{
		Title:        "Edit upstream",
		UpstreamForm: view,
		Error:        message,
	}, statusFor(message))
}

func (s *Server) updateUpstream(w http.ResponseWriter, r *http.Request, id string) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if s.upstreams == nil {
		http.NotFound(w, r)
		return
	}
	name, baseURL, prefix, view := upstreamFromForm(r, "/_admin/upstreams/"+id, "Save upstream")
	if _, err := s.upstreams.Update(id, name, baseURL, prefix); err != nil {
		if err == upstreams.ErrNotFound {
			http.NotFound(w, r)
			return
		}
		s.editUpstream(w, r, id, view, err.Error())
		return
	}
	http.Redirect(w, r, "/_admin/upstreams", http.StatusSeeOther)
}

func (s *Server) activateUpstream(w http.ResponseWriter, r *http.Request, id string) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if s.upstreams == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.upstreams.Activate(id); err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/_admin/upstreams", http.StatusSeeOther)
}

func (s *Server) deleteUpstream(w http.ResponseWriter, r *http.Request, id string) {
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form posts are not allowed", http.StatusForbidden)
		return
	}
	if s.upstreams == nil || s.rules == nil {
		http.NotFound(w, r)
		return
	}
	if _, ok := s.upstreams.Get(id); !ok {
		http.NotFound(w, r)
		return
	}
	if active, ok := s.upstreams.Active(); ok && active.ID == id {
		s.upstreamsPage(w, r, http.StatusBadRequest, upstreams.ErrActive.Error())
		return
	}
	if err := s.rules.DetachUpstream(id); err != nil {
		s.upstreamsPage(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.upstreams.Delete(id); err != nil {
		s.upstreamsPage(w, r, http.StatusBadRequest, err.Error())
		return
	}
	http.Redirect(w, r, "/_admin/upstreams", http.StatusSeeOther)
}

func (s *Server) upstreamError(w http.ResponseWriter, title string, view upstreamForm, err error) {
	s.render(w, "upstream_form", page{
		Title:        title,
		UpstreamForm: view,
		Error:        err.Error(),
	}, http.StatusBadRequest)
}

func upstreamFromForm(r *http.Request, action, submit string) (string, string, string, upstreamForm) {
	_ = r.ParseForm()
	view := upstreamForm{
		Action:  action,
		Submit:  submit,
		Name:    r.FormValue("name"),
		BaseURL: r.FormValue("base_url"),
		Prefix:  r.FormValue("prefix"),
	}
	return view.Name, view.BaseURL, view.Prefix, view
}

func (s *Server) knownUpstream(id string) error {
	if id == "" {
		return nil
	}
	if s.upstreams == nil {
		return fmt.Errorf("choose an upstream or Any upstream")
	}
	if _, ok := s.upstreams.Get(id); !ok {
		return fmt.Errorf("choose an upstream or Any upstream")
	}
	return nil
}

func (s *Server) upstreamChoices(selected string) []upstreamChoice {
	choices := []upstreamChoice{{ID: "", Name: "Any upstream", Selected: selected == ""}}
	if s.upstreams == nil {
		return choices
	}
	found := selected == ""
	for _, item := range s.upstreams.List() {
		if item.ID == selected {
			found = true
		}
		choices = append(choices, upstreamChoice{
			ID:       item.ID,
			Name:     item.Name,
			Selected: item.ID == selected,
		})
	}
	if selected != "" && !found {
		choices = append(choices, upstreamChoice{ID: selected, Name: "Missing upstream", Selected: true})
	}
	return choices
}

func (s *Server) upstreamLabel(id string) string {
	if id == "" {
		return "any"
	}
	if s.upstreams != nil {
		if item, ok := s.upstreams.Get(id); ok {
			return item.Name
		}
	}
	return "missing"
}

func (s *Server) activeUpstreamName() string {
	if s.upstreams == nil {
		return ""
	}
	item, ok := s.upstreams.Active()
	if !ok {
		return ""
	}
	return item.Name
}
