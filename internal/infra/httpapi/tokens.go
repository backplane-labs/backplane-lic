package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// tokenFilter builds the tokens index filter from the query
// (bearer[type], bearer[id], bearer[role][]).
func tokenFilter(req *request) tokens.Filter {
	query := queryOf(req.originalURI)

	f := tokens.Filter{BearerType: bearerTypeFilter(query.Get("bearer[type]"))}

	f.BearerID = queryUUID(query.Get("bearer[id]"))

	for _, role := range query["bearer[role][]"] {
		if parsed, ok := domain.ParseRole(role); ok {
			f.BearerRoles = append(f.BearerRoles, string(parsed))
		}
	}

	return f
}

func bearerTypeFilter(value string) string {
	switch value {
	case "user", "users":
		return storedBearerUser
	case "product", "products":
		return "Product"
	case "license", "licenses":
		return "License"
	default:
		return value
	}
}

// tokenCollection renders the page (never any secrets).
func tokenCollection(req *request, page tokens.Page) *oas.TokenCollectionDocument {
	doc := &oas.TokenCollectionDocument{Data: make([]oas.TokenResource, 0, len(page.Rows))}
	for i := range page.Rows {
		doc.Data = append(doc.Data, render.Token(req.account.ID, page.Rows[i], page.Rows[i].BearerRole, ""))
	}

	return doc
}

// ListTokens implements GET /v1/accounts/{account}/tokens.
func (a *API) ListTokens(ctx context.Context, _ oas.ListTokensParams) (*oas.TokenCollectionDocument, error) {
	req := requestOf(ctx)

	p, err := parsePaging(req)
	if err != nil {
		return nil, err
	}

	page, err := a.tokens.Manager().List(ctx, req.account, req.principal, tokenFilter(req), p)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := tokenCollection(req, page)
	doc.Links = render.PageLinks(req.originalURI, p, page.Total)

	return doc, nil
}

// ShowToken implements GET /v1/accounts/{account}/tokens/{tokenId}.
func (a *API) ShowToken(ctx context.Context, params oas.ShowTokenParams) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.TokenId)
	if err != nil {
		return nil, apperr.NotFound("token", params.TokenId)
	}

	token, err := a.tokens.Manager().Show(ctx, req.account, req.principal, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.TokenDocument{Data: render.Token(req.account.ID, token, token.BearerRole, "")}, nil
}

// RegenerateCurrentToken implements the deprecated PUT /tokens.
func (a *API) RegenerateCurrentToken(
	ctx context.Context, _ oas.RegenerateCurrentTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	token, raw, err := a.tokens.Manager().RegenerateCurrent(ctx, req.account, req.principal)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.TokenDocument{Data: render.Token(req.account.ID, token, token.BearerRole, raw)}, nil
}

// RegenerateToken implements PUT /tokens/{tokenId}.
func (a *API) RegenerateToken(
	ctx context.Context, params oas.RegenerateTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.TokenId)
	if err != nil {
		return nil, apperr.NotFound("token", params.TokenId)
	}

	token, raw, err := a.tokens.Manager().Regenerate(ctx, req.account, req.principal, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.TokenDocument{Data: render.Token(req.account.ID, token, token.BearerRole, raw)}, nil
}

// RevokeToken implements DELETE /tokens/{tokenId}.
func (a *API) RevokeToken(ctx context.Context, params oas.RevokeTokenParams) error {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.TokenId)
	if err != nil {
		return apperr.NotFound("token", params.TokenId)
	}

	return a.tokens.Manager().Revoke(ctx, req.account, req.principal, id) //nolint:wrapcheck // the scenario's own error
}

// ListUserTokens implements GET /users/{userId}/tokens.
func (a *API) ListUserTokens(
	ctx context.Context, params oas.ListUserTokensParams,
) (*oas.TokenCollectionDocument, error) {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return nil, err
	}

	page, err := a.tokens.Manager().List(ctx, req.account, req.principal,
		tokens.Filter{BearerType: storedBearerUser, BearerID: uuid.NullUUID{UUID: user.ID, Valid: true}},
		mustPaging(req))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	page.Rows = filterUserTokens(page.Rows, user.ID)

	return tokenCollection(req, page), nil
}

