// The request-layer metadata depth rule: every request body's metadata
// hash nests at most two levels (typed_params depth maximum 2); a
// deeper hash is the 400 "maximum depth of 2 exceeded" pointing at the
// offending key.
package httpapi

import (
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
)

// metadataDepthLimit is the request body's metadata nesting limit.
const metadataDepthLimit = 2

// metadataOf reads a metadata hash, enforcing the request-layer depth
// rule (the pointer is where the metadata lives, e.g.
// /data/attributes/metadata).
func metadataOf(raw any, pointer string) (map[string]any, error) {
	meta, err := hash(raw, pointer)
	if err != nil {
		return nil, err
	}

	if err := metadataDepthCheck(meta, pointer, 0); err != nil {
		return nil, err
	}

	return meta, nil
}

func metadataDepthCheck(m map[string]any, pointer string, level int) error {
	for key, value := range m {
		inner, ok := value.(map[string]any)
		if !ok {
			continue
		}

		path := pointer + "/" + jsonapi.CamelizeKey(key)

		if level+1 > metadataDepthLimit {
			return apperr.BadRequestSource("maximum depth of 2 exceeded", path)
		}

		if err := metadataDepthCheck(inner, path, level+1); err != nil {
			return err
		}
	}

	return nil
}
