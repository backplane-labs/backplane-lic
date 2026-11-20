package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
)

// The JSON:API names the declared policy's body repeats.
const (
	keyRelationships = "relationships"
	typeProducts     = "products"
)

// PolicyAttributes decodes a declared policy's Keygen attributes (the
// API's camelCase names) exactly as POST /policies decodes them, with the
// policy bound to the product.
func PolicyAttributes(attrs map[string]any, productID uuid.UUID) (policies.Input, error) {
	raw, err := json.Marshal(map[string]any{"data": map[string]any{
		metaType:      "policies",
		keyAttributes: attrs,
		keyRelationships: map[string]any{"product": map[string]any{
			"data": map[string]any{metaType: typeProducts, "id": productID.String()},
		}},
	}})
	if err != nil {
		return policies.Input{}, fmt.Errorf("httpapi: policy attributes: %w", err)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var body map[string]any
	if err := decoder.Decode(&body); err != nil {
		return policies.Input{}, fmt.Errorf("httpapi: policy attributes: %w", err)
	}

	under, _ := jsonapi.UnderscoreDeep(body).(map[string]any)

	in, err := validatePolicyBody(under, false)
	if err != nil {
		return policies.Input{}, fmt.Errorf("httpapi: policy attributes: %w", err)
	}

	return in, nil
}
