package httpapi

import (
	"context"

	"github.com/backplane-labs/backplane-lic/internal/app/entitlements"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// IndexEntitlements implements GET /v1/accounts/{account}/entitlements.
func (a *API) IndexEntitlements( //nolint:dupl // route handlers share the envelope
	ctx context.Context, _ oas.IndexEntitlementsParams,
) (*oas.EntitlementListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.entitlements.Index(ctx, req.account, params)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.EntitlementListDocument{Data: make([]oas.EntitlementResource, 0, len(page.Items))}
	for _, entitlement := range page.Items {
		doc.Data = append(doc.Data, render.EntitlementResource(req.account.ID, entitlement))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowEntitlement implements GET /v1/accounts/{account}/entitlements/{entitlementId}.
func (a *API) ShowEntitlement(
	ctx context.Context, params oas.ShowEntitlementParams,
) (*oas.EntitlementDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	entitlement, err := a.entitlements.Show(ctx, req.account, params.EntitlementId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.EntitlementDocument{Data: render.EntitlementResource(req.account.ID, entitlement)}, nil
}

// CreateEntitlement implements POST /v1/accounts/{account}/entitlements.
func (a *API) CreateEntitlement(
	ctx context.Context, _ oas.CreateEntitlementReq, _ oas.CreateEntitlementParams,
) (*oas.EntitlementDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateEntitlementBody(req.body, false)
	if err != nil {
		return nil, err
	}

	entitlement, err := a.entitlements.Create(ctx, req.account, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.EntitlementDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/entitlements/" + entitlement.ID.String())),
		Response: oas.EntitlementDocument{Data: render.EntitlementResource(req.account.ID, entitlement)},
	}, nil
}

// UpdateEntitlement implements PATCH /v1/accounts/{account}/entitlements/{entitlementId}.
func (a *API) UpdateEntitlement(
	ctx context.Context, _ oas.UpdateEntitlementReq, params oas.UpdateEntitlementParams,
) (*oas.EntitlementDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	if _, err := a.entitlements.Resolve(ctx, req.account, params.EntitlementId); err != nil {
		return nil, err //nolint:wrapcheck // the load's own error
	}

	in, err := validateEntitlementBody(req.body, true)
	if err != nil {
		return nil, err
	}

	entitlement, err := a.entitlements.Update(ctx, req.account, params.EntitlementId, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.EntitlementDocument{Data: render.EntitlementResource(req.account.ID, entitlement)}, nil
}

// DestroyEntitlement implements DELETE /v1/accounts/{account}/entitlements/{entitlementId}.
func (a *API) DestroyEntitlement(ctx context.Context, params oas.DestroyEntitlementParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.entitlements.Destroy(ctx, req.account, params.EntitlementId) //nolint:wrapcheck // the scenario's own error
}

// validateEntitlementBody shape-validates the create/update body like
// the reference's typed params (name and code required, non-blank,
// strings; metadata a hash).
func validateEntitlementBody(body map[string]any, update bool) (entitlements.Input, error) {
	var in entitlements.Input

	data, err := dataHash(body)
	if err != nil {
		return in, err
	}

	if err := unpermitted(data, "/data/", "type", "id", "attributes"); err != nil {
		return in, err
	}

	if err := requireType(data, "entitlement", "entitlements"); err != nil {
		return in, err
	}

	attrs, hasAttrs, err := optionalAttributes(data)
	if err != nil {
		return in, err
	}

	if !hasAttrs {
		return in, badRequest("is missing", "/data/attributes")
	}

	if err := unpermitted(attrs, "/data/attributes/", "name", "code", "metadata"); err != nil {
		return in, err
	}

	required := !update

	if in.Name, in.NameSet, err = stringAttr(attrs, "name", required); err != nil {
		return in, err
	}

	if in.Code, in.CodeSet, err = stringAttr(attrs, "code", required); err != nil {
		return in, err
	}

	if in.Metadata, in.MetadataOn, err = metadataAttr(attrs); err != nil {
		return in, err
	}

	return in, nil
}
