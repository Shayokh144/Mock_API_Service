package admin

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
)

var methods = []string{
	http.MethodGet,
	http.MethodPost,
	http.MethodPut,
	http.MethodPatch,
	http.MethodDelete,
	http.MethodHead,
	http.MethodOptions,
}

type formView struct {
	Action      string
	Submit      string
	Name        string
	Enabled     bool
	Method      string
	PathPattern string
	StatusCode  string
	Headers     string
	Body        string
}

func draftFromForm(r *http.Request) (rules.Draft, formView, error) {
	if err := r.ParseForm(); err != nil {
		return rules.Draft{}, formView{}, fmt.Errorf("could not read the form")
	}
	view := formView{
		Name:        r.FormValue("name"),
		Enabled:     r.FormValue("enabled") == "on",
		Method:      strings.ToUpper(strings.TrimSpace(r.FormValue("method"))),
		PathPattern: strings.TrimSpace(r.FormValue("path_pattern")),
		StatusCode:  strings.TrimSpace(r.FormValue("status_code")),
		Headers:     r.FormValue("headers"),
		Body:        r.FormValue("body"),
	}
	status, err := strconv.Atoi(view.StatusCode)
	if err != nil {
		return rules.Draft{}, view, fmt.Errorf("status_code must be from 100 through 599")
	}
	headers, err := parseHeaderLines(view.Headers)
	if err != nil {
		return rules.Draft{}, view, err
	}
	return rules.Draft{
		Name:        view.Name,
		Enabled:     view.Enabled,
		Method:      view.Method,
		PathPattern: view.PathPattern,
		StatusCode:  status,
		Headers:     headers,
		Body:        view.Body,
	}, view, nil
}

func parseHeaderLines(text string) (map[string]string, error) {
	headers := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("header line %q must look like Name: value", line)
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" {
			return nil, fmt.Errorf("a header name is required")
		}
		headers[name] = value
	}
	return headers, nil
}

func formatHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s: %s\n", key, headers[key])
	}
	return b.String()
}

func viewFromRule(rule rules.Rule, action, submit string) formView {
	return formView{
		Action:      action,
		Submit:      submit,
		Name:        rule.Name,
		Enabled:     rule.Enabled,
		Method:      rule.Method,
		PathPattern: rule.PathPattern,
		StatusCode:  strconv.Itoa(rule.StatusCode),
		Headers:     formatHeaders(rule.Headers),
		Body:        string(rule.Body),
	}
}
