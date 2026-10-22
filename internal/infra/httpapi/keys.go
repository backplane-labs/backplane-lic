package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/keys"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// IndexKeys implements GET /v1/accounts/{account}/keys.
func (a *API) IndexKeys(ctx context.Context, _ oas.IndexKeysParams) (*oas.KeyListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	filterPolicy := uuidFilter(req, "policy")
	filterProduct := uuidFilter(req, "product")

	page, err := a.keys.Index(ctx, req.account, filterPolicy, filterProduct, params)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.KeyListDocument{Data: make([]oas.KeyResource, 0, len(page.Items))}
	for _, key := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.KeyResource(req.account.ID, key))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowKey implements GET /v1/accounts/{account}/keys/{keyId}.
func (a *API) ShowKey(ctx context.Context, params oas.ShowKeyParams) (*oas.KeyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	key, err := a.keys.Show(ctx, req.account, keyID(params.KeyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.KeyDocument{Data: render.KeyResource(req.account.ID, key)}, nil
}

// CreateKey implements POST /v1/accounts/{account}/keys.
func (a *API) CreateKey(
	ctx context.Context, _ oas.CreateKeyReq, _ oas.CreateKeyParams,
) (*oas.KeyDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateKeyBody(req.body, false)
	if err != nil {
		return nil, err
	}

	key, err := a.keys.Create(ctx, req.account, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.KeyDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/keys/" + key.ID.String())),
		Response: oas.KeyDocument{Data: render.KeyResource(req.account.ID, key)},
	}, nil
}

// UpdateKey implements PATCH /v1/accounts/{account}/keys/{keyId}.
func (a *API) UpdateKey(
	ctx context.Context, _ oas.UpdateKeyReq, params oas.UpdateKeyParams,
) (*oas.KeyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	if _, err := a.keys.Resolve(ctx, req.account, keyID(params.KeyId)); err != nil {
		return nil, err //nolint:wrapcheck // the load's own error
	}

	in, err := validateKeyBody(req.body, true)
	if err != nil {
		return nil, err
	}

	key, err := a.keys.Update(ctx, req.account, keyID(params.KeyId), in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.KeyDocument{Data: render.KeyResource(req.account.ID, key)}, nil
}

// DestroyKey implements DELETE /v1/accounts/{account}/keys/{keyId}.
func (a *API) DestroyKey(ctx context.Context, params oas.DestroyKeyParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.keys.Destroy(ctx, req.account, keyID(params.KeyId)) //nolint:wrapcheck // the scenario's own error
}

// ShowKeyProduct implements GET /v1/accounts/{account}/keys/{keyId}/product.
func (a *API) ShowKeyProduct(ctx context.Context, params oas.ShowKeyProductParams) (*oas.ProductDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	key, product, err := a.keys.Product(ctx, req.account, keyID(params.KeyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	_ = key

	return &oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)}, nil
}

// ShowKeyPolicy implements GET /v1/accounts/{account}/keys/{keyId}/policy.
func (a *API) ShowKeyPolicy(ctx context.Context, params oas.ShowKeyPolicyParams) (*oas.PolicyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	key, policy, err := a.keys.Policy(ctx, req.account, keyID(params.KeyId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	_ = key

	return &oas.PolicyDocument{Data: render.PolicyResource(req.account.ID, policy)}, nil
}

// uuidFilter parses a UUID query filter (an invalid UUID answers the
// empty page, like the reference's scopes).
func uuidFilter(req *request, name string) *uuid.UUID {
	raw := req.query.Get(name)
	if raw == "" {
		return nil
	}

	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}

	return &id
}

// validateKeyBody shape-validates the create/update body: the create
// requires the key attribute and the pooled-policy relationship; the
// update admits the key attribute only.
func validateKeyBody( //nolint:gocognit,cyclop,funlen // the ladder, verbatim
	body map[string]any, update bool,
) (keys.Input, error) {
	var in keys.Input

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

	if err := requireType(data, "key", "keys"); err != nil {
		return in, err
	}

	attrs, hasAttrs, err := optionalAttributes(data)
	if err != nil {
		return in, err
	}

	if hasAttrs { //nolint:nestif // the typed-params ladder
		if err := unpermitted(attrs, "/data/attributes/", "key"); err != nil {
			return in, err
		}

		if value, set, err := stringAttr(attrs, "key", !update); err != nil {
			return in, err
		} else { //nolint:revive // mirrors the reference's shape
			in.KeySet = set
			if value != nil {
				in.Key = *value
			}
		}
	}

	if update {
		if raw, ok := data["relationships"]; ok {
			if _, err := hash(raw, "/data/relationships"); err != nil {
				return in, err
			}

			return in, badRequest("unpermitted parameter", "/data/relationships")
		}

		return in, nil
	}

	raw, ok := data["relationships"]
	if !ok {
		return in, badRequest("is missing", "/data/relationships")
	}

	rels, err := hash(raw, "/data/relationships")
	if err != nil {
		return in, err
	}

	if err := unpermitted(rels, "/data/relationships/", "policy"); err != nil {
		return in, err
	}

	policyRel, err := hash(rels["policy"], "/data/relationships/policy")
	if err != nil {
		return in, err
	}

	if err := unpermitted(policyRel, "/data/relationships/policy/", "data"); err != nil {
		return in, err
	}

	policyData, err := hash(policyRel["data"], "/data/relationships/policy/data")
	if err != nil {
		return in, err
	}

	if err := unpermitted(policyData, "/data/relationships/policy/data/", "type", "id"); err != nil {
		return in, err
	}

	if err := requireType(policyData, "policy", "policies"); err != nil {
		return in, err
	}

	idRaw, ok := policyData["id"]
	if !ok {
		return in, badRequest("is missing", "/data/relationships/policy/data/id")
	}

	idStr, err := str(idRaw, "/data/relationships/policy/data/id")
	if err != nil {
		return in, err
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return in, badRequest("type mismatch (received string expected UUID string)",
			"/data/relationships/policy/data/id")
	}

	in.PolicyID = id

	return in, nil
}
