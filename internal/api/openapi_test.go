package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPIMatchesRouter(t *testing.T) {
	specOps := openAPIOperations(t, filepath.Join("..", "..", "openapi.yaml"))
	codeOps := routeSet(Routes())
	if diff := setDiff(codeOps, specOps); len(diff) > 0 {
		t.Errorf("router routes missing from openapi.yaml:\n%s", strings.Join(diff, "\n"))
	}
	if diff := setDiff(specOps, codeOps); len(diff) > 0 {
		t.Errorf("openapi.yaml routes the router does not serve:\n%s", strings.Join(diff, "\n"))
	}

	_, srv, manager := testAPI(t, t.TempDir())
	defer manager.Close()
	token := readToken(t, srv.tokens)
	session := postSession(t, srv, token, `{"name":"spec"}`)

	served := map[string]bool{}
	for _, path := range uniquePaths(Routes()) {
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead} {
			if method == http.MethodPost && path == "/_api/token/rotate" {
				continue
			}
			target := concretePath(path, session.ID)
			if method == http.MethodDelete && path == "/_api/sessions/{session_id}" {
				target = "/_api/sessions/missing-session"
			}
			code, body := probe(srv, method, target, token)
			if code == http.StatusUnauthorized || code == http.StatusForbidden {
				t.Fatalf("%s %s = %d %s", method, target, code, body)
			}
			if routeHandled(code, body) {
				served[method+" "+path] = true
			}
		}
	}
	delete(codeOps, http.MethodPost+" /_api/token/rotate")
	if diff := setDiff(codeOps, served); len(diff) > 0 {
		t.Errorf("declared routes the server did not dispatch:\n%s", strings.Join(diff, "\n"))
	}
	if diff := setDiff(served, codeOps); len(diff) > 0 {
		t.Errorf("server dispatched routes that are not declared:\n%s", strings.Join(diff, "\n"))
	}

	code, body := probe(srv, http.MethodPost, "/_api/token/rotate", token)
	if code != http.StatusNoContent {
		t.Fatalf("rotate = %d %s", code, body)
	}
}

func TestOpenAPICoversRouteLiterals(t *testing.T) {
	declared := make([]string, 0, len(Routes()))
	for _, route := range Routes() {
		declared = append(declared, route.Path)
	}
	quotedPath := regexp.MustCompile(`"(/_api[^"]*)"`)
	quotedSegment := regexp.MustCompile(`"(rules/|rules|reset|history|scenario|scenarios/load|by-name/|by-name|enable|disable)"`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		text, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var literals []string
		for _, match := range quotedPath.FindAllStringSubmatch(string(text), -1) {
			literals = append(literals, match[1])
		}
		for _, match := range quotedSegment.FindAllStringSubmatch(string(text), -1) {
			literals = append(literals, match[1])
		}
		for _, literal := range literals {
			if !literalCovered(literal, declared) {
				t.Errorf("%s quotes %q, which is not part of Routes()", name, literal)
			}
		}
	}
}

func openAPIOperations(t *testing.T, path string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OpenAPI string                 `yaml:"openapi"`
		Servers []struct{ URL string } `yaml:"servers"`
		Paths   map[string]yaml.Node   `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1.") {
		t.Fatalf("openapi version = %q", doc.OpenAPI)
	}
	if len(doc.Servers) != 1 || doc.Servers[0].URL != "http://127.0.0.1:8081" {
		t.Fatalf("servers = %+v", doc.Servers)
	}
	methods := map[string]bool{
		"get": true, "post": true, "put": true, "patch": true,
		"delete": true, "head": true, "options": true, "trace": true,
	}
	out := map[string]bool{}
	for path, node := range doc.Paths {
		if node.Kind != yaml.MappingNode {
			t.Fatalf("%s is not a mapping", path)
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if !methods[key] {
				continue
			}
			out[strings.ToUpper(key)+" "+path] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("openapi.yaml has no operations")
	}
	return out
}

func routeSet(routes []Route) map[string]bool {
	out := map[string]bool{}
	for _, route := range routes {
		out[route.Method+" "+route.Path] = true
	}
	return out
}

func uniquePaths(routes []Route) []string {
	seen := map[string]bool{}
	var out []string
	for _, route := range routes {
		if seen[route.Path] {
			continue
		}
		seen[route.Path] = true
		out = append(out, route.Path)
	}
	return out
}

func concretePath(path, sessionID string) string {
	path = strings.ReplaceAll(path, "{session_id}", sessionID)
	path = strings.ReplaceAll(path, "{rule_id}", "0123456789abcdef")
	path = strings.ReplaceAll(path, "{name}", "sample-rule")
	return path
}

func probe(srv http.Handler, method, path, token string) (int, string) {
	req := httptest.NewRequest(method, path, nil)
	req.Host = "127.0.0.1:8081"
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	return rr.Code, rr.Body.String()
}

func routeHandled(code int, body string) bool {
	if code == http.StatusMethodNotAllowed {
		return false
	}
	if code == http.StatusNotFound && strings.Contains(body, "unknown API path") {
		return false
	}
	return true
}

func literalCovered(literal string, paths []string) bool {
	if literal == "/_api" || literal == "/_api/" {
		return true
	}
	segment := strings.Trim(literal, "/")
	for _, path := range paths {
		if path == literal || strings.HasPrefix(path, strings.TrimRight(literal, "/")) {
			return true
		}
		if strings.Contains(path, "/"+segment+"/") || strings.HasSuffix(path, "/"+segment) {
			return true
		}
	}
	return false
}

func setDiff(want, have map[string]bool) []string {
	var missing []string
	for key := range want {
		if !have[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}
