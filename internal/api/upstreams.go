package api

import (
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
	"github.com/Shayokh144/Mock_API_Service/internal/upstreams"
)

type upstreamBody struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Prefix  string `json:"prefix"`
}

func (s *Server) upstreamsRoute(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeError(w, http.StatusNotFound, "not_found", "upstreams are not available")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/_api/upstreams"), "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			s.listUpstreams(w, r)
		case http.MethodPost:
			s.createUpstream(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	id, extra, _ := strings.Cut(rest, "/")
	if extra != "" {
		writeError(w, http.StatusNotFound, "not_found", "unknown API path")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getUpstream(w, r, id)
	case http.MethodPut:
		s.replaceUpstream(w, r, id)
	case http.MethodDelete:
		s.deleteUpstream(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) listUpstreams(w http.ResponseWriter, r *http.Request) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	items := []upstreams.Upstream{}
	if store := s.upstreamStore(); store != nil {
		for _, item := range store.List() {
			items = append(items, maskUpstream(item))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"upstreams": items})
}

func (s *Server) createUpstream(w http.ResponseWriter, r *http.Request) {
	if !requireDefaultConfirm(w, r, sessions.DefaultID) {
		return
	}
	store := s.upstreamStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "not_found", "upstreams are not available")
		return
	}
	var body upstreamBody
	if err := readJSON(w, r, &body); err != nil {
		return
	}
	item, err := store.Create(body.Name, body.BaseURL, body.Prefix)
	if err != nil {
		writeUpstreamStoreError(w, err)
		return
	}
	logUpstreamChange("created", item)
	writeJSON(w, http.StatusCreated, maskUpstream(item))
}

func (s *Server) getUpstream(w http.ResponseWriter, r *http.Request, id string) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	store := s.upstreamStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "not_found", "upstream not found")
		return
	}
	item, ok := store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "upstream not found")
		return
	}
	writeJSON(w, http.StatusOK, maskUpstream(item))
}

func (s *Server) replaceUpstream(w http.ResponseWriter, r *http.Request, id string) {
	if !requireDefaultConfirm(w, r, sessions.DefaultID) {
		return
	}
	store := s.upstreamStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "not_found", "upstreams are not available")
		return
	}
	var body upstreamBody
	if err := readJSON(w, r, &body); err != nil {
		return
	}
	item, err := store.Update(id, body.Name, body.BaseURL, body.Prefix)
	if err != nil {
		writeUpstreamStoreError(w, err)
		return
	}
	logUpstreamChange("updated", item)
	writeJSON(w, http.StatusOK, maskUpstream(item))
}

func (s *Server) deleteUpstream(w http.ResponseWriter, r *http.Request, id string) {
	if !requireDefaultConfirm(w, r, sessions.DefaultID) {
		return
	}
	if err := rejectBody(w, r); err != nil {
		return
	}
	store := s.upstreamStore()
	if store == nil {
		writeError(w, http.StatusNotFound, "not_found", "upstreams are not available")
		return
	}
	item, ok := store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "upstream not found")
		return
	}
	info, ok := s.sessions.Get(sessions.DefaultID)
	if !ok || info.Rules == nil {
		writeError(w, http.StatusNotFound, "not_found", "upstreams are not available")
		return
	}
	if err := info.Rules.DetachUpstream(id); err != nil {
		writeError(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if err := store.Delete(id); err != nil {
		writeUpstreamStoreError(w, err)
		return
	}
	logUpstreamChange("deleted", item)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) upstreamStore() *upstreams.Store {
	if s.sessions == nil {
		return nil
	}
	return s.sessions.Upstreams()
}

func writeUpstreamStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, upstreams.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "upstream not found")
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "already exists") || strings.Contains(msg, "already uses prefix") {
		writeError(w, http.StatusConflict, "conflict", msg)
		return
	}
	if upstreamValidationMessage(msg) {
		writeError(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", "could not update upstreams")
}

func upstreamValidationMessage(msg string) bool {
	for _, part := range []string{"name ", "name is", "base URL", "prefix"} {
		if strings.Contains(msg, part) {
			return true
		}
	}
	return false
}

func maskUpstream(item upstreams.Upstream) upstreams.Upstream {
	item.BaseURL = maskBaseURL(item.BaseURL)
	return item
}

// maskBaseURL hides userinfo. The store rejects it on write. A GET still
// masks it so a stored URL cannot return a username or password.
func maskBaseURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	mask := "***"
	if _, ok := parsed.User.Password(); ok {
		mask = "***:***"
	}
	parsed.User = nil
	bare := parsed.String()
	scheme := strings.Index(bare, "://")
	if scheme < 0 {
		return raw
	}
	return bare[:scheme+3] + mask + "@" + bare[scheme+3:]
}

func logUpstreamChange(action string, item upstreams.Upstream) {
	host := ""
	if parsed, err := url.Parse(item.BaseURL); err == nil {
		host = parsed.Hostname()
	}
	log.Printf("upstream %s id=%s name=%q prefix=%q host=%s", action, item.ID, item.Name, item.Prefix, host)
}

func historyUpstreamKnown(ups *upstreams.Store, key string) bool {
	if key == "" || strings.EqualFold(key, "any") {
		return true
	}
	if ups != nil {
		for _, item := range ups.List() {
			if item.Name == key || item.ID == key {
				return true
			}
		}
	}
	return isUpstreamID(key)
}

func isUpstreamID(id string) bool {
	if len(id) != 16 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
