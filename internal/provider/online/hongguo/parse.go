package hongguo

import (
	"bytes"
	"encoding/json"
	"errors"
)

// ErrNotFound is returned when the expected embedded object is missing.
var ErrNotFound = errors.New("hongguo: embedded object not found")

// extractJSONObject finds the JSON object in s that contains the given key.
//
// The player page embeds several sibling JSON blobs inside a <script> tag,
// none of which is wrapped in a variable we can address directly. Instead of
// guessing, we locate the key, then walk backwards over candidate '{'
// positions and return the first candidate that both balances and parses.
//
// Matching is done with a scanner that understands JSON string literals, so
// braces inside strings cannot desynchronise the counter.
func extractJSONObject(s, key string) ([]byte, bool) {
	needle := []byte(`"` + key + `"`)
	ki := bytes.Index([]byte(s), needle)
	if ki < 0 {
		return nil, false
	}

	for start := bytes.LastIndex([]byte(s[:ki]), []byte("{")); start >= 0; {
		if obj, ok := matchBalanced(s, start); ok {
			if bytes.Contains(obj, needle) {
				return obj, true
			}
		}
		prev := bytes.LastIndex([]byte(s[:start]), []byte("{"))
		if prev < 0 {
			break
		}
		start = prev
	}
	return nil, false
}

// matchBalanced returns the {...} slice starting at s[start], honouring JSON
// string literals and escapes.
func matchBalanced(s string, start int) ([]byte, bool) {
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(s); i++ {
		ch := s[i]

		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return []byte(s[start : i+1]), true
			}
		}
	}
	return nil, false
}

// decodeEmbedded unmarshals the embedded object containing key into v.
func decodeEmbedded(s, key string, v any) error {
	obj, ok := extractJSONObject(s, key)
	if !ok {
		return ErrNotFound
	}
	return json.Unmarshal(obj, v)
}
