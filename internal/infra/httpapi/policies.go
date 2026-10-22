package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// IndexPolicies implements GET /v1/accounts/{account}/policies.
func (a *API) IndexPolicies(ctx context.Context, _ oas.IndexPoliciesParams) (*oas.PolicyListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	filter := productFilter(req)

	page, err := a.policies.Index(ctx, req.account, filter, params)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.PolicyListDocument{Data: make([]oas.PolicyResource, 0, len(page.Items))}
	for _, policy := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.PolicyResource(req.account.ID, policy))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowPolicy implements GET /v1/accounts/{account}/policies/{policyId}.
func (a *API) ShowPolicy(ctx context.Context, params oas.ShowPolicyParams) (*oas.PolicyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	policy, err := a.policies.Show(ctx, req.account, policyID(params.PolicyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.PolicyDocument{Data: render.PolicyResource(req.account.ID, policy)}, nil
}

// CreatePolicy implements POST /v1/accounts/{account}/policies.
func (a *API) CreatePolicy(
	ctx context.Context, _ oas.CreatePolicyReq, _ oas.CreatePolicyParams,
) (*oas.PolicyDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validatePolicyBody(req.body, false)
	if err != nil {
		return nil, err
	}

	policy, err := a.policies.Create(ctx, req.account, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.PolicyDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/policies/" + policy.ID.String())),
		Response: oas.PolicyDocument{Data: render.PolicyResource(req.account.ID, policy)},
	}, nil
}

// UpdatePolicy implements PATCH /v1/accounts/{account}/policies/{policyId}.
func (a *API) UpdatePolicy(
	ctx context.Context, _ oas.UpdatePolicyReq, params oas.UpdatePolicyParams,
) (*oas.PolicyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	if _, err := a.policies.Resolve(ctx, req.account, policyID(params.PolicyId)); err != nil {
		return nil, err //nolint:wrapcheck // the load's own error
	}

	in, err := validatePolicyBody(req.body, true)
	if err != nil {
		return nil, err
	}

	policy, err := a.policies.Update(ctx, req.account, policyID(params.PolicyId), in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.PolicyDocument{Data: render.PolicyResource(req.account.ID, policy)}, nil
}

// DestroyPolicy implements DELETE /v1/accounts/{account}/policies/{policyId}.
func (a *API) DestroyPolicy(ctx context.Context, params oas.DestroyPolicyParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.policies.Destroy(ctx, req.account, policyID(params.PolicyId)) //nolint:wrapcheck // the scenario's own error
}

// ShowPolicyProduct implements GET /v1/accounts/{account}/policies/{policyId}/product.
func (a *API) ShowPolicyProduct(
	ctx context.Context, params oas.ShowPolicyProductParams,
) (*oas.ProductDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	product, err := a.policies.Product(ctx, req.account, policyID(params.PolicyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)}, nil
}

// IndexPolicyLicenses implements GET /v1/accounts/{account}/policies/{policyId}/licenses.
func (a *API) IndexPolicyLicenses(
	ctx context.Context, params oas.IndexPolicyLicensesParams,
) (*oas.LicenseListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	policy, err := a.policyRelationship(ctx, req, params.PolicyId, "license.read")
	if err != nil {
		return nil, err
	}

	page, err := a.licenses.OfPolicy(ctx, req.account, policy, licenseFilter(req.query), listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return a.licenseListDocument(req, page, listParams), nil
}

// ShowPolicyLicense implements GET /v1/accounts/{account}/policies/{policyId}/licenses/{licenseId}.
func (a *API) ShowPolicyLicense(
	ctx context.Context, params oas.ShowPolicyLicenseParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	policy, err := a.policyRelationship(ctx, req, params.PolicyId, "license.read")
	if err != nil {
		return nil, err
	}

	license, err := a.licenses.OfPolicyShow(ctx, req.account, policy, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// IndexPolicyPool implements GET /v1/accounts/{account}/policies/{policyId}/pool.
func (a *API) IndexPolicyPool( //nolint:dupl // route handlers share the envelope
	ctx context.Context, params oas.IndexPolicyPoolParams,
) (*oas.KeyListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.policies.Pool(ctx, req.account, policyID(params.PolicyId), listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.KeyListDocument{Data: make([]oas.KeyResource, 0, len(page.Items))}
	for _, key := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.KeyResource(req.account.ID, key))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Count, page.Next))

	return doc, nil
}

// ShowPolicyPoolKey implements GET /v1/accounts/{account}/policies/{policyId}/pool/{keyId}.
func (a *API) ShowPolicyPoolKey(
	ctx context.Context, params oas.ShowPolicyPoolKeyParams,
) (*oas.KeyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	key, err := a.policies.PoolKey(ctx, req.account, policyID(params.PolicyId), keyID(params.KeyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.KeyDocument{Data: render.KeyResource(req.account.ID, key)}, nil
}

// PopPolicyPool implements DELETE /v1/accounts/{account}/policies/{policyId}/pool.
func (a *API) PopPolicyPool(ctx context.Context, params oas.PopPolicyPoolParams) (*oas.KeyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	key, err := a.policies.Pop(ctx, req.account, policyID(params.PolicyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.KeyDocument{Data: render.KeyResource(req.account.ID, key)}, nil
}

// IndexPolicyEntitlements implements GET /v1/accounts/{account}/policies/{policyId}/entitlements.
func (a *API) IndexPolicyEntitlements( //nolint:dupl // route handlers share the envelope
	ctx context.Context, params oas.IndexPolicyEntitlementsParams,
) (*oas.EntitlementListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.policies.Entitlements(ctx, req.account, policyID(params.PolicyId), listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.EntitlementListDocument{Data: make([]oas.EntitlementResource, 0, len(page.Items))}
	for _, entitlement := range page.Items {
		doc.Data = append(doc.Data, render.EntitlementResource(req.account.ID, entitlement))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Count, page.Next))

	return doc, nil
}

// ShowPolicyEntitlement implements GET /v1/accounts/{account}/policies/{policyId}/entitlements/{entitlementId}.
func (a *API) ShowPolicyEntitlement(
	ctx context.Context, params oas.ShowPolicyEntitlementParams,
) (*oas.EntitlementDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	entitlement, err := a.policies.Entitlement(ctx, req.account, policyID(params.PolicyId), params.EntitlementId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.EntitlementDocument{Data: render.EntitlementResource(req.account.ID, entitlement)}, nil
}

// AttachPolicyEntitlements implements POST /v1/accounts/{account}/policies/{policyId}/entitlements.
func (a *API) AttachPolicyEntitlements(
	ctx context.Context, _ oas.AttachPolicyEntitlementsReq, params oas.AttachPolicyEntitlementsParams,
) (*oas.PolicyEntitlementListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	ids, err := validateEntitlementIDsBody(req.body)
	if err != nil {
		return nil, err
	}

	joins, err := a.policies.Attach(ctx, req.account, policyID(params.PolicyId), ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.PolicyEntitlementListDocument{Data: make([]oas.PolicyEntitlementResource, 0, len(joins))}
	for _, join := range joins {
		doc.Data = append(doc.Data, render.PolicyEntitlementResource(req.account.ID, join))
	}

	return doc, nil
}

// DetachPolicyEntitlements implements DELETE /v1/accounts/{account}/policies/{policyId}/entitlements.
func (a *API) DetachPolicyEntitlements(
	ctx context.Context, _ oas.DetachPolicyEntitlementsReq, params oas.DetachPolicyEntitlementsParams,
) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	ids, err := validateEntitlementIDsBody(req.body)
	if err != nil {
		return err
	}

	return a.policies.Detach(ctx, req.account, policyID(params.PolicyId), ids) //nolint:wrapcheck,lll // the scenario's own error
}

// policyRelationship loads the policy and runs the relationship guard
// (the policies/*_policy.rb checks) for its sub-resources.
func (a *API) policyRelationship(
	ctx context.Context, req *request, rawPolicyID, permission string,
) (domain.Policy, error) {
	return a.policies.RequireRelationship(ctx, req.account, policyID(rawPolicyID), permission) //nolint:wrapcheck,lll // the scenario's own error
}

// productFilter parses the policies index's ?product= filter (an
// invalid UUID answers the empty page, like the reference's scope).
func productFilter(req *request) *uuid.UUID {
	raw := req.query.Get("product")
	if raw == "" {
		return nil
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		// An unparseable filter value matches nothing (the reference's
		// coerced scope), never everything.
		return &uuid.Nil
	}

	return &id
}
