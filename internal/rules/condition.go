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
	"regexp"
	"sort"
	"strconv"
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
	// Op is eq, prefix, contains, regex, present, or absent. Empty means eq when
	// Value is set and present when it is not, which is how older rules match.
	Op string         `json:"op,omitempty"`
	re *regexp.Regexp `json:"-"`
}

// MatchSpec is the optional query and body conditions on a rule.
// Every condition must hold. An empty spec matches any query and body.
type MatchSpec struct {
	Query   []Condition `json:"query,omitempty"`
	Body    []Condition `json:"body,omitempty"`
	Headers []Condition `json:"headers,omitempty"`
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
		cond, err := parseConditionLine(line, label)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[cond.Key]; ok {
			return nil, fmt.Errorf("duplicate %s condition %q", label, cond.Key)
		}
		seen[cond.Key] = struct{}{}
		out = append(out, cond)
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
	for _, c := range m.Headers {
		parts = append(parts, "header "+formatCondition(c))
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
	op := c.Op
	if op == "" {
		if c.Value == nil {
			return c.Key
		}
		return c.Key + "=" + *c.Value
	}
	if op == "present" {
		return c.Key
	}
	if op == "absent" {
		return c.Key + ":absent"
	}
	value := ""
	if c.Value != nil {
		value = *c.Value
	}
	return c.Key + ":" + op + "=" + value
}

func parseConditionLine(line, label string) (Condition, error) {
	key := line
	op := ""
	var value *string
	if before, after, ok := strings.Cut(line, ":"); ok && isOperator(operatorHead(after)) {
		key = strings.TrimSpace(before)
		name, raw, hasValue := operatorName(after), "", false
		if left, right, ok := strings.Cut(after, "="); ok {
			name = operatorName(left)
			raw = strings.TrimSpace(right)
			hasValue = true
		}
		op = name
		switch op {
		case "present", "absent":
			if hasValue {
				return Condition{}, fmt.Errorf("%s condition %q does not take a value", label, op)
			}
		default:
			if !hasValue {
				return Condition{}, fmt.Errorf("%s condition %q needs a value", label, op)
			}
			value = stringPtr(raw)
		}
	} else if before, after, ok := strings.Cut(line, "="); ok {
		key = strings.TrimSpace(before)
		value = stringPtr(strings.TrimSpace(after))
	}
	if err := validateConditionKey(key, label); err != nil {
		return Condition{}, err
	}
	cond := Condition{Key: key, Op: op, Value: value}
	if err := prepareCondition(&cond, label); err != nil {
		return Condition{}, err
	}
	return cond, nil
}

func operatorName(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func operatorHead(raw string) string {
	if left, _, ok := strings.Cut(raw, "="); ok {
		return operatorName(left)
	}
	return operatorName(raw)
}

func isOperator(name string) bool {
	switch name {
	case "eq", "prefix", "contains", "regex", "present", "absent":
		return true
	default:
		return false
	}
}

func canonicalMatch(m MatchSpec) (MatchSpec, error) {
	if err := validateConditionList(m.Query, "query"); err != nil {
		return MatchSpec{}, err
	}
	if err := validateConditionList(m.Body, "body"); err != nil {
		return MatchSpec{}, err
	}
	if err := validateConditionList(m.Headers, "header"); err != nil {
		return MatchSpec{}, err
	}
	return cloneMatch(m), nil
}

func validateConditionList(list []Condition, label string) error {
	if len(list) > maxConditions {
		return fmt.Errorf("%s conditions are limited to %d", label, maxConditions)
	}
	seen := map[string]struct{}{}
	for i := range list {
		c := list[i]
		if err := validateConditionKey(c.Key, label); err != nil {
			return err
		}
		if c.Value != nil && len(*c.Value) > 4096 {
			return fmt.Errorf("%s condition value for %q is too long", label, c.Key)
		}
		if err := prepareCondition(&list[i], label); err != nil {
			return err
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
	return len(m.Query) + len(m.Body) + len(m.Headers)
}

func cloneMatch(m MatchSpec) MatchSpec {
	return MatchSpec{
		Query:   cloneConditions(m.Query),
		Body:    cloneConditions(m.Body),
		Headers: cloneConditions(m.Headers),
	}
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
		out[i].re = c.re
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
	for _, c := range m.Headers {
		parts = append(parts, "h\x01"+conditionToken(c))
	}
	sort.Strings(parts)
	return strings.Join(parts, "\x02")
}

func conditionToken(c Condition) string {
	op := c.Op
	if op == "" {
		op = "eq"
	}
	if c.Value == nil {
		return c.Key + "\x01" + op
	}
	return c.Key + "\x01" + op + "\x01" + *c.Value
}

func queryMatches(list []Condition, query url.Values) bool {
	for _, c := range list {
		if !valuesMatch(c, query[c.Key]) {
			return false
		}
	}
	return true
}

func headerMatches(list []Condition, header http.Header) bool {
	if header == nil {
		header = http.Header{}
	}
	for _, c := range list {
		if !valuesMatch(c, header.Values(c.Key)) {
			return false
		}
	}
	return true
}

func valuesMatch(c Condition, values []string) bool {
	op := c.Op
	if op == "" {
		if c.Value == nil {
			op = "present"
		} else {
			op = "eq"
		}
	}
	switch op {
	case "absent":
		for _, value := range values {
			if value != "" {
				return false
			}
		}
		return true
	case "present":
		return len(values) > 0
	case "eq", "prefix", "contains", "regex":
		re := c.re
		if op == "regex" && re == nil && c.Value != nil {
			compiled, err := regexp.Compile(*c.Value)
			if err != nil {
				return false
			}
			re = compiled
		}
		for _, value := range values {
			if valueMatches(op, c.Value, re, value) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func valueMatches(op string, want *string, re *regexp.Regexp, value string) bool {
	if want == nil {
		return false
	}
	switch op {
	case "eq":
		return value == *want
	case "prefix":
		return strings.HasPrefix(value, *want)
	case "contains":
		return strings.Contains(value, *want)
	case "regex":
		return re != nil && re.MatchString(value)
	default:
		return false
	}
}

func prepareCondition(c *Condition, label string) error {
	op := c.Op
	if op == "" {
		return nil
	}
	if !isOperator(op) {
		return fmt.Errorf("%s condition operator %q is not supported", label, op)
	}
	if (op == "present" || op == "absent") && c.Value != nil {
		return fmt.Errorf("%s condition %q does not take a value", label, op)
	}
	if op != "present" && op != "absent" && c.Value == nil {
		return fmt.Errorf("%s condition %q needs a value", label, op)
	}
	if op == "regex" {
		re, err := regexp.Compile(*c.Value)
		if err != nil {
			return fmt.Errorf("%s condition regex for %q is invalid", label, c.Key)
		}
		c.re = re
	}
	if op == "present" {
		c.Op = ""
	}
	if op == "eq" {
		c.Op = ""
	}
	return nil
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
		op := c.Op
		if op == "" && c.Value == nil {
			op = "present"
		}
		if op == "present" {
			if !present {
				return false
			}
			continue
		}
		if !present || !scalarOK {
			if !valuesMatch(c, nil) {
				return false
			}
			continue
		}
		if !valuesMatch(c, []string{scalar}) {
			return false
		}
	}
	return true
}

func lookupJSON(root any, key string) (present bool, scalar string, scalarOK bool) {
	cur := root
	for _, part := range strings.Split(key, ".") {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return false, "", false
			}
			cur = next
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return false, "", false
			}
			cur = node[idx]
		default:
			return false, "", false
		}
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
