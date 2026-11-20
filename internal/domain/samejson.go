package domain

import (
	"bytes"
	"encoding/json"
)

// SameJSON reports whether two values encode to the same JSON: the
// declared catalog's equality, under which a nil and an empty list or a
// number decoded from the configuration and from a row compare equal
// only when they mean the same.
func SameJSON(a, b any) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)

	return errA == nil && errB == nil && bytes.Equal(left, right)
}
