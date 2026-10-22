// Package httpapi is the HTTP face of the service: the ogen handler
// (thin: extract params, call one scenario, translate the outcome),
// JSON:API rendering, the error mapping and the full middleware
// pipeline (request id, path normalisation, content negotiation,
// authentication, response signing).
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/entitlements"
	"github.com/backplane-labs/backplane-lic/internal/app/keys"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// Accounts is the consumer-side port of the account scenario.
type Accounts interface {
	// Account loads the account by id.
	Account(ctx context.Context, id uuid.UUID) (domain.Account, error)
}

// Spec identifies the configured account: both its id and its slug
// address it on the wire.
type Spec struct {
	ID   uuid.UUID
	Slug string
}

// Deps is everything the HTTP face is built from.
type Deps struct {
	Accounts     Accounts
	Authn        authn.Scenario
	Users        users.Scenario
	Tokens       tokens.Scenario
	Products     products.Scenario
	Policies     policies.Scenario
	Entitlements entitlements.Scenario
	Keys         keys.Scenario
	Spec         Spec
	// Revision feeds the Keygen-Revision header (the build's commit).
	Revision string
	// DocsHost overrides the docs origin of error links; when empty it
	// is derived from the request host (its last two labels).
	DocsHost string
	// Throttle counts password attempts; nil disables the limiter.
	Throttle Throttler
	// TrustedProxies lists the proxy CIDRs whose X-Forwarded-For is
	// honored when resolving the client IP (empty: RemoteAddr always).
	TrustedProxies []string
	Clock          func() time.Time
}

// API implements the ogen handler over the app's scenarios. The
// unimplemented base answers the routes whose groups have not landed.
type API struct {
	oas.UnimplementedHandler

	accounts     Accounts
	users        users.Scenario
	tokens       tokens.Scenario
	products     products.Scenario
	policies     policies.Scenario
	entitlements entitlements.Scenario
	keys         keys.Scenario
	spec         Spec
}

// New builds the service's HTTP handler: the ogen server wrapped in the
// architecture contract's middleware pipeline. It is the seam between
// routing and the platform's HTTP mounting.
func New(deps Deps) http.Handler {
	api := &API{
		accounts:     deps.Accounts,
		users:        deps.Users,
		tokens:       deps.Tokens,
		products:     deps.Products,
		policies:     deps.Policies,
		entitlements: deps.Entitlements,
		keys:         deps.Keys,
		spec:         deps.Spec,
	}

	server, err := oas.NewServer(api)
	if err != nil {
		// A static spec cannot fail to build a server.
		panic("httpapi: ogen server: " + err.Error())
	}

	clock := deps.Clock
	if clock == nil {
		clock = time.Now
	}

	return &pipeline{
		server:         server,
		accounts:       deps.Accounts,
		authn:          deps.Authn,
		spec:           deps.Spec,
		revision:       deps.Revision,
		docsHost:       deps.DocsHost,
		throttle:       deps.Throttle,
		trustedProxies: parseCIDRs(deps.TrustedProxies),
		clock:          clock,
	}
}

// Ping implements GET /v1/ping: a one-byte body the pipeline renders as
// text/plain.
func (a *API) Ping(context.Context) error {
	return nil
}

// Health implements GET /v1/health: a bare 204.
func (a *API) Health(context.Context) error {
	return nil
}

// GetAccount implements GET /v1/accounts/{account}: the configured
// account with its public keys in the data-level meta. The pipeline has
// already resolved and authenticated the account.
func (a *API) GetAccount(ctx context.Context, _ oas.GetAccountParams) (*oas.AccountDocument, error) {
	return accountDocument(requestOf(ctx).account), nil
}

// GenerateToken implements POST /v1/accounts/{account}/tokens: Basic
// email:password (with meta.otp for two-factor users) or any token
// authentication. The raw secret rides the response exactly once.
func (a *API) GenerateToken(
	ctx context.Context,
	_ oas.GenerateTokenReq,
	_ oas.GenerateTokenParams,
) (*oas.TokenDocumentHeaders, error) {
	req := requestOf(ctx)

	in, err := validateGenerateBody(req.body)
	if err != nil {
		return nil, err
	}

	token, raw, err := a.tokens.Generate(ctx, req.account, req.principal, in)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	// The response's Keygen-Token header carries the NEW token even for
	// the password flow (the reference's controller sets it explicitly).
	req.token = &token

	// The snapshot the scenario wrote is the live role of the token's
	// bearer (fetched from the user row, never the stored snapshot).
	role := token.BearerRole

	return &oas.TokenDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/tokens/" + token.ID.String())),
		Response: oas.TokenDocument{Data: tokenResource(req.account.ID, token, role, raw)},
	}, nil
}

