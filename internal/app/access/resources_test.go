package access_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestResourceUserRoleMatrix(t *testing.T) {
	t.Parallel()

	product := domain.Product{ID: uuid.New(), DistributionStrategy: domain.DistributionLicensed}
	policy := domain.Policy{ID: uuid.New(), ProductID: product.ID}
	key := domain.Key{ID: uuid.New(), ProductID: product.ID}
	group := domain.Group{ID: uuid.New()}
	endpoint := domain.WebhookEndpoint{ID: uuid.New()}
	entitlementID := uuid.New()
	owned := map[uuid.UUID]struct{}{entitlementID: {}}

	for _, role := range []domain.Role{
		domain.RoleAdmin, domain.RoleDeveloper, domain.RoleReadOnly,
		domain.RoleSalesAgent, domain.RoleSupportRole, domain.RoleUser,
	} {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			principal := access.Principal{BearerType: access.BearerUser, BearerID: uuid.New(), Role: role}
			family := role != domain.RoleUser
			writer := role == domain.RoleAdmin || role == domain.RoleDeveloper
			policyWriter := writer || role == domain.RoleSalesAgent
			webhookReader := writer || role == domain.RoleReadOnly

			checks := []struct {
				name string
				err  error
				want bool
			}{
				{"product owned read", access.ProductShow(principal, product, true), true},
				{"product unowned read", access.ProductShow(principal, product, false), family},
				{"product write", access.ProductWrite(principal, product), writer},
				{"product relationships", access.ProductRelationship(principal, product), family},
				{"product token create", access.ProductTokenCreate(principal), writer},
				{"policy owned read", access.PolicyShow(principal, policy, true, false), true},
				{"policy unowned read", access.PolicyShow(principal, policy, false, false), family},
				{"policy update", access.PolicyWrite(principal, policy, false), policyWriter},
				{"policy delete", access.PolicyWrite(principal, policy, true), writer},
				{"policy relationships", access.PolicyRelationship(principal, policy), family},
				{"policy entitlement write", access.PolicyEntitlementsWrite(principal, policy), policyWriter},
				{"policy pool pop", access.PolicyPoolPop(principal, policy), writer},
				{"entitlement owned read", access.EntitlementShow(principal, entitlementID, owned), true},
				{"entitlement unowned read", access.EntitlementShow(principal, uuid.New(), owned), family},
				{"entitlement write", access.EntitlementWrite(principal), writer},
				{"key read", access.KeyShow(principal, key), family},
				{"key write", access.KeyWrite(principal, key), writer},
				{"key relationships", access.KeyRelationship(principal, key), family},
				{"group member read", access.GroupShow(principal, group, func(domain.Group) bool { return true }), true},
				{"group nonmember read", access.GroupShow(principal, group, func(domain.Group) bool { return false }), family},
				{"group write", access.GroupWrite(principal), writer},
				{"group owner write", access.GroupOwnersWrite(principal), writer},
				{"endpoint read", access.WebhookEndpointShow(principal, endpoint), webhookReader},
				{"endpoint index", access.WebhookEndpointIndex(principal, []domain.WebhookEndpoint{endpoint}), webhookReader},
				{"endpoint write", access.WebhookEndpointWrite(principal, endpoint), writer},
				{"event read", access.WebhookEventShow(principal, domain.WebhookEvent{}, uuid.Nil, false), webhookReader},
				{"event index", access.WebhookEventIndex(principal, []domain.WebhookEvent{}, func(uuid.UUID) uuid.UUID { return uuid.Nil }), webhookReader},
				{"event manage", access.WebhookEventManage(principal), writer},
			}
			for _, check := range checks {
				t.Run(check.name, func(t *testing.T) {
					if got := check.err == nil; got != check.want {
						t.Errorf("allowed = %v, want %v (error %v)", got, check.want, check.err)
					}
				})
			}
		})
	}
}

// Live CE probes (2026-10-22), with direct admin-generated role tokens and
// explicit webhook read permissions: developer/read_only returned 200 for an
// endpoint and an empty events[] window; sales_agent/support_agent returned
// 403 with ReasonAllResources. Permission presence must not bypass role gates.
func TestWebhookReadRolesDoNotExpandWithPermissions(t *testing.T) {
	t.Parallel()

	for _, role := range []domain.Role{domain.RoleSalesAgent, domain.RoleSupportRole} {
		principal := access.Principal{BearerType: access.BearerUser, Role: role, Permissions: access.Effective(role, []string{"*"})}

		ctx := access.With(t.Context(), principal)
		if err := access.Require(ctx, "webhook-endpoint.read"); err != nil {
			t.Fatal(err)
		}

		if err := access.WebhookEndpointShow(principal, domain.WebhookEndpoint{}); err == nil {
			t.Errorf("%s may not read webhook endpoints even with permission", role)
		}
	}
}

// Live CE probe (2026-10-22): a user owning a license for a CLOSED product,
// explicitly granted product.read by a direct admin method, received 403
// with ReasonAllResources for both member and collection product reads.
func TestClosedProductIsNotReadableThroughOwnership(t *testing.T) {
	t.Parallel()

	principal := access.Principal{BearerType: access.BearerUser, Role: domain.RoleUser}

	closed := domain.Product{DistributionStrategy: domain.DistributionClosed}
	if err := access.ProductShow(principal, closed, true); err == nil {
		t.Fatal("owning a CLOSED product's license must not authorize reading it")
	}

	if err := access.ProductIndex(principal, false); err == nil {
		t.Fatal("a page containing an owned CLOSED product must be denied")
	}
}
