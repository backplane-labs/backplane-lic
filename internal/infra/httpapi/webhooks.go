package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// The type spellings the endpoint routes accept (the reference's
// typed-params inclusion list).
var webhookEndpointTypes = map[string]struct{}{ //nolint:gochecknoglobals // typed-params data
	"webhookEndpoint": {}, "webhookEndpoints": {},
	"webhook-endpoint": {}, "webhook-endpoints": {},
	"webhook_endpoint": {}, "webhook_endpoints": {},
}

// ListWebhookEndpoints implements GET /v1/accounts/{account}/webhook-endpoints.
func (a *API) ListWebhookEndpoints(
	ctx context.Context, _ oas.ListWebhookEndpointsParams,
) (*oas.WebhookEndpointListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	items, count, err := a.webhooks.Index(ctx, req.account, req.principal, params)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.WebhookEndpointListDocument{Data: make([]oas.WebhookEndpointResource, 0, len(items))}
	for i := range items {
		doc.Data = append(doc.Data, render.WebhookEndpoint(req.account.ID, items[i]))
	}

	doc.Links = render.Links(linksDoc(req, params, count, uuidNil()))

	return doc, nil
}

// CreateWebhookEndpoint implements POST /v1/accounts/{account}/webhook-endpoints.
func (a *API) CreateWebhookEndpoint(
	ctx context.Context, _ oas.CreateWebhookEndpointReq, _ oas.CreateWebhookEndpointParams,
) (*oas.WebhookEndpointDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateWebhookEndpointBody(req.body, false)
	if err != nil {
		return nil, err
	}

	endpoint, err := a.webhooks.Create(ctx, req.account, req.principal, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.WebhookEndpointDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/webhook-endpoints/" + endpoint.ID.String())),
		Response: oas.WebhookEndpointDocument{Data: render.WebhookEndpoint(req.account.ID, endpoint)},
	}, nil
}

