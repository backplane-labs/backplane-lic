package render

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// KeyResource renders a key exactly as the reference's KeySerializer
// does: the product linkage rides through the key's policy.
func KeyResource(accountID uuid.UUID, k domain.Key) oas.KeyResource {
	base := "/v1/accounts/" + accountID.String() + "/keys/" + k.ID.String()

	return oas.KeyResource{
		Type: oas.KeyResourceTypeKeys,
		ID:   k.ID.String(),
		Attributes: oas.KeyAttributes{
			Key:     k.Key,
			Created: timestamp(k.CreatedAt),
			Updated: timestamp(k.UpdatedAt),
		},
		Relationships: oas.KeyRelationships{
			Account: accountLink(accountID),
			Product: toOne(base, "/product", "products", k.ProductID),
			Policy:  toOne(base, "/policy", "policies", k.PolicyID),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}
