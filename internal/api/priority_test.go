package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIRulePriorityRoundTrip(t *testing.T) {
	tokens, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, tokens)
	session := postSession(t, srv, token, `{"name":"prio"}`)
	created := apiJSON(t, srv, http.MethodPost, "/_api/sessions/"+session.ID+"/rules", token,
		`{"description":"high","method":"GET","path":"/v1/items/*","status":201,"priority":-4}`)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body, `"priority":-4`) && !strings.Contains(created.Body, `"priority": -4`) {
		t.Fatalf("create = %d %s", created.Code, created.Body)
	}
	listed := apiJSON(t, srv, http.MethodGet, "/_api/sessions/"+session.ID+"/rules", token, "")
	if !strings.Contains(listed.Body, `"priority":-4`) && !strings.Contains(listed.Body, `"priority": -4`) {
		t.Fatalf("list = %s", listed.Body)
	}
}
