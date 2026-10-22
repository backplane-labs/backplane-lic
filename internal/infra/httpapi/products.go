package httpapi

import (
	"context"

	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// IndexProducts implements GET /v1/accounts/{account}/products.
func (a *API) IndexProducts( //nolint:dupl // route handlers share the envelope
	ctx context.Context, _ oas.IndexProductsParams,
) (*oas.ProductListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.products.Index(ctx, req.account, params)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.ProductListDocument{Data: make([]oas.ProductResource, 0, len(page.Items))}
	for _, product := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.ProductResource(req.account.ID, product))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowProduct implements GET /v1/accounts/{account}/products/{productId}.
func (a *API) ShowProduct(ctx context.Context, params oas.ShowProductParams) (*oas.ProductDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	product, err := a.products.Show(ctx, req.account, params.ProductId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)}, nil
}

// CreateProduct implements POST /v1/accounts/{account}/products.
func (a *API) CreateProduct(
	ctx context.Context, _ oas.CreateProductReq, _ oas.CreateProductParams,
) (*oas.ProductDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateProductBody(req.body, false)
	if err != nil {
		return nil, err
	}

	product, err := a.products.Create(ctx, req.account, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.ProductDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/products/" + product.ID.String())),
		Response: oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)},
	}, nil
}

// UpdateProduct implements PATCH /v1/accounts/{account}/products/{productId}.
func (a *API) UpdateProduct(
	ctx context.Context, _ oas.UpdateProductReq, params oas.UpdateProductParams,
) (*oas.ProductDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	if _, err := a.products.Resolve(ctx, req.account, params.ProductId); err != nil {
		return nil, err //nolint:wrapcheck // the load's own error
	}

	in, err := validateProductBody(req.body, true)
	if err != nil {
		return nil, err
	}

	product, err := a.products.Update(ctx, req.account, params.ProductId, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)}, nil
}

// DestroyProduct implements DELETE /v1/accounts/{account}/products/{productId}.
func (a *API) DestroyProduct(ctx context.Context, params oas.DestroyProductParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.products.Destroy(ctx, req.account, params.ProductId) //nolint:wrapcheck // the scenario's own error
}

// IndexProductPolicies implements GET /v1/accounts/{account}/products/{productId}/policies.
func (a *API) IndexProductPolicies(
	ctx context.Context, params oas.IndexProductPoliciesParams,
) (*oas.PolicyListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	product, err := a.products.Relationship(ctx, req.account, params.ProductId, "policy.read")
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	filter := product.ID

	page, err := a.policies.Index(ctx, req.account, &filter, listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.PolicyListDocument{Data: make([]oas.PolicyResource, 0, len(page.Items))}
	for _, policy := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.PolicyResource(req.account.ID, policy))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Count, page.Next))

	return doc, nil
}

