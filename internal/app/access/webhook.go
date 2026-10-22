package access

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// --- webhook endpoints (WebhookEndpointPolicy) ---

// WebhookEndpointShow mirrors WebhookEndpointPolicy#show?: the admin
// family (admin, developer, read_only, sales and support), or the
// product bearer for its own endpoint.
func WebhookEndpointShow(p Principal, endpoint domain.WebhookEndpoint) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && endpoint.ProductID.UUID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// WebhookEndpointWrite mirrors WebhookEndpointPolicy#create?/update?/
// destroy?: the write family (admin, developer), or the product bearer
// for its own endpoint (create infers the bearer's product).
func WebhookEndpointWrite(p Principal, endpoint domain.WebhookEndpoint) error {
	switch {
	case isWriteFamily(p):
		return nil
	case p.Role == domain.RoleProduct && endpoint.ProductID.UUID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// WebhookEndpointIndex mirrors WebhookEndpointPolicy#index?: the admin
// family; the product bearer passes only a page of its own endpoints
// (the scoped query already narrowed it — the post-check reasserts).
func WebhookEndpointIndex(p Principal, page []domain.WebhookEndpoint) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct:
		for _, endpoint := range page { //nolint:gocritic // mirrors the reference's shape
			if endpoint.ProductID.UUID != p.BearerID {
				return deny()
			}
		}

		return nil
	default:
		return deny()
	}
}

// --- webhook events (WebhookEventPolicy) ---

// WebhookEventIndex mirrors WebhookEventPolicy#index?: the admin family,
// or the product bearer for events of its own endpoints.
func WebhookEventIndex(p Principal, page []domain.WebhookEvent, productOf func(uuid.UUID) uuid.UUID) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct:
		for _, event := range page { //nolint:gocritic // mirrors the reference's shape
			if !event.EndpointID.Valid || productOf(event.EndpointID.UUID) != p.BearerID {
				return deny()
			}
		}

		return nil
	default:
		return deny()
	}
}

// WebhookEventShow mirrors WebhookEventPolicy#show?.
func WebhookEventShow(p Principal, _ domain.WebhookEvent, productID uuid.UUID, hasEndpoint bool) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && hasEndpoint && productID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// WebhookEventManage mirrors WebhookEventPolicy#destroy?/retry?: the
// write family only (products included are denied).
func WebhookEventManage(p Principal) error {
	if isWriteFamily(p) {
		return nil
	}

	return deny()
}

// ProductMayRead answers whether the product-scoped endpoint's product
// is authorized to read the event's resource (BroadcastWebhookService's
// ActionPolicy check, per resource type): products see their own
// product, policy, key and product-token events, the events of plain
// users (UserPolicy#show? allows any product for user-role records),
// and the account-global entitlement and group events; user-scoped
// second factors are never sent to product endpoints. The cases that
// need a lookup (user tokens of licensed users, policy entitlement
// joins) are resolved by the publisher before this check.
func ProductMayRead(productID uuid.UUID, resource any) bool {
	switch v := resource.(type) {
	case domain.Product:
		return v.ID == productID
	case domain.Policy:
		return v.ProductID == productID
	case domain.Key:
		return v.ProductID == productID
	case domain.Token:
		return v.BearerType == "Product" && v.BearerID == productID
	case domain.User:
		return v.Role == domain.RoleUser
	case domain.License:
		return v.ProductID == productID
	case domain.LicenseUser, domain.LicenseEntitlement,
		[]domain.LicenseUser, []domain.LicenseEntitlement,
		domain.Entitlement, domain.Group, domain.GroupOwner, []domain.GroupOwner:
		return true
	default:
		return false
	}
}
