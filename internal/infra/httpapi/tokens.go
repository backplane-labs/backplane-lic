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
	doc.Links = render.Links(linksDoc(req, p, page.Total, uuid.NullUUID{}))

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

// CreateUserToken implements POST /users/{userId}/tokens: retired
// with end-user authentication — user-bearer tokens do not exist (the
// 501).
func (a *API) CreateUserToken(
	context.Context, oas.CreateUserTokenReq, oas.CreateUserTokenParams,
) (*oas.TokenDocument, error) {
	return nil, apperr.NotImplemented()
}

// RequestPasswordReset implements POST /passwords: retired with
// end-user authentication (the 501).
func (a *API) RequestPasswordReset(
	context.Context, oas.RequestPasswordResetReq, oas.RequestPasswordResetParams,
) error {
	return apperr.NotImplemented()
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
