package render

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// Token renders a token as the reference's TokenSerializer does: raw
// (the secret) only on creation/regeneration, the activation counters
// only on activation tokens. role is the bearer's live role.
func Token(accountID uuid.UUID, token domain.Token, role domain.Role, raw string) oas.TokenResource {
	base := "/v1/accounts/" + accountID.String()

	expiry := oas.NewNilString(jsonapi.Timestamp(derefTime(token.Expiry)))
	if token.Expiry == nil {
		expiry = oas.NilString{Null: true}
	}

	bearerType, bearerPath := wireBearerKind(token.BearerType)

	bearerData := oas.NilResourceIdentifier{Null: true}
	if bearerType != "" {
		bearerData = oas.NilResourceIdentifier{
			Value: oas.ResourceIdentifier{Type: bearerType, ID: token.BearerID.String()},
			Null:  false,
		}
	}

	attributes := oas.TokenAttributes{
		Kind:    crypto.TokenKind(string(role)),
		Expiry:  expiry,
		Name:    NilPtrString(token.Name),
		Created: Timestamp(token.CreatedAt),
		Updated: Timestamp(token.UpdatedAt),
	}

	// The raw secret rides the document exactly once (creation and
	// regeneration); every other response omits the attribute.
	if raw != "" {
		attributes.Token = oas.NewOptString(raw)
	}

	// The activation counters render on activation tokens only.
	if crypto.TokenKind(string(role)) == "activation-token" {
		attributes.MaxActivations = optNilInt(token.MaxActivations)
		attributes.Activations = optNilInt(&token.Activations)
		attributes.MaxDeactivations = optNilInt(token.MaxDeactivations)
		attributes.Deactivations = optNilInt(&token.Deactivations)
	}

	return oas.TokenResource{
		Type:       oas.TokenResourceTypeTokens,
		ID:         token.ID.String(),
		Attributes: attributes,
		Relationships: oas.TokenRelationships{
			Account: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base)},
				Data: oas.NilResourceIdentifier{
					Value: oas.ResourceIdentifier{Type: "accounts", ID: accountID.String()},
				},
			},
			Bearer: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{
					Related: oas.NewNilString(base + "/" + bearerPath + "/" + token.BearerID.String()),
				},
				Data: bearerData,
			},
		},
		Links: oas.ResourceLinks{Self: base + "/tokens/" + token.ID.String()},
	}
}

// optNilInt adapts an optional counter to the wire form.
func optNilInt(v *int) oas.OptNilInt {
	if v == nil {
		none := oas.OptNilInt{}
		none.SetToNull()

		return none
	}

	return oas.NewOptNilInt(*v)
}
