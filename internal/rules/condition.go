package rules

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/andybalholm/brotli"
)

// MaxMatchBody is the largest request body that body conditions will read.
// A larger body, or one that decompresses past this size, does not satisfy body conditions.
const MaxMatchBody = 1 << 20

const maxConditions = 32

// Condition is one query or body requirement.
// A nil Value means the key must be present. A set Value must match exactly, including empty.
type Condition struct {
	Key   string  `json:"key"`
	Value *string `json:"value,omitempty"`
}

// MatchSpec is the optional query and body conditions on a rule.
// Every condition must hold. An empty spec matches any query and body.
type MatchSpec struct {
	Query []Condition `json:"query,omitempty"`
	Body  []Condition `json:"body,omitempty"`
}

// ParseConditions reads one condition per line.
// A line is key=value, or a bare key meaning the key is present.
// label is "query" or "body" and is used in errors.
func ParseConditions(text, label string) ([]Condition, error) {
	var out []Condition
	seen := map[string]struct{}{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" {
			continue
		}
		if len(out) >= maxConditions {
			return nil, fmt.Errorf("%s conditions are limited to %d", label, maxConditions)
		}
		key := line
		var value *string
		if before, after, ok := strings.Cut(line, "="); ok {
			key = strings.TrimSpace(before)
			value = stringPtr(strings.TrimSpace(after))
		}
		if err := validateConditionKey(key, label); err != nil {
			return nil, err
		}
		if value != nil && len(*value) > 4096 {
			return nil, fmt.Errorf("%s condition value for %q is too long", label, key)
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate %s condition %q", label, key)
		}
		seen[key] = struct{}{}
		out = append(out, Condition{Key: key, Value: value})
	}
	return out, nil
}

// FormatConditions writes conditions one per line, in stored order.
func FormatConditions(list []Condition) string {
	if len(list) == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range list {
		b.WriteString(formatCondition(c))
		b.WriteByte('\n')
	}
	return b.String()
}

// MatchSummary is the rules-list text for a rule's conditions.
func MatchSummary(m MatchSpec) string {
	parts := make([]string, 0, len(m.Query)+len(m.Body))
	for _, c := range m.Query {
		parts = append(parts, "query "+formatCondition(c))
	}
	for _, c := range m.Body {
		parts = append(parts, "body "+formatCondition(c))
	}
	summary := strings.Join(parts, ", ")
	if len(summary) > 80 {
		return summary[:77] + "..."
	}
	return summary
}

func stringPtr(s string) *string {
	return &s
}

func formatCondition(c Condition) string {
	if c.Value == nil {
		return c.Key
	}
	return c.Key + "=" + *c.Value
}

func canonicalMatch(m MatchSpec) (MatchSpec, error) {
	if err := validateConditionList(m.Query, "query"); err != nil {
		return MatchSpec{}, err
	}
	if err := validateConditionList(m.Body, "body"); err != nil {
		return MatchSpec{}, err
	}
	return cloneMatch(m), nil
}

func validateConditionList(list []Condition, label string) error {
	if len(list) > maxConditions {
		return fmt.Errorf("%s conditions are limited to %d", label, maxConditions)
	}
	seen := map[string]struct{}{}
	for _, c := range list {
		if err := validateConditionKey(c.Key, label); err != nil {
			return err
		}
		if c.Value != nil && len(*c.Value) > 4096 {
			return fmt.Errorf("%s condition value for %q is too long", label, c.Key)
		}
		if _, ok := seen[c.Key]; ok {
			return fmt.Errorf("duplicate %s condition %q", label, c.Key)
		}
		seen[c.Key] = struct{}{}
	}
	return nil
}

func validateConditionKey(key, label string) error {
	if key == "" {
		return fmt.Errorf("%s condition is missing a key", label)
	}
	if len(key) > 256 {
		return fmt.Errorf("%s condition key is too long", label)
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s condition key contains a control character", label)
		}
	}
	for _, part := range strings.Split(key, ".") {
		if part == "" {
			return fmt.Errorf("%s condition %q has an empty path segment", label, key)
		}
	}
	return nil
}

func conditionCount(m MatchSpec) int {
	return len(m.Query) + len(m.Body)
}

func cloneMatch(m MatchSpec) MatchSpec {
	return MatchSpec{Query: cloneConditions(m.Query), Body: cloneConditions(m.Body)}
}