// Me implements GET /v1/accounts/{account}/me: the authenticated user
// with, for token authentication, the current token in included.
func (a *API) Me(ctx context.Context, _ oas.MeParams) (*oas.MeDocument, error) {
	req := requestOf(ctx)

	if !req.principal.IsUser() {
		return nil, errNotUserBearer
	}

	user, status, err := a.users.Me(ctx, req.account.ID, req.principal.BearerID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.MeDocument{Data: userResource(req.account.ID, user, status)}
	if req.token != nil {
		doc.Included = []oas.TokenResource{
			tokenResource(req.account.ID, *req.token, req.principal.Role, ""),
		}
	}

	return doc, nil
}

// Profile implements GET /v1/accounts/{account}/profile: the
// authenticated user with the current token's id in meta.
func (a *API) Profile(ctx context.Context, _ oas.ProfileParams) (*oas.ProfileDocument, error) {
	req := requestOf(ctx)

	if !req.principal.IsUser() {
		return nil, errNotUserBearer
	}

	user, status, err := a.users.Me(ctx, req.account.ID, req.principal.BearerID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	tokenID := oas.NilString{Null: true}
	if req.token != nil {
		tokenID = oas.NewNilString(req.token.ID.String())
	}

	return &oas.ProfileDocument{
		Data: userResource(req.account.ID, user, status),
		Meta: oas.ProfileMeta{TokenId: tokenID},
	}, nil
}

// NewError maps handler errors to Keygen error documents: the typed app
// error (or the 500 fallback) with the request id as meta.id.
func (a *API) NewError(ctx context.Context, err error) *oas.ErrorStatusCode {
	req := requestOf(ctx)

	return errorStatus(apperrFrom(err), req)
}

// accountDocument renders the account exactly as the reference's
// AccountSerializer does: camelCase attributes, every has-many
// relationship as a related link, the public keys base64-encoded in the
// data-level meta.
func accountDocument(account domain.Account) *oas.AccountDocument {
	base := "/v1/accounts/" + account.ID.String()

	related := func(path string) oas.ToManyRelationship {
		return oas.ToManyRelationship{
			Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)},
		}
	}

	return &oas.AccountDocument{
		Data: oas.AccountResource{
			Type: oas.AccountResourceTypeAccounts,
			ID:   account.ID.String(),
			Attributes: oas.AccountAttributes{
				Name:       nilString(account.Name),
				Slug:       nilString(account.Slug),
				ApiVersion: nilString(account.APIVersion),
				Status:     oas.NewNilString(domain.AccountStatusSubscribed),
				Protected:  oas.NewNilBool(account.Protected),
				Created:    timestamp(account.CreatedAt),
				Updated:    timestamp(account.UpdatedAt),
			},
			Relationships: oas.AccountRelationships{
				Settings:         related("/settings"),
				WebhookEndpoints: related("/webhook-endpoints"),
				WebhookEvents:    related("/webhook-events"),
				Products:         related("/products"),
				Platforms:        related("/platforms"),
				Arches:           related("/arches"),
				Channels:         related("/channels"),
				Releases:         related("/releases"),
				Artifacts:        related("/artifacts"),
				Policies:         related("/policies"),
				Users:            related("/users"),
				Keys:             related("/keys"),
				Licenses:         related("/licenses"),
				Machines:         related("/machines"),
				Components:       related("/components"),
				Processes:        related("/processes"),
				Tokens:           related("/tokens"),
			},
			Links: oas.ResourceLinks{Self: base},
			Meta: oas.AccountMeta{
				// Base64 of the stored string in every case: the RSA and
				// ECDSA PEMs, and — for ed25519 — the hex string itself.
				PublicKey: base64Std(account.Keys.RSAPublic),
				Keys: oas.AccountKeysMeta{
					Ed25519: base64Std(account.Keys.Ed25519Public),
					Rsa2048: base64Std(account.Keys.RSAPublic),
					Ecdsa:   base64Std(account.Keys.ECDSAPublic),
				},
			},
		},
	}
}

