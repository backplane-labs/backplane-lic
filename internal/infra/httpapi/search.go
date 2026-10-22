package httpapi

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/search"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// encodable is a generated document.
type encodable interface{ Encode(e *jx.Encoder) }

// Search implements POST /v1/accounts/{account}/search: the matches of one
// resource type, answered as that type's list document.
func (a *API) Search( //nolint:cyclop,funlen // one branch per resource
	ctx context.Context, _ oas.SearchReq, _ oas.SearchParams,
) (jx.Raw, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	request, err := searchRequest(req.raw)
	if err != nil {
		return nil, err
	}

	result, err := a.search.Search(ctx, req.account, request)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	var doc encodable

	switch result.Resource {
	case search.Licenses:
		page, err := a.licenses.Index(ctx, req.account, licenses.Filter{IDs: result.IDs}, params)
		if err != nil {
			return nil, err //nolint:wrapcheck // the scenario's own error
		}

		now := a.licenseNow()
		list := &oas.LicenseListDocument{Data: make([]oas.LicenseResource, 0, len(page.Items))}

		for _, license := range page.Items { //nolint:gocritic // mirrors the reference's shape
			list.Data = append(list.Data, render.License(req.account.ID, license, now, nil))
		}

		list.Links = render.Links(linksDoc(req, params, page.Count, page.Next))
		doc = list
	case search.Users:
		page, err := a.users.List(ctx, req.account, req.principal, users.Filter{IDs: result.IDs}, params)
		if err != nil {
			return nil, err //nolint:wrapcheck // the scenario's own error
		}

		doc = listedUsers(req, page, params)
	case search.Products:
		page, err := a.products.Search(ctx, req.account, result.IDs, params)
		if err != nil {
			return nil, err //nolint:wrapcheck // the scenario's own error
		}

		list := &oas.ProductListDocument{Data: make([]oas.ProductResource, 0, len(page.Items))}
		for _, product := range page.Items { //nolint:gocritic // mirrors the reference's shape
			list.Data = append(list.Data, render.ProductResource(req.account.ID, product))
		}

		list.Links = render.Links(linksDoc(req, params, page.Count, page.Next))
		doc = list
	case search.Policies:
		page, err := a.policies.Search(ctx, req.account, result.IDs, params)
		if err != nil {
			return nil, err //nolint:wrapcheck // the scenario's own error
		}

		list := &oas.PolicyListDocument{Data: make([]oas.PolicyResource, 0, len(page.Items))}
		for _, policy := range page.Items { //nolint:gocritic // mirrors the reference's shape
			list.Data = append(list.Data, render.PolicyResource(req.account.ID, policy))
		}

		list.Links = render.Links(linksDoc(req, params, page.Count, page.Next))
		doc = list
	case search.Groups:
		page, err := a.groups.Search(ctx, req.account, req.principal, result.IDs, params)
		if err != nil {
			return nil, err //nolint:wrapcheck // the scenario's own error
		}

		list := &oas.GroupCollectionDocument{
			Data:  make([]oas.GroupResource, 0, len(page.Rows)),
			Links: render.Links(linksDoc(req, params, page.Total, uuid.NullUUID{})),
		}

		for i := range page.Rows {
			list.Data = append(list.Data, render.Group(page.Rows[i]))
		}

		doc = list
	case search.Entitlements:
		page, err := a.entitlements.Search(ctx, req.account, result.IDs, params)
		if err != nil {
			return nil, err //nolint:wrapcheck // the scenario's own error
		}

		list := &oas.EntitlementListDocument{Data: make([]oas.EntitlementResource, 0, len(page.Items))}
		for _, entitlement := range page.Items {
			list.Data = append(list.Data, render.EntitlementResource(req.account.ID, entitlement))
		}

		list.Links = render.Links(linksDoc(req, params, page.Count, page.Next))
		doc = list
	default: // machines: devices are deferred, so the account has none
		doc = &oas.MachineStubListDocument{Data: []oas.MachineStubListDocumentDataItem{}}
	}

	var e jx.Encoder
	doc.Encode(&e)

	return jx.Raw(e.Bytes()), nil
}

// The members of the search meta object.
const (
	metaType  = "type"
	metaOp    = "op"
	metaQuery = "query"
)

// searchRequest decodes the body's meta in document order, with the
// reference's typed-parameter checks: unknown members first, then kinds.
func searchRequest(raw []byte) (search.Request, error) {
	out := search.Request{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}

	body, err := search.ObjectFields(raw)
	if err != nil {
		return out, apperr.BadRequestSource("is missing", "/meta")
	}

	for _, field := range body {
		if field.Key != "meta" {
			return out, apperr.BadRequestSource("unpermitted parameter", "/"+field.Key)
		}
	}

	for _, field := range body {
		if err := searchMeta(&out, field.Value); err != nil {
			return out, err
		}
	}

	return out, nil
}

// searchMeta decodes one meta object into the request.
func searchMeta(out *search.Request, value json.RawMessage) error {
	meta, err := search.ObjectFields(value)
	if err != nil {
		return apperr.BadRequestSource(mismatch(value, "object"), "/meta")
	}

	out.HasMeta = true

	for _, member := range meta {
		if member.Key != metaType && member.Key != metaOp && member.Key != metaQuery {
			return apperr.BadRequestSource("unpermitted parameter", "/meta/"+member.Key)
		}
	}

	for _, member := range meta {
		if member.Key == metaQuery {
			query, err := search.ObjectFields(member.Value)
			if err != nil {
				return apperr.BadRequestSource(mismatch(member.Value, "object"), "/meta/query")
			}

			out.HasQuery, out.Query = true, query

			continue
		}

		var text string
		if json.Unmarshal(member.Value, &text) != nil {
			return apperr.BadRequestSource(mismatch(member.Value, "string"), "/meta/"+member.Key)
		}

		if member.Key == metaType {
			out.Type, out.HasType = text, true
		} else {
			out.Op = text
		}
	}

	return nil
}

// mismatch is typed_params' kind error detail.
func mismatch(value json.RawMessage, expected string) string {
	return "type mismatch (received " + rawKind(value) + " expected " + expected + ")"
}

// rawKind names a raw JSON value's kind as the reference's details do.
func rawKind(value json.RawMessage) string {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()

	var decoded any
	if decoder.Decode(&decoded) != nil {
		return "unknown"
	}

	return jsonKind(decoded)
}
