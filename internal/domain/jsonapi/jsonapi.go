// Package jsonapi holds the wire-format transforms every handler shares:
// the metadata key casing (camelCase on the wire, snake_case stored) and
// the timestamp rendering.
package jsonapi

import (
	"strings"
	"time"
	"unicode"
)

// Timestamp renders t exactly like the reference: RFC3339 with exactly
// three fractional digits and a Z suffix, e.g. 2026-10-22T11:23:22.953Z.
func Timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000") + "Z"
}

// UnderscoreKey reproduces Rails' underscore + parameterize('_'): the
// whole request body (metadata keys included) arrives snake_cased.
func UnderscoreKey(key string) string {
	var b strings.Builder
	b.Grow(len(key))

	runes := []rune(key)

	for i, r := range runes {
		switch {
		case r == '-' || r == '_' || r == ' ':
			b.WriteByte('_')
		case unicode.IsUpper(r):
			if i > 0 {
				prev := runes[i-1]

				nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
				if unicode.IsDigit(prev) || unicode.IsLower(prev) ||
					(unicode.IsUpper(prev) && nextLower) {
					b.WriteByte('_')
				}
			}

			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteByte('_')
		}
	}

	return b.String()
}

// CamelizeKey reproduces Rails' camelize(:lower): lowerCamelCase from a
// snake_case or dashed key.
func CamelizeKey(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) == 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(strings.ToLower(parts[0]))

	for _, p := range parts[1:] {
		if p == "" {
			continue
		}

		r := []rune(p)
		b.WriteRune(unicode.ToUpper(r[0]))
		b.WriteString(string(r[1:]))
	}

	return b.String()
}

// UnderscoreDeep transforms every key of the value (maps, recursively;
// slices and their maps too).
func UnderscoreDeep(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[UnderscoreKey(k)] = UnderscoreDeep(item)
		}

		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = UnderscoreDeep(item)
		}

		return out
	default:
		return v
	}
}

// CamelizeDeep transforms every key of the value back to lowerCamelCase.
func CamelizeDeep(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[CamelizeKey(k)] = CamelizeDeep(item)
		}

		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = CamelizeDeep(item)
		}

		return out
	default:
		return v
	}
}