// ShowUserToken implements GET /users/{userId}/tokens/{tokenId}.
func (a *API) ShowUserToken(
	ctx context.Context, params oas.ShowUserTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(params.TokenId)
	if err != nil {
		return nil, apperr.NotFound("token", params.TokenId)
	}

	token, err := a.tokens.Manager().Show(ctx, req.account, req.principal, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	if token.BearerType != storedBearerUser || token.BearerID != user.ID {
		return nil, apperr.NotFound("token", params.TokenId)
	}

	return &oas.TokenDocument{Data: render.Token(req.account.ID, token, token.BearerRole, "")}, nil
}

// CreateUserToken implements POST /users/{userId}/tokens (200 + secret).
func (a *API) CreateUserToken(
	ctx context.Context, _ oas.CreateUserTokenReq, params oas.CreateUserTokenParams,
) (*oas.TokenDocument, error) {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return nil, err
	}

	attrs, err := validateUserTokenBody(req)
	if err != nil {
		return nil, err
	}

	token, raw, err := a.tokens.Manager().CreateForUser(ctx, req.account, req.principal, user, attrs)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.TokenDocument{Data: render.Token(req.account.ID, token, token.BearerRole, raw)}, nil
}

// RequestPasswordReset implements POST /passwords: always 204.
func (a *API) RequestPasswordReset(
	ctx context.Context, _ oas.RequestPasswordResetReq, _ oas.RequestPasswordResetParams,
) error {
	req := requestOf(ctx)

	email, err := validatePasswordResetBody(req)
	if err != nil {
		return err
	}

	return a.passwords.Request(ctx, req.account, email, nil) //nolint:wrapcheck // the scenario's own error
}

// validatePasswordResetBody decodes meta{email, deliver?}.
func validatePasswordResetBody(req *request) (string, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "meta"); err != nil {
		return "", err
	}

	metaRaw, ok := body["meta"]
	if !ok {
		return "", apperr.BadRequestSource("is missing", "/meta")
	}

	meta, err := hash(metaRaw, "/meta")
	if err != nil {
		return "", err
	}

	if err := unpermitted(meta, "/meta/", "email", "deliver"); err != nil {
		return "", err
	}

	email, err := metaString(meta, "email")
	if err != nil {
		return "", err
	}

	return email, nil
}

// validateUserTokenBody decodes the /users/{id}/tokens creation body:
// data optional, attributes{name, expiry} only.
func validateUserTokenBody(req *request) (tokens.CreateAttrs, error) {
	var attrs tokens.CreateAttrs

	if len(req.body) == 0 {
		return attrs, nil
	}

	body := req.body

	if err := unpermitted(body, "/", "data"); err != nil {
		return attrs, err
	}

	dataRaw, ok := body["data"]
	if !ok {
		return attrs, nil
	}

	data, err := hash(dataRaw, "/data")
	if err != nil {
		return attrs, err
	}

	if err := unpermitted(data, "/data/", "type", "attributes"); err != nil {
		return attrs, err
	}

	if typeRaw, present := data["type"]; present {
		typeStr, err := str(typeRaw, pointerOf("data", "type"))
		if err != nil {
			return attrs, err
		}

		if typeStr != "token" && typeStr != "tokens" {
			return attrs, apperr.BadRequestSource("is invalid", pointerOf("data", "type"))
		}
	}

	attrsRaw, hasAttrs := data["attributes"]
	if !hasAttrs {
		return attrs, nil
	}

	return tokenAttrs(attrsRaw)
}

// tokenAttrs decodes attributes{expiry, name} of a token creation.
func tokenAttrs(raw any) (tokens.CreateAttrs, error) {
	var attrs tokens.CreateAttrs

	obj, err := hash(raw, pointerOf("data", "attributes"))
	if err != nil {
		return attrs, err
	}

	if err := unpermitted(obj, "/data/attributes/", "expiry", "name"); err != nil {
		return attrs, err
	}

	if value, present := obj["expiry"]; present && value != nil {
		s, err := str(value, pointerOf("data", "attributes", "expiry"))
		if err != nil {
			return attrs, err
		}

		if t, parsed := parseTime(s); parsed {
			attrs.Expiry = &t
		}
	}

	if value, present := obj["name"]; present && value != nil {
		s, err := str(value, pointerOf("data", "attributes", "name"))
		if err != nil {
			return attrs, err
		}

		attrs.Name = &s
	}

	return attrs, nil
}

// filterUserTokens narrows a token page to the user's own (defensive;
// the scoped query already did).
func filterUserTokens(rows []domain.Token, userID uuid.UUID) []domain.Token {
	out := make([]domain.Token, 0, len(rows))
	for i := range rows {
		token := rows[i]
		if token.BearerType == storedBearerUser && token.BearerID == userID {
			out = append(out, token)
		}
	}

	return out
}

// mustPaging answers the pagination (parse errors surface on the
// dedicated index routes; the sub-routes fall back to the default).
func mustPaging(req *request) paging.Params {
	parsed, _ := parsePaging(req)

	return parsed
}
