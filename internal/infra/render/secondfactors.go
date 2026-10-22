package render

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// SecondFactor renders a second factor; the secret and provisioning
// uri only while the factor is disabled.
func SecondFactor(accountID uuid.UUID, factor domain.SecondFactor, email string) oas.SecondFactorResource {
	base := "/v1/accounts/" + accountID.String()

	var uri, secret oas.OptString

	// The secret and provisioning uri render only while the factor is
	// disabled (and never in webhooks).
	if !factor.Enabled {
		uri = oas.NewOptString(factor.URI(email))
		secret = oas.NewOptString(factor.Secret)
	}

	return oas.SecondFactorResource{
		Type: oas.SecondFactorResourceTypeSecondFactors,
		ID:   factor.ID.String(),
		Attributes: oas.SecondFactorAttributes{
			URI:     uri,
			Secret:  secret,
			Enabled: factor.Enabled,
			Created: Timestamp(factor.CreatedAt),
			Updated: Timestamp(factor.UpdatedAt),
		},
		Relationships: oas.SecondFactorResourceRelationships{
			Account: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base)},
				Data: oas.NilResourceIdentifier{
					Value: oas.ResourceIdentifier{Type: "accounts", ID: accountID.String()},
				},
			},
			User: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{
					Related: oas.NewNilString(base + "/users/" + factor.UserID.String()),
				},
				Data: oas.NilResourceIdentifier{
					Value: oas.ResourceIdentifier{Type: "users", ID: factor.UserID.String()},
				},
			},
		},
		Links: oas.ResourceLinks{
			Self: base + "/users/" + factor.UserID.String() + "/second-factors/" + factor.ID.String(),
		},
	}
}
