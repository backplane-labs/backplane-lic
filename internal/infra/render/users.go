// Package render holds the domain→ogen conversions: every resource
// document the API (and later the webhook publisher) writes on the
// wire, in exactly the reference's serializer shape.
package render

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// User renders a user as the reference's UserSerializer does (status
// precomputed by the scenario).
func User(accountID uuid.UUID, user domain.User, status string) oas.UserResource {
	base := "/v1/accounts/" + accountID.String() + "/users/" + user.ID.String()

	many := func(path string) oas.ToManyRelationship {
		return oas.ToManyRelationship{
			Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)},
		}
	}

	var fullName oas.NilString

	fullName.SetToNull()

	if name := user.FullName(); name != nil {
		fullName = oas.NewNilString(*name)
	}

	return oas.UserResource{
		Type: oas.UserResourceTypeUsers,
		ID:   user.ID.String(),
		Attributes: oas.UserAttributes{
			FullName:  fullName,
			FirstName: NilString(user.FirstName),
			LastName:  NilString(user.LastName),
			Email:     NilString(user.Email),
			Status:    status,
			Role:      user.Role.Wire(),
			Metadata:  Metadata(user.Metadata),
			Created:   Timestamp(user.CreatedAt),
			Updated:   Timestamp(user.UpdatedAt),
		},
		Relationships: oas.UserRelationships{
			Account: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString("/v1/accounts/" + accountID.String())},
				Data: oas.NilResourceIdentifier{
					Value: oas.ResourceIdentifier{
						Type: "accounts",
						ID:   accountID.String(),
					},
				},
			},
			Group: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base + "/group")},
				Data:  GroupIdentifier(user.GroupID),
			},
			Products:      many("/products"),
			Licenses:      many("/licenses"),
			Machines:      many("/machines"),
			Tokens:        many("/tokens"),
			SecondFactors: many("/second-factors"),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}

// GroupIdentifier renders a group linkage (null when none).
func GroupIdentifier(group uuid.NullUUID) oas.NilResourceIdentifier {
	if !group.Valid {
		return oas.NilResourceIdentifier{Null: true}
	}

	return oas.NilResourceIdentifier{
		Value: oas.ResourceIdentifier{Type: "groups", ID: group.UUID.String()},
	}
}

// ProductStub renders the minimal product document of the user/group
// relationship indexes (the full serializer belongs to the products
// group).
func ProductStub(accountID, id uuid.UUID) oas.UserProductResource {
	return oas.UserProductResource{
		Type:       oas.UserProductResourceTypeProducts,
		ID:         id.String(),
		Attributes: oas.NewOptNilUserProductResourceAttributes(oas.UserProductResourceAttributes{}),
		Links:      oas.ResourceLinks{Self: "/v1/accounts/" + accountID.String() + "/products/" + id.String()},
	}
}

// MachineStub renders the minimal machine document of the user/group
// relationship indexes.
func MachineStub(accountID, id uuid.UUID) oas.UserMachineResource {
	return oas.UserMachineResource{
		Type:       oas.UserMachineResourceTypeMachines,
		ID:         id.String(),
		Attributes: oas.NewOptNilUserMachineResourceAttributes(oas.UserMachineResourceAttributes{}),
		Links:      oas.ResourceLinks{Self: "/v1/accounts/" + accountID.String() + "/machines/" + id.String()},
	}
}

// Metadata renders stored metadata camelCased on the wire ({} when
// empty).
func Metadata(stored map[string]any) oas.Metadata {
	camel, _ := jsonapi.CamelizeDeep(stored).(map[string]any)
	if camel == nil {
		camel = map[string]any{}
	}

	out := oas.Metadata{}

	for k, v := range camel {
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}

		out[k] = raw
	}

	return out
}

// Timestamp renders the wire timestamp (RFC3339, millisecond
// precision, Z).
func Timestamp(t time.Time) oas.Timestamp {
	return oas.Timestamp(jsonapi.Timestamp(t))
}

// NilString renders "" as null, like the reference's serializers.
func NilString(s string) oas.NilString {
	if s == "" {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(s)
}

// NilPtrString renders an optional string.
func NilPtrString(s *string) oas.NilString {
	if s == nil {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(*s)
}

// OptNilString renders the raw token secret: present on creation,
// explicitly null otherwise.
func OptNilString(s string) oas.OptNilString {
	if s == "" {
		none := oas.OptNilString{}
		none.SetToNull()

		return none
	}

	return oas.NewOptNilString(s)
}

// Base64Std answers the strict standard base64 of s.
func Base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// withQuery merges the link's page query with the original request's
// other query parameters (page params win).

// derefTime answers the pointed time (zero when nil).
func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return *t
}

// wireBearerKind maps the stored bearer type to its wire resource type
// and path segment.
func wireBearerKind(stored string) (string, string) {
	const wireUsers = "users"

	switch stored {
	case "User":
		return wireUsers, wireUsers
	case "Product":
		return "products", "products"
	case "License":
		return "licenses", "licenses"
	default:
		return "", wireUsers
	}
}
