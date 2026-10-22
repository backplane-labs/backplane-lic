package httpapi

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
)

// policyID parses a policy path id (UUIDs only on the wire; a non-UUID
// answers the 404 the reference's scoped find does).
func policyID(raw string) uuid.UUID {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil
	}

	return id
}

// tokenID parses a token path id.
func tokenID(raw string) uuid.UUID { return policyID(raw) }

// keyID parses a key path id.
func keyID(raw string) uuid.UUID { return policyID(raw) }

// notFound builds the record 404 for a raw (string) alias.
func notFound(resource, alias string) error {
	return apperr.NotFound(resource, alias)
}

// uuidNil is the zero next-cursor.
func uuidNil() uuid.NullUUID { return uuid.NullUUID{} }

// apperrBadRequest is the typed-params 400 alias.
func apperrBadRequest(detail, pointer string) error {
	return apperr.BadRequestSource(detail, pointer)
}
