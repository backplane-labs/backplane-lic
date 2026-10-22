package render

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// EntitlementResource renders an entitlement exactly as the
// reference's EntitlementSerializer does.
func EntitlementResource(accountID uuid.UUID, e domain.Entitlement) oas.EntitlementResource {
	return oas.EntitlementResource{
		Type: oas.EntitlementResourceTypeEntitlements,
		ID:   e.ID.String(),
		Attributes: oas.EntitlementAttributes{
			Name:     e.Name,
			Code:     e.Code,
			Metadata: metadata(e.Metadata),
			Created:  timestamp(e.CreatedAt),
			Updated:  timestamp(e.UpdatedAt),
		},
		Relationships: oas.EntitlementResourceRelationships{
			Account: accountLink(accountID),
		},
		Links: oas.ResourceLinks{
			Self: "/v1/accounts/" + accountID.String() + "/entitlements/" + e.ID.String(),
		},
	}
}

// PolicyEntitlementResource renders one policy_entitlements join row
// exactly as the attach route's PolicyEntitlementSerializer does: the
// links object carries related (the policy-side relationship URL), not
// self.
func PolicyEntitlementResource(accountID uuid.UUID, join domain.PolicyEntitlement) oas.PolicyEntitlementResource {
	account := "/v1/accounts/" + accountID.String()
	policy := account + "/policies/" + join.PolicyID.String()

	return oas.PolicyEntitlementResource{
		Type: oas.PolicyEntitlementResourceTypePolicyEntitlements,
		ID:   join.ID.String(),
		Attributes: oas.PolicyEntitlementResourceAttributes{
			Created: timestamp(join.CreatedAt),
			Updated: timestamp(join.UpdatedAt),
		},
		Relationships: oas.PolicyEntitlementResourceRelationships{
			Account: accountLink(accountID),
			Entitlement: toOne(account, "/entitlements/"+join.EntitlementID.String(),
				"entitlements", join.EntitlementID),
			Policy: toOne(account, "/policies/"+join.PolicyID.String(), "policies", join.PolicyID),
		},
		Links: oas.PolicyEntitlementLinks{
			Related: policy + "/entitlements/" + join.EntitlementID.String(),
		},
	}
}
