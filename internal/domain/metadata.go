// Metadata limits: the reference's JsonValidator rules shared by every
// resource that carries metadata.
package domain

import "encoding/json"

// The metadata limits.
const (
	MetadataMaxBytes = 16 * 1024
	MetadataMaxDepth = 4
	MetadataMaxKeys  = 64
)

// MetadataViolation answers the model-level metadata violation's
// detail ("" when the metadata holds): bytesize, depth, then keys, all
// rendered METADATA_TOO_LONG.
func MetadataViolation(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return ""
	}

	if len(raw) > MetadataMaxBytes {
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
