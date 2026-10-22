// Request-layer metadata containers have typed_params' depth limit of two.
// Arrays consume a nesting level just like hashes and include their index in
// the error pointer. The model separately enforces its depth-four limit.
package httpapi

import (
	"strconv"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
)

const metadataDepthLimit = 2

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

func metadataDepthCheck(value any, pointer string, level int) error {
	switch node := value.(type) {
	case map[string]any:
		if level > metadataDepthLimit {
			return apperr.BadRequestSource("maximum depth of 2 exceeded", pointer)
		}

		for _, key := range sortedKeys(node) {
			path := pointer + "/" + jsonapi.CamelizeKey(key)
			if err := metadataDepthCheck(node[key], path, level+1); err != nil {
				return err
			}
		}
	case []any:
		if level > metadataDepthLimit {
			return apperr.BadRequestSource("maximum depth of 2 exceeded", pointer)
		}

		for index, item := range node {
			if err := metadataDepthCheck(item, pointer+"/"+strconv.Itoa(index), level+1); err != nil {
				return err
			}
		}
	}

	return nil
}
