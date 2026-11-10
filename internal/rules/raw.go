package rules

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
)

const (
	// ModeFields builds the response from status, headers, and a JSON body.
	ModeFields = "fields"
	// ModeRaw sends a pasted HTTP response.
	ModeRaw = "raw"
)

// Header is one raw response header, in the order it was pasted.
type Header struct {
	Name  string
	Value string
}

// ParsedRaw is a validated HTTP/1 response paste.
type ParsedRaw struct {
	Proto   string
	Code    int
	Reason  string
	Headers []Header
	Body    []byte
}

// ParseRawResponse reads a status line, headers, a blank line, and a body.
// Line endings may be CRLF or LF. The body bytes after the blank line are kept as pasted.
func ParseRawResponse(raw string) (ParsedRaw, error) {
	if strings.TrimSpace(raw) == "" {
		return ParsedRaw{}, fmt.Errorf("raw response must start with a status line")
	}
	if len(raw) > config.MaxMockBody {
		return ParsedRaw{}, fmt.Errorf("raw response must be 1 MiB or smaller")
	}
	head, body, ok := splitRawHead(raw)
	if !ok {
		return ParsedRaw{}, fmt.Errorf("raw response needs a blank line between the headers and the body")
	}
	lines := splitHeaderLines(head)
	if len(lines) == 0 || lines[0] == "" {
		return ParsedRaw{}, fmt.Errorf("raw response must start with a status line")
	}
	proto, code, reason, err := parseStatusLine(lines[0])
	if err != nil {
		return ParsedRaw{}, err
	}
	headers := make([]Header, 0, len(lines)-1)
	for _, line := range lines[1:] {
		if line == "" {
			return ParsedRaw{}, fmt.Errorf("raw response needs a blank line between the headers and the body")
		}
		name, value, err := parseRawHeader(line)
		if err != nil {
			return ParsedRaw{}, err
		}
		if strings.EqualFold(name, "Transfer-Encoding") {
			return ParsedRaw{}, fmt.Errorf("raw response must not set Transfer-Encoding")
		}
		headers = append(headers, Header{Name: name, Value: value})
	}
	return ParsedRaw{Proto: proto, Code: code, Reason: reason, Headers: headers, Body: []byte(body)}, nil
}

// WantsClose reports whether the pasted headers ask the server to close the connection.
func WantsClose(parsed ParsedRaw) bool {
	for _, header := range parsed.Headers {
		if strings.EqualFold(header.Name, "Connection") && headerToken(header.Value, "close") {
			return true
		}
	}
	return false
}

// WireBytes is the HTTP/1 message to write, with Content-Length recomputed from the body.
func WireBytes(parsed ParsedRaw, mockHeader bool) []byte {
	var buf bytes.Buffer
	reason := parsed.Reason
	if reason == "" {
		reason = http.StatusText(parsed.Code)
	}
	fmt.Fprintf(&buf, "%s %d", parsed.Proto, parsed.Code)
	if reason != "" {
		buf.WriteByte(' ')
		buf.WriteString(reason)
	}
	buf.WriteString("\r\n")
	for _, header := range parsed.Headers {
		if strings.EqualFold(header.Name, "Content-Length") || strings.EqualFold(header.Name, "Transfer-Encoding") {
			continue
		}
		fmt.Fprintf(&buf, "%s: %s\r\n", header.Name, header.Value)
	}
	if mockHeader {
		buf.WriteString("X-Mock-Response: true\r\n")
	}
	fmt.Fprintf(&buf, "Content-Length: %d\r\n\r\n", len(parsed.Body))
	buf.Write(parsed.Body)
	return buf.Bytes()
}

func splitRawHead(raw string) (head, body string, ok bool) {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\n' {
			continue
		}
		lineStart := i
		for lineStart > 0 && raw[lineStart-1] != '\n' {
			lineStart--
		}
		line := strings.TrimRight(raw[lineStart:i], "\r")
		if line == "" {
			return raw[:lineStart], raw[i+1:], true
		}
	}
	return "", "", false
}

func splitHeaderLines(head string) []string {
	if head == "" {
		return nil
	}
	rawLines := strings.Split(head, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		line = strings.TrimRight(line, "\r")
		lines = append(lines, line)
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func parseStatusLine(line string) (proto string, code int, reason string, err error) {
	fields := strings.Fields(line)
	if len(fields) < 2 || !isHTTP1(fields[0]) {
		return "", 0, "", fmt.Errorf("status line must look like HTTP/1.1 401 Unauthorized")
	}
	code, err = strconv.Atoi(fields[1])
	if err != nil || code < 100 || code > 599 {
		return "", 0, "", fmt.Errorf("status code must be from 100 through 599")
	}
	reason = ""
	if len(fields) > 2 {
		reason = strings.TrimSpace(line[strings.Index(line, fields[1])+len(fields[1]):])
	}
	return fields[0], code, reason, nil
}

func isHTTP1(proto string) bool {
	if len(proto) != len("HTTP/1.1") || !strings.EqualFold(proto[:5], "HTTP/") {
		return false
	}
	return proto[5:] == "1.0" || proto[5:] == "1.1"
}

func parseRawHeader(line string) (string, string, error) {
	name, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", "", fmt.Errorf("header line %q must look like Name: value", line)
	}
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" || !validHeaderToken(name) {
		return "", "", fmt.Errorf("header line %q must look like Name: value", line)
	}
	if strings.ContainsAny(value, "\r\n") {
		return "", "", fmt.Errorf("header line %q must look like Name: value", line)
	}
	return name, value, nil
}

func headerToken(value, token string) bool {
	for _, part := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(part), token) {
			return true
		}
	}
	return false
}

func responseMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", ModeFields:
		return "", nil
	case ModeRaw:
		return ModeRaw, nil
	default:
		return "", fmt.Errorf("response mode must be fields or raw")
	}
}
