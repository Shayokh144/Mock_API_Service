package api

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"github.com/Shayokh144/Mock_API_Service/internal/scenario"
	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

func (s *Server) scenarioRoute(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if err := rejectBody(w, r); err != nil {
		return
	}
	info, ok := s.sessions.Get(id)
	if !ok || info.Rules == nil {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	s.sessions.Touch(id)
	format := "json"
	if strings.EqualFold(r.URL.Query().Get("format"), "yaml") {
		format = "yaml"
	}
	doc := scenario.FromRules(info.Name, "", info.Rules.List(), func(upstreamID string) string {
		return upstreamName(s.sessions.Upstreams(), upstreamID)
	})
	payload, err := scenario.Marshal(doc, format)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not export the scenario")
		return
	}
	if format == "yaml" {
		w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func (s *Server) loadScenario(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	info, ok := s.sessions.Get(id)
	if !ok || info.Rules == nil {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	format, ok := scenarioContentType(r.Header.Get("Content-Type"))
	if !ok {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json or application/yaml")
		return
	}
	replace, ok := scenarioMode(r.URL.Query().Get("mode"))
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "mode must be replace or merge")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body must be 1 MiB or smaller")
			return
		}
		writeError(w, http.StatusBadRequest, "bad_request", "could not read the scenario")
		return
	}
	doc, err := scenario.Parse(data, format)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.applyDocument(info, doc, replace)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	s.sessions.Touch(id)
	writeJSON(w, http.StatusOK, map[string]any{
		"created":   result.Created,
		"updated":   result.Updated,
		"unchanged": result.Unchanged,
	})
}

func (s *Server) applyDocument(info sessions.Info, doc scenario.Document, replace bool) (rules.ApplyResult, error) {
	if info.Rules == nil {
		return rules.ApplyResult{}, errors.New("session not found")
	}
	drafts, err := doc.Drafts(func(name string) (string, error) {
		return resolveUpstream(s.sessions.Upstreams(), name)
	})
	if err != nil {
		return rules.ApplyResult{}, err
	}
	return info.Rules.ApplyDrafts(drafts, replace, true)
}

func scenarioContentType(media string) (string, bool) {
	media = strings.ToLower(strings.TrimSpace(strings.Split(media, ";")[0]))
	switch media {
	case "application/json":
		return "json", true
	case "application/yaml", "text/yaml", "application/x-yaml":
		return "yaml", true
	default:
		return "", false
	}
}

func scenarioMode(raw string) (bool, bool) {
	switch strings.TrimSpace(raw) {
	case "", "replace":
		return true, true
	case "merge":
		return false, true
	default:
		return false, false
	}
}
