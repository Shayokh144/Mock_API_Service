package scenario

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/rules"
	"gopkg.in/yaml.v3"
)

const MaxRules = 500

// Document is a version 1 scenario.
type Document struct {
	Version     int    `json:"version" yaml:"version"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Rules       []Rule `json:"rules" yaml:"rules"`
}

// Rule is one scenario rule. The fields match the rules API.
type Rule struct {
	Name         string   `json:"name" yaml:"name"`
	Description  string   `json:"description" yaml:"description"`
	Upstream     string   `json:"upstream" yaml:"upstream"`
	Method       string   `json:"method" yaml:"method"`
	Path         string   `json:"path" yaml:"path"`
	Query        []string `json:"query_conditions" yaml:"query_conditions"`
	BodyMatch    []string `json:"body_conditions" yaml:"body_conditions"`
	Status       int      `json:"status" yaml:"status"`
	Headers      any      `json:"headers" yaml:"headers"`
	Body         any      `json:"body" yaml:"body"`
	RawResponse  string   `json:"raw_response" yaml:"raw_response"`
	DelayMS      int      `json:"delay_ms" yaml:"delay_ms"`
	Enabled      *bool    `json:"enabled" yaml:"enabled"`
	FreshRequest bool     `json:"fresh_request" yaml:"fresh_request"`
	MockHeader   bool     `json:"mock_header" yaml:"mock_header"`
}

// Parse reads a JSON or YAML scenario. format is "json" or "yaml".
func Parse(data []byte, format string) (Document, error) {
	if len(data) > 1<<20 {
		return Document{}, errors.New("scenario file must be 1 MiB or smaller")
	}
	var doc Document
	switch format {
	case "json":
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&doc); err != nil {
			return Document{}, fmt.Errorf("scenario: %s", cleanJSON(err))
		}
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			return Document{}, errors.New("scenario contains extra JSON")
		}
	case "yaml":
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&doc); err != nil {
			return Document{}, fmt.Errorf("scenario: %s", err.Error())
		}
		var extra any
		if err := dec.Decode(&extra); err == nil && extra != nil {
			return Document{}, errors.New("scenario contains extra YAML")
		}
	default:
		return Document{}, errors.New("scenario format must be json or yaml")
	}
	if doc.Version != 1 {
		return Document{}, errors.New("scenario version must be 1")
	}
	if len(doc.Rules) > MaxRules {
		return Document{}, errors.New("a scenario can hold 500 rules")
	}
	for i := range doc.Rules {
		if strings.TrimSpace(doc.Rules[i].Name) == "" {
			return Document{}, fmt.Errorf("rules[%d].name is required", i)
		}
	}
	return doc, nil
}

// Format returns "yaml" or "json" from a file name or content type.
func Format(name, contentType string) string {
	lower := strings.ToLower(name + " " + contentType)
	switch {
	case strings.Contains(lower, "yaml") || strings.Contains(lower, ".yml"):
		return "yaml"
	default:
		return "json"
	}
}

// Drafts converts the document into rule drafts. resolve maps an upstream name to an id.
func (d Document) Drafts(resolve func(string) (string, error)) ([]rules.Draft, error) {
	out := make([]rules.Draft, 0, len(d.Rules))
	seen := map[string]struct{}{}
	for i, rule := range d.Rules {
		name, err := rules.NormalizeAPIName(rule.Name)
		if err != nil || name == "" {
			return nil, fmt.Errorf("rules[%d].name must match [a-z0-9][a-z0-9._-]{0,63}", i)
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("rules[%d].name %s is repeated", i, name)
		}
		seen[name] = struct{}{}
		upstreamID, err := resolve(rule.Upstream)
		if err != nil {
			return nil, fmt.Errorf("rules[%d].upstream: %s", i, err.Error())
		}
		query, err := rules.ParseConditions(strings.Join(rule.Query, "\n"), "query")
		if err != nil {
			return nil, fmt.Errorf("rules[%d].query_conditions: %s", i, err.Error())
		}
		bodyMatch, err := rules.ParseConditions(strings.Join(rule.BodyMatch, "\n"), "body")
		if err != nil {
			return nil, fmt.Errorf("rules[%d].body_conditions: %s", i, err.Error())
		}
		headers, err := headerMap(rule.Headers)
		if err != nil {
			return nil, fmt.Errorf("rules[%d].headers: %s", i, err.Error())
		}
		mode := ""
		raw := strings.TrimSpace(rule.RawResponse)
		payload := ""
		if raw != "" {
			mode = rules.ModeRaw
		} else {
			payload, err = bodyText(rule.Body)
			if err != nil {
				return nil, fmt.Errorf("rules[%d].body: %s", i, err.Error())
			}
		}
		enabled := true
		if rule.Enabled != nil {
			enabled = *rule.Enabled
		}
		out = append(out, rules.Draft{
			Name:         rule.Description,
			APIName:      name,
			Enabled:      enabled,
			FreshRequest: rule.FreshRequest,
			UpstreamID:   upstreamID,
			Method:       rule.Method,
			PathPattern:  rule.Path,
			Match:        rules.MatchSpec{Query: query, Body: bodyMatch},
			StatusCode:   rule.Status,
			DelayMS:      rule.DelayMS,
			ResponseMode: mode,
			RawResponse:  raw,
			MockHeader:   rule.MockHeader,
			Headers:      headers,
			Body:         payload,
		})
	}
	return out, nil
}

// FromRules builds a scenario from stored rules. A rule with no stable name uses its id.
func FromRules(name, description string, list []rules.Rule, upstreamName func(id string) string) Document {
	doc := Document{Version: 1, Name: name, Description: description, Rules: []Rule{}}
	for _, rule := range list {
		stable := rule.APIName
		if stable == "" {
			stable = rule.ID
		}
		enabled := rule.Enabled
		item := Rule{
			Name:         stable,
			Description:  rule.Name,
			Upstream:     upstreamName(rule.UpstreamID),
			Method:       rule.Method,
			Path:         rule.PathPattern,
			Query:        lines(rule.Match.Query),
			BodyMatch:    lines(rule.Match.Body),
			Status:       rule.StatusCode,
			DelayMS:      rule.DelayMS,
			Enabled:      &enabled,
			FreshRequest: rule.FreshRequest,
			MockHeader:   rule.MockHeader,
			RawResponse:  rule.RawResponse,
		}
		if len(rule.Headers) > 0 {
			item.Headers = rule.Headers
		}
		if len(rule.Body) > 0 && string(rule.Body) != "null" && rule.ResponseMode != rules.ModeRaw {
			var parsed any
			if err := json.Unmarshal(rule.Body, &parsed); err == nil {
				item.Body = parsed
			} else {
				item.Body = string(rule.Body)
			}
		}
		doc.Rules = append(doc.Rules, item)
	}
	return doc
}

// Marshal encodes a scenario as JSON or YAML.
func Marshal(doc Document, format string) ([]byte, error) {
	if format == "yaml" {
		return yaml.Marshal(doc)
	}
	payload, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func lines(list []rules.Condition) []string {
	text := rules.FormatConditions(list)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func headerMap(value any) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]string{}
		for key, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("header values must be strings")
			}
			out[key] = text
		}
		return out, nil
	case map[string]string:
		return typed, nil
	case []any:
		out := map[string]string{}
		for _, item := range typed {
			line, ok := item.(string)
			if !ok {
				return nil, errors.New("header list items must be strings")
			}
			name, rest, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(name) == "" {
				return nil, errors.New("headers must be Name: value")
			}
			out[strings.TrimSpace(name)] = strings.TrimSpace(rest)
		}
		return out, nil
	default:
		return nil, errors.New("headers must be an object or a list of pairs")
	}
}

func bodyText(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	if text, ok := value.(string); ok {
		return text, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("body must be JSON")
	}
	return string(payload), nil
}

func cleanJSON(err error) string {
	text := err.Error()
	text = strings.TrimPrefix(text, "json: ")
	return text
}
