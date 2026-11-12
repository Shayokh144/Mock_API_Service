package scenario

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestParseParityAndErrors(t *testing.T) {
	yamlDoc := []byte("version: 1\nname: demo\ndescription: sample\nrules:\n  - name: login-fail\n    description: Login\n    method: POST\n    path: /v1/login\n    status: 401\n    body:\n      error: no\n")
	jsonDoc := []byte(`{"version":1,"name":"demo","description":"sample","rules":[{"name":"login-fail","description":"Login","method":"POST","path":"/v1/login","status":401,"body":{"error":"no"}}]}`)
	fromYAML, err := Parse(yamlDoc, "yaml")
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := Parse(jsonDoc, "json")
	if err != nil {
		t.Fatal(err)
	}
	yd, err := fromYAML.Drafts(func(string) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	jd, err := fromJSON.Drafts(func(string) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if yd[0].APIName != jd[0].APIName || yd[0].PathPattern != jd[0].PathPattern || yd[0].Body != jd[0].Body {
		t.Fatalf("yaml %#v json %#v", yd[0], jd[0])
	}
	if _, err := Parse([]byte("version: 2\nname: x\nrules: []\n"), "yaml"); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatal(err)
	}
	if _, err := Parse([]byte("version: 1\nname: x\nextra: 1\nrules: []\n"), "yaml"); err == nil || !strings.Contains(err.Error(), "line") {
		t.Fatalf("unknown = %v", err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scenarios/okya-auth-refresh-failure.yaml", "scenarios/easyhotel-offline.json"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(raw, Format(name, "")); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	store := rules.NewMemory()
	if _, err := store.ApplyDrafts(yd, true, false); err != nil {
		t.Fatal(err)
	}
	doc := FromRules("demo", "sample", store.List(), func(string) string { return "Any" })
	again, err := doc.Drafts(func(string) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyDrafts(again, true, false); err != nil {
		t.Fatal(err)
	}
	got := store.List()
	if len(got) != 1 || got[0].APIName != "login-fail" || got[0].PathPattern != "/v1/login" {
		t.Fatalf("round trip = %+v", got)
	}
}
