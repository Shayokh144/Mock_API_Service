package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIMaxUsesAndResponsesRoundTrip(t *testing.T) {
	tokens, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, tokens)
	session := postSession(t, srv, token, `{"name":"seq"}`)
	body := `{"description":"steps","method":"GET","path":"/v1/seq","status":200,"max_uses":2,"sequence":"loop","responses":[{"status_code":200,"body":{"n":1}},{"status_code":201,"body":{"n":2}}]}`
	created := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token, body)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body, `"max_uses":2`) || !strings.Contains(created.Body, `"sequence":"loop"`) {
		t.Fatalf("create = %d %s", created.Code, created.Body)
	}
	if !strings.Contains(created.Body, `"n":1`) || !strings.Contains(created.Body, `"status_code":201`) {
		t.Fatalf("responses = %s", created.Body)
	}
}
