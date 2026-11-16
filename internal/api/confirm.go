package api

import (
	"net/http"

	"github.com/Shayokh144/Mock_API_Service/internal/sessions"
)

// requireDefaultConfirm guards mutating /_api calls on the developer session.
// Test sessions and reads do not use it. False means the response is already written.
func requireDefaultConfirm(w http.ResponseWriter, r *http.Request, id string) bool {
	if id != sessions.DefaultID || r.URL.Query().Get("confirm") == sessions.DefaultID {
		return true
	}
	writeError(w, http.StatusConflict, "conflict", "writes to the default session need ?confirm=default")
	return false
}
