package httpapi

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
)

// The singular wire spellings of the bearer resource types (the
// reference accepts both forms).
const (
	typeUserSingular    = "user"
	typeProductSingular = "product"
	typeLicenseSingular = "license"
)

// validateGenerateBody shape-validates the decoded (key-underscored)
// body of POST /tokens exactly like the reference's typed params: only
// data and meta at the top; data carries type/attributes{name,expiry}
// and relationships{bearer{data{type,id}}}; meta carries otp. Every
// unknown key is an "unpermitted parameter" pointing at its path, every
// absent required key an "is missing", every wrong-typed value a "type
// mismatch".
func validateGenerateBody(body map[string]any) (tokens.GenerateInput, error) {
	var in tokens.GenerateInput

	if body == nil {
		return in, nil
	}

	if err := unpermitted(body, "/", "data", "meta"); err != nil {
		return in, err
	}

	data, hasData := body["data"]
	if !hasData {
		return in, validateMeta(body)
	}

	obj, err := hash(data, "/data")
	if err != nil {
		return in, err
	}

	if err := unpermitted(obj, "/data/", "type", "attributes", "relationships"); err != nil {
		return in, err
	}

	typeRaw, hasType := obj["type"]
	if !hasType {
		return in, apperr.BadRequestSource("is missing", "/data/type")
	}

	typeStr, err := str(typeRaw, "/data/type")
	if err != nil {
		return in, err
	}

	if typeStr != "token" && typeStr != "tokens" {
		return in, apperr.BadRequestSource("is invalid", "/data/type")
	}

	if attrs, hasAttrs := obj["attributes"]; hasAttrs {
		if err := validateAttributes(attrs, &in); err != nil {
			return in, err
		}
	}

	if rels, hasRels := obj["relationships"]; hasRels {
		if err := validateRelationships(rels, &in); err != nil {
			return in, err
		}
	}

	return in, validateMeta(body)
}

// validateAttributes checks data.attributes: only expiry and name, both
// optional strings (an unparseable expiry is dropped, like the
// reference's lax time coercion).
func validateAttributes(attrs any, in *tokens.GenerateInput) error {
	attrObj, err := hash(attrs, "/data/attributes")
	if err != nil {
		return err
	}

	if err := unpermitted(attrObj, "/data/attributes/", "expiry", "name"); err != nil {
		return err
	}

	_, in.ExpirySet = attrObj["expiry"]
	if raw, present := attrObj["expiry"]; present && raw != nil {
		s, err := str(raw, "/data/attributes/expiry")
		if err != nil {
			return err
		}

		if t, parsed := parseTime(s); parsed {
			in.Expiry = &t
		}
	}

	if raw, present := attrObj["name"]; present && raw != nil {
		s, err := str(raw, "/data/attributes/name")
		if err != nil {
			return err
		}

		in.Name = &s
	}

	return nil
}

// validateRelationships checks data.relationships: only bearer, whose
// data object selects the token's bearer (the admin override).
func validateRelationships(rels any, in *tokens.GenerateInput) error {
	relObj, err := hash(rels, "/data/relationships")
	if err != nil {
		return err
	}

	if err := unpermitted(relObj, "/data/relationships/", "bearer"); err != nil {
		return err
	}

	if bearerRaw, present := relObj["bearer"]; present {
		in.BearerType, in.BearerID, err = validateBearer(bearerRaw)
		if err != nil {
			return err
		}
	}

	return nil
}

// validateMeta checks the top-level meta: only otp, required string.
func validateMeta(body map[string]any) error {
	raw, ok := body["meta"]
	if !ok {
		return nil
	}

	obj, err := hash(raw, "/meta")
	if err != nil {
		return err
	}

	if err := unpermitted(obj, "/meta/", "otp"); err != nil {
		return err
	}

	if _, present := obj["otp"]; !present {
		return apperr.BadRequestSource("is missing", "/meta/otp")
	}

	_, err = str(obj["otp"], "/meta/otp")

	return err
}

