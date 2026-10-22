// Webhook rendering: the endpoint and event resources, and the webhook
// payload documents the publisher embeds in the outbox rows.
package render

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// WebhookEndpoint renders an endpoint exactly as the reference's
// WebhookEndpointSerializer does (the product relationship's linkage
// always present, null when account-scoped).
func WebhookEndpoint(accountID uuid.UUID, e domain.WebhookEndpoint) oas.WebhookEndpointResource {
	base := "/v1/accounts/" + accountID.String()

	product := oas.ToOneRelationship{
		Links: oas.RelationshipLinks{Related: oas.NilString{Null: true}},
		Data:  oas.NilResourceIdentifier{Null: true},
	}
	if e.ProductID.Valid {
		product.Links.Related = oas.NewNilString(base + "/products/" + e.ProductID.UUID.String())
		product.Data = oas.NilResourceIdentifier{
			Value: oas.ResourceIdentifier{Type: "products", ID: e.ProductID.UUID.String()},
		}
	}

	return oas.WebhookEndpointResource{
		Type: oas.WebhookEndpointResourceTypeWebhookEndpoints,
		ID:   e.ID.String(),
		Attributes: oas.WebhookEndpointAttributes{
			URL:                e.URL,
			Subscriptions:      e.Subscriptions,
			SignatureAlgorithm: e.SignatureAlgorithm,
			ApiVersion:         e.APIVersion,
			Created:            Timestamp(e.CreatedAt),
			Updated:            Timestamp(e.UpdatedAt),
		},
		Relationships: oas.WebhookEndpointResourceRelationships{
			Account: accountLink(accountID),
			Product: product,
		},
		Links: oas.ResourceLinks{Self: base + "/webhook-endpoints/" + e.ID.String()},
	}
}

// WebhookEvent renders an event exactly as the reference's
// WebhookEventSerializer does (the payload as the JSON string of the
// resource document; the idempotency token in resource-level meta).
func WebhookEvent(accountID uuid.UUID, e domain.WebhookEvent) oas.WebhookEventResource {
	base := "/v1/accounts/" + accountID.String()

	code := oas.NilInt{Null: true}
	if e.LastResponseCode != nil {
		code = oas.NewNilInt(*e.LastResponseCode)
	}

	body := oas.NilString{Null: true}
	if e.LastResponseBody != nil {
		body = oas.NewNilString(*e.LastResponseBody)
	}

	return oas.WebhookEventResource{
		Type: oas.WebhookEventResourceTypeWebhookEvents,
		ID:   e.ID.String(),
		Attributes: oas.WebhookEventAttributes{
			Endpoint:         e.Endpoint,
			Payload:          e.Payload,
			Event:            e.Event,
			Status:           oas.WebhookEventAttributesStatus(e.Status),
			LastResponseCode: code,
			LastResponseBody: body,
			ApiVersion:       e.APIVersion,
			Created:          Timestamp(e.CreatedAt),
			Updated:          Timestamp(e.UpdatedAt),
		},
		Relationships: oas.WebhookEventResourceRelationships{
			Account: accountLink(accountID),
		},
		Links: oas.ResourceLinks{Self: base + "/webhook-events/" + e.ID.String()},
		Meta:  oas.WebhookEventMeta{IdempotencyToken: e.IdempotencyToken},
	}
}

// PayloadDocument renders the resource document a webhook payload
// carries: the same JSON:API renderer the API responses use. The types
// that need extra inputs (the user's status, the second factor's email)
// are resolved by the caller before this; an unknown type is an error,
// never a wrong payload.
func PayloadDocument(accountID uuid.UUID, resource any) (any, error) {
	switch v := resource.(type) {
	case domain.User:
		// The caller renders users itself (the status needs a lookup).
		return nil, fmt.Errorf("render: webhook payload: user %s: %w", v.ID, errWebhookExtras)
	case domain.Product:
		return oas.ProductDocument{Data: ProductResource(accountID, v)}, nil
	case domain.Policy:
		return oas.PolicyDocument{Data: PolicyResource(accountID, v)}, nil
	case domain.Entitlement:
		return oas.EntitlementDocument{Data: EntitlementResource(accountID, v)}, nil
	case domain.Group:
		return oas.GroupDocument{Data: oas.NilGroupResource{Value: Group(v)}}, nil
	case domain.GroupOwner:
		// Relationship events broadcast the join records as a
		// collection document.
		return oas.GroupOwnerCollectionDocument{Data: []oas.GroupOwnerResource{GroupOwner(v)}}, nil
	case []domain.GroupOwner:
		owners := make([]oas.GroupOwnerResource, 0, len(v))
		for _, owner := range v {
			owners = append(owners, GroupOwner(owner))
		}

		return oas.GroupOwnerCollectionDocument{Data: owners}, nil
	case domain.PolicyEntitlement:
		return oas.PolicyEntitlementListDocument{
			Data: []oas.PolicyEntitlementResource{PolicyEntitlementResource(accountID, v)},
		}, nil
	case domain.Key:
		return oas.KeyDocument{Data: KeyResource(accountID, v)}, nil
	case domain.Token:
		// The raw secret never rides webhooks.
		return oas.TokenDocument{Data: Token(accountID, v, v.BearerRole, "")}, nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedPayload, resource)
	}
}

// ErrUnsupportedPayload marks a resource the renderer cannot turn
// into a webhook payload.
var ErrUnsupportedPayload = errors.New("render: webhook payload: unsupported resource")

// errWebhookExtras marks the types the caller must pre-resolve.
var errWebhookExtras = errors.New("needs caller-resolved inputs")