// ShowProductPolicy implements GET /v1/accounts/{account}/products/{productId}/policies/{policyId}.
func (a *API) ShowProductPolicy(
	ctx context.Context, params oas.ShowProductPolicyParams,
) (*oas.PolicyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	product, err := a.products.Relationship(ctx, req.account, params.ProductId, "policy.read")
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	policy, err := a.policies.Show(ctx, req.account, policyID(params.PolicyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	if policy.ProductID != product.ID {
		return nil, notFound("policy", params.PolicyId)
	}

	return &oas.PolicyDocument{Data: render.PolicyResource(req.account.ID, policy)}, nil
}

// IndexProductLicenses implements GET /v1/accounts/{account}/products/{productId}/licenses:
// the right shape (an empty collection with links) until the licenses
// group lands.
func (a *API) IndexProductLicenses(
	ctx context.Context, params oas.IndexProductLicensesParams,
) (*oas.LicenseStubListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	if _, err := a.products.Relationship(ctx, req.account, params.ProductId, "license.read"); err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.LicenseStubListDocument{Data: []oas.LicenseStubListDocumentDataItem{}}
	doc.Links = render.Links(linksDoc(req, listParams, 0, uuidNil()))

	return doc, nil
}

// ShowProductLicense implements GET /v1/accounts/{account}/products/{productId}/licenses/{licenseId}:
// the licenses collection is empty until the group lands, so every
// show is the 404.
func (a *API) ShowProductLicense(
	ctx context.Context, params oas.ShowProductLicenseParams,
) (oas.ShowProductLicenseOK, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	if _, err := a.products.Relationship(ctx, req.account, params.ProductId, "license.read"); err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return nil, notFound("license", params.LicenseId)
}

// IndexProductMachines implements GET /v1/accounts/{account}/products/{productId}/machines
// (empty until the machines group lands).
func (a *API) IndexProductMachines(
	ctx context.Context, params oas.IndexProductMachinesParams,
) (*oas.MachineStubListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	if _, err := a.products.Relationship(ctx, req.account, params.ProductId, "machine.read"); err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.MachineStubListDocument{Data: []oas.MachineStubListDocumentDataItem{}}
	doc.Links = render.Links(linksDoc(req, listParams, 0, uuidNil()))

	return doc, nil
}

// ShowProductMachine implements GET /v1/accounts/{account}/products/{productId}/machines/{machineId}
// (404 until the machines group lands).
func (a *API) ShowProductMachine(
	ctx context.Context, params oas.ShowProductMachineParams,
) (oas.ShowProductMachineOK, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	if _, err := a.products.Relationship(ctx, req.account, params.ProductId, "machine.read"); err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return nil, notFound("machine", params.MachineId)
}

// IndexProductTokens implements GET /v1/accounts/{account}/products/{productId}/tokens.
func (a *API) IndexProductTokens(
	ctx context.Context, params oas.IndexProductTokensParams,
) (*oas.ProductTokenListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.products.Tokens(ctx, req.account, params.ProductId, listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.ProductTokenListDocument{Data: make([]oas.TokenResource, 0, len(page.Items))}
	for _, token := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.Token(req.account.ID, token, token.BearerRole, ""))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Count, page.Next))

	return doc, nil
}

// CreateProductToken implements POST /v1/accounts/{account}/products/{productId}/tokens:
// a 200 (not 201) with the raw secret exactly once.
func (a *API) CreateProductToken(
	ctx context.Context, _ oas.CreateProductTokenReq, params oas.CreateProductTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateProductTokenBody(req.body)
	if err != nil {
		return nil, err
	}

	token, raw, err := a.products.GenerateProductToken(ctx, req.account, params.ProductId, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := render.Token(req.account.ID, token, token.BearerRole, raw)

	return &oas.TokenDocument{Data: doc}, nil
}

// ShowProductToken implements GET /v1/accounts/{account}/products/{productId}/tokens/{tokenId}.
func (a *API) ShowProductToken(
	ctx context.Context, params oas.ShowProductTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	token, err := a.products.Token(ctx, req.account, params.ProductId, tokenID(params.TokenId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := render.Token(req.account.ID, token, token.BearerRole, "")

	return &oas.TokenDocument{Data: doc}, nil
}

// IndexProductUsers implements GET /v1/accounts/{account}/products/{productId}/users.
func (a *API) IndexProductUsers(
	ctx context.Context, params oas.IndexProductUsersParams,
) (*oas.ProductUserListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	roles := roleFilter(req)

	page, err := a.products.Users(ctx, req.account, params.ProductId, roles, listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.ProductUserListDocument{Data: make([]oas.UserResource, 0, len(page.Items))}
	for _, entry := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.User(req.account.ID, entry.User, entry.Status))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Count, page.Next))

	return doc, nil
}

// ShowProductUser implements GET /v1/accounts/{account}/products/{productId}/users/{userId}.
func (a *API) ShowProductUser(
	ctx context.Context, params oas.ShowProductUserParams,
) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	roles := roleFilter(req)

	entry, err := a.products.User(ctx, req.account, params.ProductId, params.UserId, roles)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.UserDocument{Data: render.User(req.account.ID, entry.User, entry.Status)}, nil
}