// validateBearer checks relationships.bearer: a required data object of
// {type, id} where type is one of the bearer resource types and id a
// UUID.
func validateBearer(raw any) (string, uuid.UUID, error) {
	var bearerType string

	obj, err := hash(raw, "/data/relationships/bearer")
	if err != nil {
		return "", uuid.Nil, err
	}

	if err := unpermitted(obj, "/data/relationships/bearer/", "data"); err != nil {
		return "", uuid.Nil, err
	}

	dataRaw, ok := obj["data"]
	if !ok {
		return "", uuid.Nil, apperr.BadRequestSource("is missing", "/data/relationships/bearer/data")
	}

	data, err := hash(dataRaw, "/data/relationships/bearer/data")
	if err != nil {
		return "", uuid.Nil, err
	}

	if err := unpermitted(data, "/data/relationships/bearer/data/", "type", "id"); err != nil {
		return "", uuid.Nil, err
	}

	typeRaw, ok := data["type"]
	if !ok {
		return "", uuid.Nil, apperr.BadRequestSource("is missing", "/data/relationships/bearer/data/type")
	}

	typeStr, err := str(typeRaw, "/data/relationships/bearer/data/type")
	if err != nil {
		return "", uuid.Nil, err
	}

	switch typeStr {
	case typeUserSingular, access.BearerUser:
		bearerType = access.BearerUser
	case typeProductSingular, access.BearerProduct:
		bearerType = access.BearerProduct
	case typeLicenseSingular, access.BearerLicense:
		bearerType = access.BearerLicense
	default:
		return "", uuid.Nil, apperr.BadRequestSource("is invalid", "/data/relationships/bearer/data/type")
	}

	idRaw, ok := data["id"]
	if !ok {
		return "", uuid.Nil, apperr.BadRequestSource("is missing", "/data/relationships/bearer/data/id")
	}

	idStr, err := str(idRaw, "/data/relationships/bearer/data/id")
	if err != nil {
		return "", uuid.Nil, err
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return "", uuid.Nil, apperr.BadRequestSource(
			"type mismatch (received string expected UUID string)",
			"/data/relationships/bearer/data/id",
		)
	}

	return bearerType, id, nil
}

// unpermitted scans a level's keys: the first key outside allowed is
// the "unpermitted parameter" error (it wins over missing checks, like
// the reference).
func unpermitted(obj map[string]any, prefix string, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, a := range allowed {
		set[a] = struct{}{}
	}

	// Deterministic order (multiple violations are an error either way).
	// The pointer names the parameter as the client spelled it
	// (camelCase), not the decoder's underscored form.
	for _, k := range sortedKeys(obj) {
		if _, ok := set[k]; !ok {
			return apperr.BadRequestSource("unpermitted parameter", prefix+jsonapi.CamelizeKey(k))
		}
	}

	return nil
}

// hash requires a JSON object at pointer.
func hash(raw any, pointer string) (map[string]any, error) {
	switch v := raw.(type) {
	case map[string]any:
		return v, nil
	case nil:
		return nil, apperr.BadRequestSource("is missing", pointer)
	default:
		return nil, apperr.BadRequestSource(
			fmt.Sprintf("type mismatch (received %s expected hash)", jsonKind(raw)),
			pointer,
		)
	}
}

// str requires a JSON string at pointer.
func str(raw any, pointer string) (string, error) {
	s, ok := raw.(string)
	if !ok {
		return "", apperr.BadRequestSource(
			fmt.Sprintf("type mismatch (received %s expected string)", jsonKind(raw)),
			pointer,
		)
	}

	return s, nil
}

// jsonKind names the JSON kind of a decoded value like the reference's
// error details do (integral numbers are "integer").
func jsonKind(raw any) string {
	switch v := raw.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if v == trunc(v) {
			return "integer"
		}

		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "hash"
	default:
		return "unknown"
	}
}

func trunc(v float64) float64 {
	if v < 0 {
		return -float64(int64(-v))
	}

	return float64(int64(v))
}

func sortedKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}

	sortStrings(keys)

	return keys
}

func sortStrings(keys []string) {
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
}

// parseTime mirrors the reference's lax Time.parse for the formats
// clients actually send.
func parseTime(s string) (time.Time, bool) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}

	return time.Time{}, false
}
