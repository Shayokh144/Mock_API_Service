package api

// Route is one method and path the admin API dispatches.
// Path templates use the same {parameter} names as openapi.yaml.
// ServeHTTP is the dispatcher. The OpenAPI test fails when this list,
// the spec, and live requests disagree.
type Route struct {
	Method string
	Path   string
}

// Routes returns every /_api operation the router serves.
func Routes() []Route {
	return []Route{
		{Method: "GET", Path: "/_api/health"},
		{Method: "POST", Path: "/_api/token/rotate"},
		{Method: "GET", Path: "/_api/sessions"},
		{Method: "POST", Path: "/_api/sessions"},
		{Method: "GET", Path: "/_api/sessions/{session_id}"},
		{Method: "DELETE", Path: "/_api/sessions/{session_id}"},
		{Method: "GET", Path: "/_api/sessions/{session_id}/rules"},
		{Method: "POST", Path: "/_api/sessions/{session_id}/rules"},
		{Method: "GET", Path: "/_api/sessions/{session_id}/rules/{rule_id}"},
		{Method: "PUT", Path: "/_api/sessions/{session_id}/rules/{rule_id}"},
		{Method: "DELETE", Path: "/_api/sessions/{session_id}/rules/{rule_id}"},
		{Method: "POST", Path: "/_api/sessions/{session_id}/rules/{rule_id}/enable"},
		{Method: "POST", Path: "/_api/sessions/{session_id}/rules/{rule_id}/disable"},
		{Method: "GET", Path: "/_api/sessions/{session_id}/rules/by-name/{name}"},
		{Method: "PUT", Path: "/_api/sessions/{session_id}/rules/by-name/{name}"},
		{Method: "DELETE", Path: "/_api/sessions/{session_id}/rules/by-name/{name}"},
		{Method: "POST", Path: "/_api/sessions/{session_id}/rules/by-name/{name}/enable"},
		{Method: "POST", Path: "/_api/sessions/{session_id}/rules/by-name/{name}/disable"},
		{Method: "POST", Path: "/_api/sessions/{session_id}/reset"},
		{Method: "GET", Path: "/_api/sessions/{session_id}/history"},
		{Method: "DELETE", Path: "/_api/sessions/{session_id}/history"},
		{Method: "GET", Path: "/_api/sessions/{session_id}/scenario"},
		{Method: "POST", Path: "/_api/sessions/{session_id}/scenarios/load"},
	}
}