func cloneConditions(in []Condition) []Condition {
	if len(in) == 0 {
		return nil
	}
	out := make([]Condition, len(in))
	for i, c := range in {
		out[i] = c
		if c.Value != nil {
			v := *c.Value
			out[i].Value = &v
		}
	}
	return out
}

func ruleSignature(rule Rule) string {
	return rule.Method + "\x00" + rule.PathPattern + "\x00" + rule.UpstreamID + "\x00" + conditionSignature(rule.Match)
}

func conditionSignature(m MatchSpec) string {
	parts := make([]string, 0, conditionCount(m))
	for _, c := range m.Query {
		parts = append(parts, "q\x01"+conditionToken(c))
	}
	for _, c := range m.Body {
		parts = append(parts, "b\x01"+conditionToken(c))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x02")
}

func conditionToken(c Condition) string {
	if c.Value == nil {
		return c.Key + "\x01*"
	}
	return c.Key + "\x01=" + *c.Value
}

func queryMatches(list []Condition, query url.Values) bool {
	for _, c := range list {
		values, ok := query[c.Key]
		if !ok {
			return false
		}
		if c.Value == nil {
			continue
		}
		found := false
		for _, value := range values {
			if value == *c.Value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func bodyMatches(list []Condition, header http.Header, body []byte) bool {
	if header == nil {
		header = http.Header{}
	}
	switch bodyKind(header.Get("Content-Type")) {
	case "json":
		return jsonBodyMatches(list, body)
	case "form":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return false
		}
		return queryMatches(list, values)
	default:
		return false
	}
}

func bodyKind(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		media = strings.TrimSpace(contentType)
	}
	media = strings.ToLower(media)
	switch {
	case media == "application/json" || media == "text/json" || strings.HasSuffix(media, "+json"):
		return "json"
	case media == "application/x-www-form-urlencoded":
		return "form"
	default:
		return ""
	}
}

func jsonBodyMatches(list []Condition, body []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(body)))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return false
	}
	for _, c := range list {
		present, scalar, scalarOK := lookupJSON(root, c.Key)
		if !present {
			return false
		}
		if c.Value == nil {
			continue
		}
		if !scalarOK || scalar != *c.Value {
			return false
		}
	}
	return true
}

func lookupJSON(root any, key string) (present bool, scalar string, scalarOK bool) {
	cur := root
	for _, part := range strings.Split(key, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return false, "", false
		}
		next, ok := obj[part]
		if !ok {
			return false, "", false
		}
		cur = next
	}
	scalar, scalarOK = jsonScalar(cur)
	return true, scalar, scalarOK
}

func jsonScalar(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		if t {
			return "true", true
		}
		return "false", true
	case nil:
		return "null", true
	default:
		return "", false
	}
}

// decodeMatchBody undoes Content-Encoding the same way history does.
// ok is false when the body is over MaxMatchBody or cannot be decoded.
func decodeMatchBody(header http.Header, body []byte) ([]byte, bool) {
	if len(body) > MaxMatchBody {
		return nil, false
	}
	if header == nil {
		return body, true
	}
	encodings := splitMatchEncodings(header.Values("Content-Encoding"))
	data := body
	for i := len(encodings) - 1; i >= 0; i-- {
		enc := encodings[i]
		if enc == "identity" {
			continue
		}
		next, err := inflateMatch(enc, data)
		if err != nil {
			return nil, false
		}
		data = next
	}
	return data, true
}

func splitMatchEncodings(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func inflateMatch(enc string, body []byte) ([]byte, error) {
	switch enc {
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return readMatchLimit(reader)
	case "deflate":
		zlibReader, err := zlib.NewReader(bytes.NewReader(body))
		if err == nil {
			defer zlibReader.Close()
			return readMatchLimit(zlibReader)
		}
		raw := flate.NewReader(bytes.NewReader(body))
		defer raw.Close()
		return readMatchLimit(raw)
	case "br":
		return readMatchLimit(brotli.NewReader(bytes.NewReader(body)))
	default:
		return nil, fmt.Errorf("unsupported content-encoding %q", enc)
	}
}

func readMatchLimit(r io.Reader) ([]byte, error) {
	buf := make([]byte, MaxMatchBody+1)
	n := 0
	for n < len(buf) {
		nn, err := r.Read(buf[n:])
		n += nn
		if err == io.EOF {
			return buf[:n], nil
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("decoded body exceeds %d bytes", MaxMatchBody)
}