// userResource renders a user as the reference's UserSerializer does.
func userResource(accountID uuid.UUID, user domain.User, status string) oas.UserResource {
	base := "/v1/accounts/" + accountID.String() + "/users/" + user.ID.String()

	many := func(path string) oas.ToManyRelationship {
		return oas.ToManyRelationship{
			Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)},
		}
	}

	var fullName oas.NilString

	fullName.SetToNull()

	if name := user.FullName(); name != nil {
		fullName = oas.NewNilString(*name)
	}

	return oas.UserResource{
		Type: oas.UserResourceTypeUsers,
		ID:   user.ID.String(),
		Attributes: oas.UserAttributes{
			FullName:  fullName,
			FirstName: nilString(user.FirstName),
			LastName:  nilString(user.LastName),
			Email:     nilString(user.Email),
			Status:    status,
			Role:      user.Role.Wire(),
			Metadata:  metadata(user.Metadata),
			Created:   timestamp(user.CreatedAt),
			Updated:   timestamp(user.UpdatedAt),
		},
		Relationships: oas.UserRelationships{
			Account: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString("/v1/accounts/" + accountID.String())},
				Data: oas.NilResourceIdentifier{
					Value: oas.ResourceIdentifier{
						Type: "accounts",
						ID:   accountID.String(),
					},
				},
			},
			Group: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base + "/group")},
				Data:  oas.NilResourceIdentifier{Null: true},
			},
			Products:      many("/products"),
			Licenses:      many("/licenses"),
			Machines:      many("/machines"),
			Tokens:        many("/tokens"),
			SecondFactors: many("/second-factors"),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}

// tokenResource renders a token; raw (the secret) only on
// creation/regeneration, and kind derived from the bearer's live role.
func tokenResource(accountID uuid.UUID, token domain.Token, role domain.Role, raw string) oas.TokenResource {
	base := "/v1/accounts/" + accountID.String()

	expiry := oas.NewNilString(jsonapi.Timestamp(derefTime(token.Expiry)))
	if token.Expiry == nil {
		expiry = oas.NilString{Null: true}
	}

	bearerType, bearerPath := wireBearer(token.BearerType)

	bearerData := oas.NilResourceIdentifier{Null: true}
	if bearerType != "" {
		bearerData = oas.NilResourceIdentifier{
			Value: oas.ResourceIdentifier{Type: bearerType, ID: token.BearerID.String()},
			Null:  false,
		}
	}

	return oas.TokenResource{
		Type: oas.TokenResourceTypeTokens,
		ID:   token.ID.String(),
		Attributes: oas.TokenAttributes{
			Kind:    crypto.TokenKind(string(role)),
			Token:   optNilString(raw),
			Expiry:  expiry,
			Name:    nilPtrString(token.Name),
			Created: timestamp(token.CreatedAt),
			Updated: timestamp(token.UpdatedAt),
		},
		Relationships: oas.TokenRelationships{
			Account: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base)},
				Data: oas.NilResourceIdentifier{
					Value: oas.ResourceIdentifier{Type: "accounts", ID: accountID.String()},
				},
			},
			Bearer: oas.ToOneRelationship{
				Links: oas.RelationshipLinks{
					Related: oas.NewNilString(base + "/" + bearerPath + "/" + token.BearerID.String()),
				},
				Data: bearerData,
			},
		},
		Links: oas.ResourceLinks{Self: base + "/tokens/" + token.ID.String()},
	}
}

// wireBearer maps the stored bearer type to its wire form (the
// resource type and the path segment).
func wireBearer(stored string) (string, string) {
	switch stored {
	case "User":
		return access.BearerUser, access.BearerUser
	case "Product":
		return access.BearerProduct, access.BearerProduct
	case "License":
		return access.BearerLicense, access.BearerLicense
	default:
		return "", access.BearerUser
	}
}

// metadata renders the stored metadata camelCased on the wire ({} when
// empty).
func metadata(stored map[string]any) oas.Metadata {
	camel, _ := jsonapi.CamelizeDeep(stored).(map[string]any)
	if camel == nil {
		camel = map[string]any{}
	}

	out := oas.Metadata{}

	for k, v := range camel {
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}

		out[k] = raw
	}

	return out
}

// errNotUserBearer: /me and /profile serve user bearers (the reference
// serves license documents for license bearers — a later phase).
var errNotUserBearer = apperr.NotFoundDefault()

func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func timestamp(t time.Time) oas.Timestamp {
	return oas.Timestamp(jsonapi.Timestamp(t))
}

func nilString(s string) oas.NilString {
	if s == "" {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(s)
}

// optNilString renders the raw token secret: present on creation,
// explicitly null otherwise.
func optNilString(s string) oas.OptNilString {
	if s == "" {
		none := oas.OptNilString{}
		none.SetToNull()

		return none
	}

	return oas.NewOptNilString(s)
}

func nilPtrString(s *string) oas.NilString {
	if s == nil {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(*s)
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}

	return *t
}
