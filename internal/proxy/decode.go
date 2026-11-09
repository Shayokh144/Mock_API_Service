package proxy

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
	"strings"
	"unicode/utf8"

	"github.com/andybalholm/brotli"

	"github.com/Shayokh144/Mock_API_Service/internal/config"
)

// displayBody is the history copy of a body. It decodes Content-Encoding and
// keeps at most the history preview. The caller still forwards the original bytes.
func displayBody(headers http.Header, body []byte) (string, bool) {
	if len(body) == 0 {
		return "", false
	}
	if headers == nil {
		headers = http.Header{}
	}
	decoded, truncated, note := decodeEncodings(headers.Values("Content-Encoding"), body)
	if note != "" {
		return note, truncated
	}
	return presentBody(headers.Get("Content-Type"), decoded, truncated)
}

func decodeEncodings(values []string, body []byte) ([]byte, bool, string) {
	encodings := splitEncodings(values)
	data := body
	for i := len(encodings) - 1; i >= 0; i-- {
		enc := encodings[i]
		if enc == "identity" {
			continue
		}
		next, truncated, err := inflate(enc, data)
		if err != nil {
			return nil, false, err.Error()
		}
		if truncated && i > 0 {
			return nil, true, fmt.Sprintf("could not decode content-encoding %q: decompressed copy exceeds %d bytes", strings.Join(encodings, ", "), config.HistoryBodyPreview)
		}
		data = next
		if truncated {
			return data, true, ""
		}
	}
	return data, false, ""
}

func splitEncodings(values []string) []string {
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

func inflate(enc string, body []byte) ([]byte, bool, error) {
	switch enc {
	case "gzip":
		reader, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false, fmt.Errorf("could not decode content-encoding %q: corrupt body", enc)
		}
		defer reader.Close()
		return readDecoded(reader, enc)
	case "deflate":
		return inflateDeflate(body)
	case "br":
		return readDecoded(brotli.NewReader(bytes.NewReader(body)), enc)
	default:
		return nil, false, fmt.Errorf("could not decode content-encoding %q: unsupported encoding", enc)
	}
}

func inflateDeflate(body []byte) ([]byte, bool, error) {
	zlibReader, err := zlib.NewReader(bytes.NewReader(body))
	if err == nil {
		defer zlibReader.Close()
		return readDecoded(zlibReader, "deflate")
	}
	raw := flate.NewReader(bytes.NewReader(body))
	defer raw.Close()
	return readDecoded(raw, "deflate")
}

func readDecoded(r io.Reader, enc string) ([]byte, bool, error) {
	limit := config.HistoryBodyPreview
	buf := make([]byte, limit+1)
	n := 0
	for n < len(buf) {
		nn, err := r.Read(buf[n:])
		n += nn
		if err == io.EOF {
			return buf[:n], false, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("could not decode content-encoding %q: corrupt body", enc)
		}
	}
	return buf[:limit], true, nil
}

func presentBody(contentType string, data []byte, truncated bool) (string, bool) {
	if len(data) > config.HistoryBodyPreview {
		data = data[:config.HistoryBodyPreview]
		truncated = true
	}
	if truncated {
		data = cutUTF8(data, len(data))
	}
	if isBinary(contentType, data) {
		return binaryNote(contentType, len(data), truncated), truncated
	}
	if !truncated && isJSON(contentType, data) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, data, "", "  "); err == nil {
			formatted := pretty.Bytes()
			if len(formatted) > config.HistoryBodyPreview {
				return string(cutUTF8(formatted, config.HistoryBodyPreview)), true
			}
			return pretty.String(), false
		}
	}
	return string(cutUTF8(data, len(data))), truncated
}

func isBinary(contentType string, data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return true
	}
	media := mediaType(contentType)
	switch {
	case media == "":
		return false
	case strings.HasPrefix(media, "text/"):
		return false
	case media == "application/json" || strings.HasSuffix(media, "+json"):
		return false
	case media == "application/xml" || strings.HasSuffix(media, "+xml"):
		return false
	case media == "application/javascript" || media == "application/xhtml+xml":
		return false
	case strings.HasPrefix(media, "image/"), strings.HasPrefix(media, "audio/"), strings.HasPrefix(media, "video/"), strings.HasPrefix(media, "font/"):
		return true
	case media == "application/octet-stream" || media == "application/pdf" || media == "application/zip" || media == "application/gzip" || media == "application/wasm" || media == "application/x-protobuf":
		return true
	default:
		return false
	}
}

func isJSON(contentType string, data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || (trimmed[0] != '{' && trimmed[0] != '[') {
		return false
	}
	media := mediaType(contentType)
	if media != "" && media != "application/json" && !strings.HasSuffix(media, "+json") && media != "text/json" && !strings.HasPrefix(media, "text/") {
		return false
	}
	return json.Valid(trimmed)
}

func binaryNote(contentType string, n int, truncated bool) string {
	media := mediaType(contentType)
	if media == "" {
		media = "unknown"
	}
	if truncated {
		return fmt.Sprintf("binary body, more than %d bytes, content-type %s", n, media)
	}
	return fmt.Sprintf("binary body, %d bytes, content-type %s", n, media)
}

func mediaType(contentType string) string {
	media, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	return strings.ToLower(media)
}

func cutUTF8(b []byte, max int) []byte {
	if len(b) > max {
		b = b[:max]
	}
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}