// ShowWebhookEndpoint implements GET /v1/accounts/{account}/webhook-endpoints/{webhookEndpointId}.
func (a *API) ShowWebhookEndpoint(
	ctx context.Context, params oas.ShowWebhookEndpointParams,
) (*oas.WebhookEndpointDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	endpoint, err := a.webhooks.Show(ctx, req.account, req.principal, webhookEndpointID(params.WebhookEndpointId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.WebhookEndpointDocument{Data: render.WebhookEndpoint(req.account.ID, endpoint)}, nil
}

// UpdateWebhookEndpoint implements PATCH /v1/accounts/{account}/webhook-endpoints/{webhookEndpointId}.
func (a *API) UpdateWebhookEndpoint(
	ctx context.Context, _ oas.UpdateWebhookEndpointReq, params oas.UpdateWebhookEndpointParams,
) (*oas.WebhookEndpointDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateWebhookEndpointBody(req.body, true)
	if err != nil {
		return nil, err
	}

	endpoint, err := a.webhooks.Update(
		ctx, req.account, req.principal, webhookEndpointID(params.WebhookEndpointId), in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.WebhookEndpointDocument{Data: render.WebhookEndpoint(req.account.ID, endpoint)}, nil
}

// DestroyWebhookEndpoint implements DELETE /v1/accounts/{account}/webhook-endpoints/{webhookEndpointId}.
func (a *API) DestroyWebhookEndpoint(ctx context.Context, params oas.DestroyWebhookEndpointParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.webhooks.Destroy( //nolint:wrapcheck // the scenario's own error
		ctx, req.account, req.principal, webhookEndpointID(params.WebhookEndpointId))
}

// ListWebhookEvents implements GET /v1/accounts/{account}/webhook-events
// (the events[] filter narrows by exact event names).
func (a *API) ListWebhookEvents(
	ctx context.Context, _ oas.ListWebhookEventsParams,
) (*oas.WebhookEventListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	filter := webhooks.EventFilter{Events: req.query["events[]"]}
	filter.Events = append(filter.Events, req.query["events"]...)

	items, count, err := a.webhooks.EventsIndex(ctx, req.account, req.principal, filter, params)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.WebhookEventListDocument{Data: make([]oas.WebhookEventResource, 0, len(items))}
	for i := range items {
		doc.Data = append(doc.Data, render.WebhookEvent(req.account.ID, items[i]))
	}

	doc.Links = render.Links(linksDoc(req, params, count, uuidNil()))

	return doc, nil
}

// ShowWebhookEvent implements GET /v1/accounts/{account}/webhook-events/{webhookEventId}.
func (a *API) ShowWebhookEvent(
	ctx context.Context, params oas.ShowWebhookEventParams,
) (*oas.WebhookEventDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	event, err := a.webhooks.EventShow(ctx, req.account, req.principal, webhookEventID(params.WebhookEventId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.WebhookEventDocument{Data: render.WebhookEvent(req.account.ID, event)}, nil
}

// DestroyWebhookEvent implements DELETE /v1/accounts/{account}/webhook-events/{webhookEventId}.
func (a *API) DestroyWebhookEvent(ctx context.Context, params oas.DestroyWebhookEventParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.webhooks.EventDestroy( //nolint:wrapcheck // the scenario's own error
		ctx, req.account, req.principal, webhookEventID(params.WebhookEventId))
}

// RetryWebhookEvent implements POST /v1/accounts/{account}/webhook-events/{webhookEventId}/actions/retry.
func (a *API) RetryWebhookEvent(
	ctx context.Context, _ oas.RetryWebhookEventReq, params oas.RetryWebhookEventParams,
) (*oas.WebhookEventDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	retry, err := a.webhooks.Retry(ctx, req.account, req.principal, webhookEventID(params.WebhookEventId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.WebhookEventDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/webhook-events/" + retry.ID.String())),
		Response: oas.WebhookEventDocument{Data: render.WebhookEvent(req.account.ID, retry)},
	}, nil
}

// webhookEndpointID parses an endpoint path id (non-UUIDs answer the
// scoped find's 404).
func webhookEndpointID(raw string) uuid.UUID { return policyID(raw) }

// webhookEventID parses an event path id.
func webhookEventID(raw string) uuid.UUID { return policyID(raw) }

// supportedAPIVersions mirrors the reference's request-migrations list
// (the typed-params inclusion of api_version).
var supportedAPIVersions = []string{ //nolint:gochecknoglobals // typed-params data
	"1.0", "1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7", "1.8",
}

// validateWebhookEndpointBody shape-validates the create/update body
// like the reference's typed params (the model's 422s — URL, DNS,
// subscriptions, signature algorithm — live in the scenario).
func validateWebhookEndpointBody( //nolint:cyclop // the typed-params ladder
	body map[string]any, update bool,
) (webhooks.Input, error) {
	var in webhooks.Input

	data, err := dataHash(body)
	if err != nil {
		return in, err
	}

	if update {
		if err := unpermitted(data, "/data/", "type", "id", "attributes", "relationships"); err != nil {
			return in, err
		}
	} else if err := unpermitted(data, "/data/", "type", "attributes", "relationships"); err != nil {
		return in, err
	}

	if err := requireWebhookEndpointType(data); err != nil {
		return in, err
	}

	attrs, hasAttrs, err := optionalAttributes(data)
	if err != nil {
		return in, err
	}

	if !hasAttrs {
		if update {
			return validateWebhookRelationships(data, in)
		}

		return in, badRequest("is missing", "/data/attributes")
	}

	if err := unpermitted(attrs, "/data/attributes/",
		"url", "subscriptions", "api_version", "signature_algorithm"); err != nil {
		return in, err
	}

	url, urlSet, err := urlAttr(attrs, update)
	if err != nil {
		return in, err
	}

	if urlSet {
		in.URL = &url
	}

	if in.Subscriptions, in.SubscriptionsOn, err = stringsAttr(attrs, "subscriptions"); err != nil {
		return in, err
	}

	if in.APIVersion, _, err = nullableStringAttr(attrs, "api_version"); err != nil {
		return in, err
	}

	if in.APIVersion != nil && !contains(supportedAPIVersions, *in.APIVersion) {
		return in, badRequest("is invalid", "/data/attributes/apiVersion")
	}

	if in.SignatureAlgorithm, _, err = nullableStringAttr(attrs, "signature_algorithm"); err != nil {
		return in, err
	}

	return validateWebhookRelationships(data, in)
}

// validateWebhookRelationships checks the product relationship (the
// only one CE permits; a null data clears it on update).
func validateWebhookRelationships( //nolint:cyclop // the typed-params ladder
	data map[string]any, in webhooks.Input,
) (webhooks.Input, error) {
	raw, ok := data["relationships"]
	if !ok || raw == nil {
		return in, nil
	}

	rels, err := hash(raw, "/data/relationships")
	if err != nil {
		return in, err
	}

	if err := unpermitted(rels, "/data/relationships/", "product"); err != nil {
		return in, err
	}

	product, ok := rels["product"]
	if !ok || product == nil {
		return in, nil
	}

	rel, err := hash(product, "/data/relationships/product")
	if err != nil {
		return in, err
	}

	if err := unpermitted(rel, "/data/relationships/product/", "data"); err != nil {
		return in, err
	}

	in.ProductOn = true

	dataRaw, ok := rel["data"]
	if !ok || dataRaw == nil {
		return in, nil
	}

	link, err := hash(dataRaw, "/data/relationships/product/data")
	if err != nil {
		return in, err
	}

	if err := unpermitted(link, "/data/relationships/product/data/", "type", "id"); err != nil {
		return in, err
	}

	typeRaw, err := str(link["type"], "/data/relationships/product/data/type")
	if err != nil {
		return in, err
	}

	if typeRaw != "product" && typeRaw != "products" {
		return in, badRequest("is invalid", "/data/relationships/product/data/type")
	}

	idRaw, err := str(link["id"], "/data/relationships/product/data/id")
	if err != nil {
		return in, err
	}

	id, err := uuid.Parse(idRaw)
	if err != nil {
		return in, badRequest("is invalid", "/data/relationships/product/data/id")
	}

	in.Product = &id

	return in, nil
}

// requireWebhookEndpointType checks data.type against the six accepted
// spellings.
func requireWebhookEndpointType(data map[string]any) error {
	raw, ok := data["type"]
	if !ok {
		return badRequest("is missing", "/data/type")
	}

	value, err := str(raw, "/data/type")
	if err != nil {
		return err
	}

	if _, known := webhookEndpointTypes[value]; !known {
		return badRequest("is invalid", "/data/type")
	}

	return nil
}

// urlAttr reads the url attribute: required on create (null included),
// optional on update (null keeps the stored URL). set reports whether
// a value applies.
func urlAttr(attrs map[string]any, update bool) (string, bool, error) {
	raw, present := attrs["url"]
	if !present || raw == nil {
		if !update && !present {
			return "", false, badRequest("is missing", "/data/attributes/url")
		}

		if !update {
			return "", false, badRequest("is invalid", "/data/attributes/url")
		}

		return "", false, nil
	}

	value, err := str(raw, "/data/attributes/url")
	if err != nil {
		return "", true, err
	}

	if value == "" {
		return "", true, badRequest("cannot be blank", "/data/attributes/url")
	}

	return value, true, nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}

	return false
}
