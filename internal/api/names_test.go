package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestStableNames(t *testing.T) {
	_, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, srv.tokens)
	a := postSession(t, srv, token, `{"name":"a"}`)
	b := postSession(t, srv, token, `{"name":"b"}`)
	path := "/_api/sessions/" + a.ID + "/rules/by-name/login-fail"
	body := `{"description":"Login","method":"POST","path":"/v1/login","status":401,"body":{"error":"no"}}`
	first := apiJSON(t, srv, "PUT", path, token, body)
	if first.Code != 201 || !strings.Contains(first.Body, `"name":"login-fail"`) {
		t.Fatalf("create = %d %s", first.Code, first.Body)
	}
	second := apiJSON(t, srv, "PUT", path, token, `{"name":"login-fail","description":"Login","method":"POST","path":"/v1/login","status":500,"body":{"error":"yes"}}`)
	if second.Code != 200 || !strings.Contains(second.Body, `"status":500`) {
		t.Fatalf("replace = %d %s", second.Code, second.Body)
	}
	again := apiJSON(t, srv, "PUT", path, token, `{"name":"login-fail","description":"Login","method":"POST","path":"/v1/login","status":500,"body":{"error":"yes"}}`)
	if again.Code != 200 {
		t.Fatalf("repeat = %d %s", again.Code, again.Body)
	}
	bad := apiJSON(t, srv, "PUT", "/_api/sessions/"+a.ID+"/rules/by-name/Login", token, body)
	if bad.Code != 400 {
		t.Fatalf("invalid = %d %s", bad.Code, bad.Body)
	}
	clash := apiJSON(t, srv, "POST", "/_api/sessions/"+a.ID+"/rules", token, `{"name":"login-fail","method":"GET","path":"/v1/other","status":200}`)
	if clash.Code != 409 {
		t.Fatalf("clash = %d %s", clash.Code, clash.Body)
	}
	other := apiJSON(t, srv, "PUT", "/_api/sessions/"+b.ID+"/rules/by-name/login-fail", token, body)
	if other.Code != 201 {
		t.Fatalf("other session = %d %s", other.Code, other.Body)
	}
	got := apiJSON(t, srv, "GET", path, token, "")
	if got.Code != 200 || !strings.Contains(got.Body, "login-fail") {
		t.Fatalf("get = %d %s", got.Code, got.Body)
	}

	dir := t.TempDir()
	raw := []byte(`{"rules":[{"id":"0123456789abcdef","name":"Old label","enabled":true,"method":"GET","path_pattern":"/v1/old","status_code":200,"headers":{},"body":{"ok":true},"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`)
	file := filepath.Join(dir, "rules.json")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := rules.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	list := store.List()
	if len(list) != 1 || list[0].Name != "Old label" || list[0].APIName != "" {
		t.Fatalf("loaded = %+v", list)
	}
}
