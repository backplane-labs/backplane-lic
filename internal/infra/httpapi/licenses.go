// The license route handlers: thin extract→scenario→render shells.
package httpapi

import (
	"context"
	"crypto/rand"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// base10 is the digit multiplier of the query ttl parse.
const base10 = 10

// licenseNow answers the request's clock.
func (a *API) licenseNow() time.Time {
	if a.clock == nil {
		return time.Now()
	}

	return a.clock()
}

// licenseDocument renders one license (or null).
func (a *API) licenseDocument(req *request, license *domain.License, rawKey *string) oas.NilLicenseResource {
	if license == nil {
		return oas.NilLicenseResource{Null: true}
	}

	resource := render.License(req.account.ID, *license, a.licenseNow(), rawKey)

	return oas.NilLicenseResource{Value: resource}
}

// licenseLocation answers the create Location header value.
func licenseLocation(req *request, license domain.License) oas.OptString {
	return oas.NewOptString(req.location("/v1/accounts/" +
		req.account.ID.String() + "/licenses/" + license.ID.String()))
}

// IndexLicenses implements GET /v1/accounts/{account}/licenses.
func (a *API) IndexLicenses(ctx context.Context, _ oas.IndexLicensesParams) (*oas.LicenseListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.licenses.Index(ctx, req.account, licenseFilter(req.query), params)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	now := a.licenseNow()

	doc := &oas.LicenseListDocument{Data: make([]oas.LicenseResource, 0, len(page.Items))}
	for _, license := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.License(req.account.ID, license, now, nil))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowLicense implements GET /v1/accounts/{account}/licenses/{licenseId}.
func (a *API) ShowLicense(ctx context.Context, params oas.ShowLicenseParams) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	license, err := a.licenses.Show(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// CreateLicense implements POST /v1/accounts/{account}/licenses.
func (a *API) CreateLicense(
	ctx context.Context, _ oas.CreateLicenseReq, _ oas.CreateLicenseParams,
) (*oas.LicenseDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateLicenseCreateBody(req)
	if err != nil {
		return nil, err
	}

	license, err := a.licenses.Create(ctx, req.account, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocumentHeaders{
		Location: licenseLocation(req, license),
		Response: oas.LicenseDocument{Data: a.licenseDocument(req, &license, license.RawKey)},
	}, nil
}

// UpdateLicense implements PATCH /v1/accounts/{account}/licenses/{licenseId}.
func (a *API) UpdateLicense(
	ctx context.Context, _ oas.UpdateLicenseReq, params oas.UpdateLicenseParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateLicenseUpdateBody(req)
	if err != nil {
		return nil, err
	}

	license, err := a.licenses.Update(ctx, req.account, params.LicenseId, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// DestroyLicense implements DELETE /v1/accounts/{account}/licenses/{licenseId}.
func (a *API) DestroyLicense(ctx context.Context, params oas.DestroyLicenseParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	if _, err := a.licenses.Destroy(ctx, req.account, params.LicenseId); err != nil {
		return err //nolint:wrapcheck // the scenario's own error
	}

	return nil
}

// --- relationships ---

// IndexLicenseMachines implements GET …/licenses/{licenseId}/machines.
func (a *API) IndexLicenseMachines(
	ctx context.Context, params oas.IndexLicenseMachinesParams,
) (*oas.MachineListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.licenses.Machines(ctx, req.account, params.LicenseId, machineFilter(req), listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.MachineListDocument{Data: make([]oas.MachineResource, 0, len(page.Items))}
	for _, machine := range page.Items { //nolint:gocritic // mirrors the reference's shape
		doc.Data = append(doc.Data, render.Machine(req.account.ID, machine, uuid.Nil, nil))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Count, page.Next))

	return doc, nil
}

// ShowLicenseMachine implements GET …/licenses/{licenseId}/machines/{machineId}.
func (a *API) ShowLicenseMachine(
	ctx context.Context, params oas.ShowLicenseMachineParams,
) (*oas.MachineDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	machine, err := a.licenses.Machine(ctx, req.account, params.LicenseId, params.MachineId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.MachineDocument{Data: oas.NilMachineResource{
		Value: render.Machine(req.account.ID, machine, uuid.Nil, nil),
	}}, nil
}

// IndexLicenseTokens implements GET …/licenses/{licenseId}/tokens.
func (a *API) IndexLicenseTokens(
	ctx context.Context, params oas.IndexLicenseTokensParams,
) (*oas.TokenCollectionDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.licenses.Tokens(ctx, req.account, params.LicenseId, listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.TokenCollectionDocument{Data: make([]oas.TokenResource, 0, len(page.Rows))}
	for i := range page.Rows {
		doc.Data = append(doc.Data, render.Token(req.account.ID, page.Rows[i], page.Rows[i].BearerRole, ""))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Total, uuidNil()))

	return doc, nil
}

// ShowLicenseToken implements GET …/licenses/{licenseId}/tokens/{tokenId}.
func (a *API) ShowLicenseToken(
	ctx context.Context, params oas.ShowLicenseTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	token, err := a.licenses.Token(ctx, req.account, params.LicenseId, tokenID(params.TokenId))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.TokenDocument{Data: render.Token(req.account.ID, token, token.BearerRole, "")}, nil
}

// CreateLicenseToken implements POST …/licenses/{licenseId}/tokens.
func (a *API) CreateLicenseToken(
	ctx context.Context, _ oas.CreateLicenseTokenReq, params oas.CreateLicenseTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	attrs, err := validateTokenBody(req, false)
	if err != nil {
		return nil, err
	}

	token, raw, err := a.licenses.GenerateToken(ctx, req.account, params.LicenseId, attrs)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.TokenDocument{Data: render.Token(req.account.ID, token, token.BearerRole, raw)}, nil
}

// validateTokenBody decodes the optional token creation body (shared
// with the product token route's shape).
func validateTokenBody( //nolint:cyclop,funlen // the typed-params ladder
	req *request, _ bool,
) (tokens.CreateAttrs, error) {
	var attrs tokens.CreateAttrs

	body := req.body
	if body == nil {
		return attrs, nil
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return attrs, err
	}

	dataRaw, ok := body["data"]
	if !ok || dataRaw == nil {
		return attrs, nil
	}

	data, err := hash(dataRaw, "/data")
	if err != nil {
		return attrs, err
	}

	if err := unpermitted(data, "/data/", "type", "attributes"); err != nil {
		return attrs, err
	}

	if err := requireType(data, "token", "tokens"); err != nil {
		return attrs, err
	}

	attrsRaw, hasAttrs := data["attributes"]
	if !hasAttrs || attrsRaw == nil {
		return attrs, nil
	}

	attrsObj, err := hash(attrsRaw, "/data/attributes")
	if err != nil {
		return attrs, err
	}

	if err := unpermitted(attrsObj, "/data/attributes/",
		"expiry", "name", "max_activations", "max_deactivations"); err != nil {
		return attrs, err
	}

	if expiryRaw, hasExpiry := attrsObj["expiry"]; hasExpiry && expiryRaw != nil {
		value, err := str(expiryRaw, "/data/attributes/expiry")
		if err != nil {
			return attrs, err
		}

		if t, parsed := parseTime(value); parsed {
			attrs.Expiry = &t
		}
	}

	if nameRaw, hasName := attrsObj["name"]; hasName && nameRaw != nil {
		value, err := str(nameRaw, "/data/attributes/name")
		if err != nil {
			return attrs, err
		}

		attrs.Name = &value
	}

	intAttrOf := func(name string) (*int, error) {
		raw, has := attrsObj[name]
		if !has || raw == nil {
			return nil, nil //nolint:nilnil // an absent attribute simply does not set
		}

		value, err := int64Of(raw, "/data/attributes/"+name)
		if err != nil {
			return nil, err
		}

		out := int(value)

		return &out, nil
	}

	maxActivations, err := intAttrOf("max_activations")
	if err != nil {
		return attrs, err
	}

	attrs.MaxActivations = maxActivations
	attrs.MaxDeactivations, err = intAttrOf("max_deactivations")

	return attrs, err
}

// ShowLicenseProduct implements GET …/licenses/{licenseId}/product.
func (a *API) ShowLicenseProduct(
	ctx context.Context, params oas.ShowLicenseProductParams,
) (*oas.ProductDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	product, err := a.licenses.Product(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)}, nil
}

// ShowLicensePolicy implements GET …/licenses/{licenseId}/policy.
func (a *API) ShowLicensePolicy(
	ctx context.Context, params oas.ShowLicensePolicyParams,
) (*oas.PolicyDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	policy, err := a.licenses.Policy(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.PolicyDocument{Data: render.PolicyResource(req.account.ID, policy)}, nil
}

// UpdateLicensePolicy implements PATCH …/licenses/{licenseId}/policy.
func (a *API) UpdateLicensePolicy(
	ctx context.Context, _ oas.UpdateLicensePolicyReq, params oas.UpdateLicensePolicyParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	link, _, err := validateRelationshipPatchBody(req, "policy")
	if err != nil {
		return nil, err
	}

	if !link.Valid {
		return nil, apperr.BadRequestSource("is missing", "/data")
	}

	license, err := a.licenses.Transfer(ctx, req.account, params.LicenseId, link.UUID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// ShowLicenseGroup implements GET …/licenses/{licenseId}/group.
func (a *API) ShowLicenseGroup(
	ctx context.Context, params oas.ShowLicenseGroupParams,
) (*oas.GroupDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	group, err := a.licenses.Group(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.GroupDocument{Data: oas.NilGroupResource{Value: render.Group(group)}}, nil
}

// UpdateLicenseGroup implements PATCH …/licenses/{licenseId}/group.
func (a *API) UpdateLicenseGroup(
	ctx context.Context, _ oas.UpdateLicenseGroupReq, params oas.UpdateLicenseGroupParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	link, isNull, err := validateRelationshipPatchBody(req, "group")
	if err != nil {
		return nil, err
	}

	license, err := a.licenses.SetGroup(ctx, req.account, params.LicenseId, groupLinkOf(link, isNull))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

func groupLinkOf(link uuid.NullUUID, isNull bool) uuid.NullUUID {
	if isNull {
		return uuid.NullUUID{}
	}

	return link
}

// ShowLicenseOwner implements GET …/licenses/{licenseId}/owner.
func (a *API) ShowLicenseOwner(
	ctx context.Context, params oas.ShowLicenseOwnerParams,
) (*oas.LicenseOwnerDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	owner, present, err := a.licenses.Owner(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	if !present {
		return &oas.LicenseOwnerDocument{Data: oas.NilUserResource{Null: true}}, nil
	}

	active, err := a.licenses.UserActive(ctx, req.account, owner.ID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseOwnerDocument{Data: oas.NilUserResource{
		Value: render.User(req.account.ID, owner, active),
	}}, nil
}

// UpdateLicenseOwner implements PATCH …/licenses/{licenseId}/owner.
func (a *API) UpdateLicenseOwner(
	ctx context.Context, _ oas.UpdateLicenseOwnerReq, params oas.UpdateLicenseOwnerParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	return a.updateLicenseOwner(ctx, req, params.LicenseId, "owner")
}

// UpdateLicenseUserAlias implements PATCH …/licenses/{licenseId}/user
// (the v1.5 alias).
func (a *API) UpdateLicenseUserAlias(
	ctx context.Context, _ oas.UpdateLicenseUserAliasReq, params oas.UpdateLicenseUserAliasParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	return a.updateLicenseOwner(ctx, req, params.LicenseId, "user")
}

func (a *API) updateLicenseOwner(
	ctx context.Context, req *request, idOrKey, kind string,
) (*oas.LicenseDocument, error) {
	link, isNull, err := validateRelationshipPatchBody(req, kind)
	if err != nil {
		return nil, err
	}

	owner := link
	if isNull {
		owner = uuid.NullUUID{}
	}

	license, err := a.licenses.SetOwner(ctx, req.account, idOrKey, owner, kind == typeUserSingular)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// ShowLicenseUserAlias implements GET …/licenses/{licenseId}/user (the
// v1.5 alias of owner).
func (a *API) ShowLicenseUserAlias(
	ctx context.Context, params oas.ShowLicenseUserAliasParams,
) (*oas.LicenseOwnerDocument, error) {
	return a.ShowLicenseOwner(ctx, oas.ShowLicenseOwnerParams(params))
}

// IndexLicenseUsers implements GET …/licenses/{licenseId}/users.
func (a *API) IndexLicenseUsers(
	ctx context.Context, params oas.IndexLicenseUsersParams,
) (*oas.UserCollectionDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	page, err := a.licenses.Users(ctx, req.account, params.LicenseId, listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.UserCollectionDocument{Data: make([]oas.UserResource, 0, len(page.Items))}
	for i := range page.Items {
		doc.Data = append(doc.Data, render.User(req.account.ID, page.Items[i].User, page.Items[i].Status))
	}

	doc.Links = render.Links(linksDoc(req, listParams, page.Count, uuidNil()))

	return doc, nil
}

// ShowLicenseUser implements GET …/licenses/{licenseId}/users/{userId}.
func (a *API) ShowLicenseUser(
	ctx context.Context, params oas.ShowLicenseUserParams,
) (*oas.LicenseOwnerDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listed, err := a.licenses.User(ctx, req.account, params.LicenseId, params.UserId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseOwnerDocument{Data: oas.NilUserResource{
		Value: render.User(req.account.ID, listed.User, listed.Status),
	}}, nil
}

// AttachLicenseUsers implements POST …/licenses/{licenseId}/users.
func (a *API) AttachLicenseUsers(
	ctx context.Context, _ oas.AttachLicenseUsersReq, params oas.AttachLicenseUsersParams,
) (*oas.LicenseUserListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	ids, err := validateAttachBody(req, typeUserSingular, typeUserPlural)
	if err != nil {
		return nil, err
	}

	rows, err := a.licenses.AttachUsers(ctx, req.account, params.LicenseId, ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.LicenseUserListDocument{Data: make([]oas.LicenseUserResource, 0, len(rows))}
	for i := range rows {
		doc.Data = append(doc.Data, render.LicenseUser(req.account.ID, rows[i]))
	}

	return doc, nil
}

// DetachLicenseUsers implements DELETE …/licenses/{licenseId}/users.
func (a *API) DetachLicenseUsers(
	ctx context.Context, _ oas.DetachLicenseUsersReq, params oas.DetachLicenseUsersParams,
) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	ids, err := validateAttachBody(req, typeUserSingular, typeUserPlural)
	if err != nil {
		return err
	}

	return a.licenses.DetachUsers(ctx, req.account, params.LicenseId, ids) //nolint:wrapcheck // the scenario's own error
}

// IndexLicenseEntitlements implements GET …/licenses/{licenseId}/entitlements.
func (a *API) IndexLicenseEntitlements(
	ctx context.Context, params oas.IndexLicenseEntitlementsParams,
) (*oas.EntitlementListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	items, count, err := a.licenses.Entitlements(ctx, req.account, params.LicenseId, listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.EntitlementListDocument{Data: make([]oas.EntitlementResource, 0, len(items))}
	for i := range items {
		doc.Data = append(doc.Data, render.EntitlementResource(req.account.ID, items[i]))
	}

	doc.Links = render.Links(linksDoc(req, listParams, count, uuidNil()))

	return doc, nil
}

// ShowLicenseEntitlement implements GET …/licenses/{licenseId}/entitlements/{entitlementId}.
func (a *API) ShowLicenseEntitlement(
	ctx context.Context, params oas.ShowLicenseEntitlementParams,
) (*oas.EntitlementDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	id := policyID(params.EntitlementId)

	entitlement, err := a.licenses.Entitlement(ctx, req.account, params.LicenseId, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.EntitlementDocument{Data: render.EntitlementResource(req.account.ID, entitlement)}, nil
}

// AttachLicenseEntitlements implements POST …/licenses/{licenseId}/entitlements.
func (a *API) AttachLicenseEntitlements(
	ctx context.Context, _ oas.AttachLicenseEntitlementsReq, params oas.AttachLicenseEntitlementsParams,
) (*oas.LicenseEntitlementListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	ids, err := validateAttachBody(req, typeEntitlementSing, typeEntitlementPlur)
	if err != nil {
		return nil, err
	}

	rows, err := a.licenses.AttachEntitlements(ctx, req.account, params.LicenseId, ids)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.LicenseEntitlementListDocument{Data: make([]oas.LicenseEntitlementResource, 0, len(rows))}
	for i := range rows {
		doc.Data = append(doc.Data, render.LicenseEntitlement(req.account.ID, rows[i]))
	}

	return doc, nil
}

// DetachLicenseEntitlements implements DELETE …/licenses/{licenseId}/entitlements.
func (a *API) DetachLicenseEntitlements(
	ctx context.Context, _ oas.DetachLicenseEntitlementsReq, params oas.DetachLicenseEntitlementsParams,
) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	ids, err := validateAttachBody(req, typeEntitlementSing, typeEntitlementPlur)
	if err != nil {
		return err
	}

	return a.licenses.DetachEntitlements( //nolint:wrapcheck // the scenario's own error
		ctx, req.account, params.LicenseId, ids,
	)
}

// --- actions ---

// QuickValidateLicense implements GET …/licenses/{licenseId}/actions/validate.
func (a *API) QuickValidateLicense(
	ctx context.Context, params oas.QuickValidateLicenseParams,
) (*oas.LicenseValidateDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	skipTouch := req.headers.Get("Origin") == "https://app.keygen.sh"

	license, verdict, err := a.licenses.ValidateQuick(ctx, req.account, params.LicenseId, skipTouch)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseValidateDocument{
		Data: a.licenseDocument(req, &license, nil),
		Meta: validationMeta(a.licenseNow(), verdict, nil, nil),
	}, nil
}

// ValidateLicense implements POST …/licenses/{licenseId}/actions/validate.
func (a *API) ValidateLicense(
	ctx context.Context, _ oas.ValidateLicenseReq, params oas.ValidateLicenseParams,
) (*oas.LicenseValidateDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	nonce, scope, rawScope, _, _, err := validateValidationBody(req, false)
	if err != nil {
		return nil, err
	}

	license, verdict, err := a.licenses.Validate(ctx, req.account, params.LicenseId, scope)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseValidateDocument{
		Data: a.licenseDocument(req, &license, nil),
		Meta: validationMeta(a.licenseNow(), verdict, nonce, rawScope),
	}, nil
}

// ValidateLicenseKey implements POST /v1/accounts/{account}/licenses/actions/validate-key.
func (a *API) ValidateLicenseKey(
	ctx context.Context, _ oas.ValidateLicenseKeyReq, _ oas.ValidateLicenseKeyParams,
) (*oas.LicenseValidateDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	nonce, scope, rawScope, key, legacy, err := validateValidationBody(req, true)
	if err != nil {
		return nil, err
	}

	license, verdict, err := a.licenses.ValidateKey(ctx, req.account, key, legacy, scope)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseValidateDocument{
		Data: a.licenseDocument(req, license, nil),
		Meta: validationMeta(a.licenseNow(), verdict, nonce, rawScope),
	}, nil
}

func validationMeta(
	now time.Time, verdict licenses.Verdict, nonce *int64, rawScope map[string]any,
) oas.LicenseValidateMeta {
	return render.LicenseValidationMeta(domain.LicenseValidationEvent{
		Time: now, Valid: verdict.Valid, Detail: verdict.Detail, Code: verdict.Code,
		Nonce: nonce, Scope: rawScope,
	})
}

// RevokeLicense implements DELETE …/licenses/{licenseId}/actions/revoke.
func (a *API) RevokeLicense(ctx context.Context, params oas.RevokeLicenseParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.licenses.Revoke(ctx, req.account, params.LicenseId) //nolint:wrapcheck // the scenario's own error
}

// RenewLicense implements POST …/licenses/{licenseId}/actions/renew.
func (a *API) RenewLicense(ctx context.Context, params oas.RenewLicenseParams) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	license, err := a.licenses.Renew(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// SuspendLicense implements POST …/licenses/{licenseId}/actions/suspend.
func (a *API) SuspendLicense(ctx context.Context, params oas.SuspendLicenseParams) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	license, err := a.licenses.Suspend(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// ReinstateLicense implements POST …/licenses/{licenseId}/actions/reinstate.
func (a *API) ReinstateLicense(ctx context.Context, params oas.ReinstateLicenseParams) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	license, err := a.licenses.Reinstate(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// CheckInLicense implements POST …/licenses/{licenseId}/actions/check-in.
func (a *API) CheckInLicense(ctx context.Context, params oas.CheckInLicenseParams) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	license, err := a.licenses.CheckIn(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// IncrementLicenseUsage implements POST …/licenses/{licenseId}/actions/increment-usage.
func (a *API) IncrementLicenseUsage(
	ctx context.Context, _ oas.IncrementLicenseUsageReq, params oas.IncrementLicenseUsageParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	increment, err := validateUsageBody(req, "increment")
	if err != nil {
		return nil, err
	}

	license, err := a.licenses.IncrementUsage(ctx, req.account, params.LicenseId, increment)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// DecrementLicenseUsage implements POST …/licenses/{licenseId}/actions/decrement-usage.
func (a *API) DecrementLicenseUsage(
	ctx context.Context, _ oas.DecrementLicenseUsageReq, params oas.DecrementLicenseUsageParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	decrement, err := validateUsageBody(req, "decrement")
	if err != nil {
		return nil, err
	}

	license, err := a.licenses.DecrementUsage(ctx, req.account, params.LicenseId, decrement)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// ResetLicenseUsage implements POST …/licenses/{licenseId}/actions/reset-usage.
func (a *API) ResetLicenseUsage(ctx context.Context, params oas.ResetLicenseUsageParams) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	license, err := a.licenses.ResetUsage(ctx, req.account, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// --- check-out ---

// CheckoutLicenseFile implements GET …/licenses/{licenseId}/actions/check-out
// (the binary certificate).
func (a *API) CheckoutLicenseFile(
	ctx context.Context, params oas.CheckoutLicenseFileParams,
) (*oas.CheckoutLicenseFileOKHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	checkout, cert, err := a.checkout(ctx, req, params.LicenseId, checkoutQueryParams(req))
	if err != nil {
		return nil, err
	}

	return &oas.CheckoutLicenseFileOKHeaders{
		ContentDisposition: oas.NewOptString(
			"attachment; filename=\"" + checkout.License.ID.String() + ".lic\""),
		Response: oas.CheckoutLicenseFileOK{Data: strings.NewReader(cert)},
	}, nil
}

// CheckoutLicense implements POST …/licenses/{licenseId}/actions/check-out.
func (a *API) CheckoutLicense(
	ctx context.Context, _ oas.CheckoutLicenseReq, params oas.CheckoutLicenseParams,
) (*oas.LicenseFileDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	meta, err := validateCheckoutMeta(req)
	if err != nil {
		return nil, err
	}

	// The meta wins over the query parameters.
	query := checkoutQueryParams(req)
	mergeCheckoutParams(&meta, &query)

	checkout, cert, err := a.checkout(ctx, req, params.LicenseId, meta)
	if err != nil {
		return nil, err
	}

	return &oas.LicenseFileDocument{Data: render.LicenseFile(
		req.account.ID, uuid.New(), cert, checkout.Algorithm, checkout.Includes,
		checkout.TTL, checkout.IssuedAt, checkout.Expiry, checkout.License.ID,
	)}, nil
}

// checkout runs the scenario and builds the certificate.
func (a *API) checkout(
	ctx context.Context, req *request, idOrKey string, params licenses.CheckoutParams,
) (licenses.Checkout, string, error) {
	checkout, err := a.licenses.Checkout(ctx, req.account, idOrKey, params)
	if err != nil {
		return checkout, "", err //nolint:wrapcheck // the scenario's own error
	}

	payload, err := render.CheckoutPayload(req.account.ID, checkout, a.licenseNow())
	if err != nil {
		return checkout, "", err //nolint:wrapcheck // the renderer's own error
	}

	cert, err := crypto.BuildFileCertificate(crypto.FileCertificateInput{
		Keys: req.account.Keys, Payload: payload, Secret: checkout.License.Key,
		Algorithm: checkout.Algorithm, RNG: rand.Reader,
		Prefix: "license/", Begin: "-----BEGIN LICENSE FILE-----", End: "-----END LICENSE FILE-----",
	})
	if err != nil {
		return checkout, "", err //nolint:wrapcheck // the crypto's own error
	}

	return checkout, cert, nil
}

// checkoutQueryParams reads the checkout query parameters.
func checkoutQueryParams(req *request) licenses.CheckoutParams {
	var params licenses.CheckoutParams

	query := req.query

	if raw := query.Get("encrypt"); raw != "" {
		value := raw == "true" || raw == "1" || raw == "t"
		params.Encrypt = &value
	}

	params.Algorithm = query.Get("algorithm")

	if raw := query.Get("ttl"); raw != "" {
		var ttl int32

		params.TTLSet = true

		for _, c := range raw {
			if c < '0' || c > '9' {
				ttl = -1

				break
			}

			ttl = ttl*base10 + (c - '0')
		}

		if ttl >= 0 {
			params.TTL = &ttl
		}
	}

	params.Include = append(params.Include, query["include[]"]...)
	params.Include = append(params.Include, query["include"]...)

	return params
}

// mergeCheckoutParams overlays the meta over the query parameters
// (present meta values win).
func mergeCheckoutParams(dst, src *licenses.CheckoutParams) {
	if src.Encrypt != nil {
		dst.Encrypt = src.Encrypt
	}

	if src.Algorithm != "" {
		dst.Algorithm = src.Algorithm
	}

	if src.TTL != nil {
		dst.TTL = src.TTL
	}

	if len(src.Include) > 0 {
		dst.Include = src.Include
	}
}

// licenseListDocument renders an index page (the shared envelope of
// the license routes and the nested parent routes).
func (a *API) licenseListDocument(
	req *request, page licenses.List, params paging.Params,
) *oas.LicenseListDocument {
	doc := &oas.LicenseListDocument{Data: make([]oas.LicenseResource, 0, len(page.Items))}

	now := a.licenseNow()

	for i := range page.Items {
		doc.Data = append(doc.Data, render.License(req.account.ID, page.Items[i], now, nil))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc
}
