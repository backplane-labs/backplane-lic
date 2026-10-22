// Metadata limits: the reference's JsonValidator rules shared by every
// resource that carries metadata.
package domain

import (
	"encoding/json"
	"strings"
)

// The metadata limits.
const (
	MetadataMaxBytes = 16 * 1024
	MetadataMaxDepth = 4
	MetadataMaxKeys  = 64
)

// MetadataViolation returns the first model-level metadata violation in the
// reference's wire order: bytesize, depth, then keys. The reference renders
// only the first error for a given attribute and validation code.
func MetadataViolation(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}

	// JSON.generate does not perform Go's HTML/JavaScript escaping.
	if len(raw)-metadataEscapeOverhead(m) > MetadataMaxBytes {
		return "too large (exceeded limit of 16384 bytes)"
	}

	if metadataDepth(m) > MetadataMaxDepth {
		return "too many items (exceeded limit of 4 items)"
	}

	if metadataKeys(m) > MetadataMaxKeys {
		return "too many keys (exceeded limit of 64 keys)"
	}

	return ""
}

// metadataEscapeOverhead is the byte expansion Go adds beyond Ruby's JSON
// representation. Count original characters, not escape sequences, so literal
// strings such as "\\u2028" remain distinct from the Unicode character.
func metadataEscapeOverhead(value any) int {
	switch typed := value.(type) {
	case string:
		const (
			htmlExpansion      = 5
			separatorExpansion = 3
		)

		return htmlExpansion*(strings.Count(typed, "<")+strings.Count(typed, ">")+strings.Count(typed, "&")) +
			separatorExpansion*(strings.Count(typed, "\u2028")+strings.Count(typed, "\u2029"))
	case map[string]any:
		overhead := 0
		for key, item := range typed {
			overhead += metadataEscapeOverhead(key) + metadataEscapeOverhead(item)
		}

		return overhead
	case []any:
		overhead := 0
		for _, item := range typed {
			overhead += metadataEscapeOverhead(item)
		}

		return overhead
	default:
		return 0
	}
}

func metadataDepth(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		depth := 0
		for _, inner := range typed {
			if inner := metadataDepth(inner); inner > depth {
				depth = inner
			}
		}

		return 1 + depth
	case []any:
		depth := 0
		for _, inner := range typed {
			if inner := metadataDepth(inner); inner > depth {
				depth = inner
			}
		}

		return 1 + depth
	default:
		return 0
	}
}

func metadataKeys(value any) int {
	switch typed := value.(type) {
	case map[string]any:
		count := len(typed)
		for _, inner := range typed {
			count += metadataKeys(inner)
		}

		return count
	case []any:
		count := 0
		for _, inner := range typed {
			count += metadataKeys(inner)
		}

		return count
	default:
		return 0
	}
}