// roleFilter parses the products users relationship's roles[] filter
// (default: the plain user role).
func roleFilter(req *request) []string {
	values := append(req.query["roles[]"], req.query["roles"]...) //nolint:gocritic // mirrors the reference's shape
	if len(values) == 0 {
		return []string{string(domain.RoleUser)}
	}

	out := make([]string, 0, len(values))
	for _, value := range values {
		if role, ok := domain.ParseRole(value); ok {
			out = append(out, string(role))
		}
	}

	if len(out) == 0 {
		return []string{string(domain.RoleUser)}
	}

	return out
}

// validateProductBody shape-validates the create/update body like the
// reference's typed params: create admits a relationships object whose
// members are all unpermitted, update rejects the block outright.
func validateProductBody( //nolint:cyclop // the ladder, verbatim
	body map[string]any, update bool,
) (products.Input, error) {
	var in products.Input

	data, err := dataHash(body)
	if err != nil {
		return in, err
	}

	if update {
		if err := unpermitted(data, "/data/", "type", "id", "attributes"); err != nil {
			return in, err
		}
	} else if err := unpermitted(data, "/data/", "type", "attributes", "relationships"); err != nil {
		return in, err
	}

	if err := requireType(data, "product", "products"); err != nil {
		return in, err
	}

	attrs, hasAttrs, err := optionalAttributes(data)
	if err != nil {
		return in, err
	}

	if !hasAttrs {
		return in, badRequest("is missing", "/data/attributes")
	}

	if err := unpermitted(attrs, "/data/attributes/",
		"name", "code", "distribution_strategy", "url", "platforms", "metadata"); err != nil {
		return in, err
	}

	if in.Name, in.NameOn, err = stringAttr(attrs, "name", !update); err != nil {
		return in, err
	}

	if in.Code, in.CodeOn, err = nullableStringAttr(attrs, "code"); err != nil {
		return in, err
	}

	if in.Distribution, in.DistributionOn, err = nullableStringAttr(attrs, "distribution_strategy"); err != nil {
		return in, err
	}

	if in.URL, in.URLOn, err = nullableStringAttr(attrs, "url"); err != nil {
		return in, err
	}

	if in.Platforms, in.PlatformsOn, err = stringsAttr(attrs, "platforms"); err != nil {
		return in, err
	}

	if in.Metadata, in.MetadataOn, err = metadataAttr(attrs); err != nil {
		return in, err
	}

	if !update { //nolint:nestif // the typed-params ladder
		if raw, ok := data["relationships"]; ok {
			rels, err := hash(raw, "/data/relationships")
			if err != nil {
				return in, err
			}

			if len(rels) > 0 {
				return in, badRequest("unpermitted parameter", "/data/relationships/"+firstKey(rels))
			}
		}
	}

	return in, nil
}

// validateProductTokenBody shape-validates POST /products/{id}/tokens.
func validateProductTokenBody(body map[string]any) (products.TokenInput, error) { //nolint:cyclop // one branch per rule
	var in products.TokenInput

	if body == nil {
		return in, nil
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return in, err
	}

	if raw, ok := body["data"]; !ok || raw == nil {
		return in, nil
	}

	data, err := hash(body["data"], "/data")
	if err != nil {
		return in, err
	}

	if err := unpermitted(data, "/data/", "type", "attributes"); err != nil {
		return in, err
	}

	if err := requireType(data, "token", "tokens"); err != nil {
		return in, err
	}

	attrs, hasAttrs, err := optionalAttributes(data)
	if err != nil {
		return in, err
	}

	if !hasAttrs {
		return in, nil
	}

	if err := unpermitted(attrs, "/data/attributes/", "expiry", "name"); err != nil {
		return in, err
	}

	if raw, ok := attrs["expiry"]; ok && raw != nil {
		value, err := str(raw, "/data/attributes/expiry")
		if err != nil {
			return in, err
		}

		if parsed, ok := parseTime(value); ok { //nolint:govet // mechanical
			in.Expiry = &parsed
		}
	}

	if in.Name, _, err = nullableStringAttr(attrs, "name"); err != nil {
		return in, err
	}

	return in, nil
}

// firstKey answers the level's first key (deterministic order).
func firstKey(obj map[string]any) string {
	keys := sortedKeys(obj)

	return keys[0]
}

// badRequest is the local alias for the typed-params 400.
func badRequest(detail, pointer string) error { return apperrBadRequest(detail, pointer) }
