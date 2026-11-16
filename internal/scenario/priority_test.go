package scenario

import (
	"strings"
	"testing"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

func TestScenarioPriorityRoundTrip(t *testing.T) {
	doc, err := Parse([]byte("version: 1\nname: p\nrules:\n  - name: high\n    method: GET\n    path: /v1/items/*\n    status: 201\n    priority: 4\n  - name: old\n    method: GET\n    path: /v1/plain\n    status: 200\n"), "yaml")
	if err != nil {
		t.Fatal(err)
	}
	drafts, err := doc.Drafts(func(string) (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if drafts[0].Priority != 4 || drafts[1].Priority != 0 {
		t.Fatalf("drafts = %+v", drafts)
	}
	exported := FromRules("p", "", []rules.Rule{{
		ID: "0123456789abcdef", APIName: "high", Method: "GET", PathPattern: "/v1/items/*",
		StatusCode: 201, Priority: 4, Enabled: true,
	}}, func(string) string { return "" })
	encoded, err := Marshal(exported, "json")
	if err != nil || !strings.Contains(string(encoded), `"priority": 4`) {
		t.Fatalf("json = %s %v", encoded, err)
	}
}
