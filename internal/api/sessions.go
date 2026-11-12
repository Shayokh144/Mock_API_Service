package api

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

type sessionBody struct {
	Name    string `json:"name"`
	Port    int    `json:"port"`
	IdleSec int    `json:"idle_timeout_seconds"`
}

type sessionResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Port        int    `json:"port"`
	ProxyURL    string `json:"proxy_url"`
	EmulatorURL string `json:"emulator_url"`
}

func (s *Server) sessionsRoute(w http.ResponseWriter, r *http.Request) {
	if s.sessions == nil {
		writeError(w, http.StatusNotFound, "not_found", "sessions are not available")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/_api/sessions")
	rest = strings.Trim(rest, "/")
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			s.listSessions(w, r)
		case http.MethodPost:
			s.createSession(w, r)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
		return
	}
	id, extra, _ := strings.Cut(rest, "/")
	if extra != "" {
		if extra == "rules" || strings.HasPrefix(extra, "rules/") {
			s.rulesRoute(w, r, id, strings.TrimPrefix(extra, "rules"))
			return
		}
		if extra == "reset" {
			s.resetSession(w, r, id)
			return
		}
		if extra == "history" {
			s.historyRoute(w, r, id)
			return
		}
		writeError(w, http.StatusNotFound, "not_found", "unknown API path")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getSession(w, r, id)
	case http.MethodDelete:
		s.deleteSession(w, r, id)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	items := s.sessions.List()
	out := make([]sessionResponse, 0, len(items))
	for _, item := range items {
		out = append(out, toSessionResponse(item))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var body sessionBody
	if err := readJSON(w, r, &body); err != nil {
		return
	}
	name := strings.TrimSpace(body.Name)
	if utf8.RuneCountInString(name) > 80 {
		writeError(w, http.StatusBadRequest, "bad_request", "name must be 80 characters or fewer")
		return
	}
	if body.IdleSec < 0 {
		writeError(w, http.StatusBadRequest, "bad_request", "idle_timeout_seconds must be zero or greater")
		return
	}
	info, err := s.sessions.Create(name, body.Port, time.Duration(body.IdleSec)*time.Second)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSessionResponse(info))
}

func (s *Server) getSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	info, ok := s.sessions.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "session not found")
		return
	}
	s.sessions.Touch(id)
	writeJSON(w, http.StatusOK, toSessionResponse(info))
}

func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	if err := s.sessions.Delete(id); err != nil {
		writeSessionError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func toSessionResponse(info sessions.Info) sessionResponse {
	return sessionResponse{
		ID:          info.ID,
		Name:        info.Name,
		Port:        info.Port,
		ProxyURL:    info.ProxyURL,
		EmulatorURL: info.EmulatorURL,
	}
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessions.ErrDefault):
		writeError(w, http.StatusConflict, "conflict", "the default session cannot be deleted")
	case errors.Is(err, sessions.ErrPortBusy):
		writeError(w, http.StatusConflict, "conflict", "that port is in use")
	case errors.Is(err, sessions.ErrTooMany):
		writeError(w, http.StatusTooManyRequests, "too_many", "only 10 test sessions can be open")
	case errors.Is(err, sessions.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "session not found")
	case errors.Is(err, sessions.ErrBadRequest):
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", "could not update the session")
	}
}
