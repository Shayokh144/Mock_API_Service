// Package api serves the local automation API on the admin listener.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/admin"
)

const (
	// Version is reported by GET /_api/health.
	Version = "dev"
	// MaxBody is the largest JSON body an API request may send.
	MaxBody = 1 << 20
)

// Server is the /_api handler. It is mounted only on the admin listener.
type Server struct {
	tokens  *TokenFile
	port    string
	version string
}

// New returns an API server. port is the admin listen port, used by the Host check.
func New(tokens *TokenFile, listenAddr, version string) *Server {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		port = ""
	}
	if version == "" {
		version = Version
	}
	return &Server{tokens: tokens, port: port, version: version}
}

// Mount sends /_api to the API server and every other path to the dashboard.
func Mount(dashboard, apiHandler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_api" || strings.HasPrefix(r.URL.Path, "/_api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		dashboard.ServeHTTP(w, r)
	})
}

// ServeHTTP handles /_api. Callers mount it only for that path prefix.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !admin.AllowedHost(r.Host, s.port) {
		writeError(w, http.StatusForbidden, "forbidden", "admin API requests must use a loopback Host on the admin port")
		return
	}
	if r.Method == http.MethodOptions || strings.TrimSpace(r.Header.Get("Origin")) != "" {
		writeError(w, http.StatusForbidden, "forbidden", "the admin API does not accept browser origins")
		return
	}
	token, ok := bearer(r.Header.Get("Authorization"))
	if !ok || s.tokens == nil || !s.tokens.Match(token) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}
	path := r.URL.Path
	switch {
	case path == "/_api/health" && r.Method == http.MethodGet:
		s.health(w, r)
	case path == "/_api/token/rotate" && r.Method == http.MethodPost:
		s.rotate(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown API path")
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := rejectBody(w, r); err != nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"version": s.version,
	})
}

func (s *Server) rotate(w http.ResponseWriter, r *http.Request) {
	if err := readJSON(w, r, &struct{}{}); err != nil {
		return
	}
	if err := s.tokens.Rotate(); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not rotate the token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func bearer(header string) (string, bool) {
	const prefix = "bearer "
	header = strings.TrimSpace(header)
	if len(header) < len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body apiError
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// rejectBody fails when a GET carries a body.
func rejectBody(w http.ResponseWriter, r *http.Request) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return readJSON(w, r, &struct{}{})
}

// readJSON enforces the JSON content type, the 1 MiB cap, and unknown-field rejection.
// An empty body is accepted and leaves dst unchanged.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil || (r.ContentLength == 0 && !chunked(r)) {
		return nil
	}
	media := r.Header.Get("Content-Type")
	if !jsonContent(media) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return errStop
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBody)
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(err.Error(), "request body too large") {
			writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body must be 1 MiB or smaller")
			return errStop
		}
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return errStop
	}
	dec := json.NewDecoder(bytes.NewReader(buf))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			writeError(w, http.StatusBadRequest, "bad_request", "request contains an unknown field")
			return errStop
		}
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return errStop
	}
	var extra struct{}
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return errStop
	}
	return nil
}

func jsonContent(media string) bool {
	media = strings.ToLower(strings.TrimSpace(media))
	if i := strings.Index(media, ";"); i >= 0 {
		media = strings.TrimSpace(media[:i])
	}
	return media == "application/json"
}

func chunked(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Transfer-Encoding")), "chunked")
}

var errStop = errors.New("response already written")
