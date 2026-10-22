package tokens

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// Filter is the tokens index filter set (bearer[type], bearer[id],
// bearer[role][]).
type Filter struct {
	// BearerType is the stored polymorphic type (User/Product/License).
	BearerType string
	BearerID   uuid.NullUUID
	// BearerRoles are stored (underscored) role names.
	BearerRoles []string
}

// Page is one result page with its total.
type Page struct {
	Rows  []domain.Token
	Total int
}

// CreateAttrs is the write payload of a token creation (the generate
// endpoint and the bearer sub-routes).
type CreateAttrs struct {
	Name   *string
	Expiry *time.Time
	// MaxActivations/MaxDeactivations apply to activation tokens (the
	// license sub-route).
	MaxActivations   *int
	MaxDeactivations *int
}

// Store is the scenario's consumer-side storage port.
type Store interface {
	// InsertToken writes a new token row.
	InsertToken(ctx context.Context, token domain.Token) error
	// BearerUser loads the override bearer (a user).
	BearerUser(ctx context.Context, accountID uuid.UUID, id uuid.UUID) (domain.User, error)
	// TokenByID loads a token by its id.
	TokenByID(ctx context.Context, accountID, id uuid.UUID) (domain.Token, error)
	// ListTokens answers the filtered, scoped page.
	ListTokens(ctx context.Context, accountID uuid.UUID, f Filter, scopeKind string,
		scopeID uuid.UUID, p paging.Params,
	) ([]domain.Token, error)
	// CountTokens answers the filtered, scoped total.
	CountTokens(ctx context.Context, accountID uuid.UUID, f Filter, scopeKind string, scopeID uuid.UUID) (int, error)
	// RegenerateToken writes the new digest (and reset expiry).
	RegenerateToken(ctx context.Context, accountID, id uuid.UUID, digest string, expiry *time.Time) error
	// DeleteToken removes the row.
	DeleteToken(ctx context.Context, accountID, id uuid.UUID) error
	// UserLicensedToProduct answers the product-scope membership check.
	UserLicensedToProduct(ctx context.Context, accountID, userID, productID uuid.UUID) (bool, error)
	// LicenseOfProduct answers whether the license belongs to the
	// product.
	LicenseOfProduct(ctx context.Context, accountID, licenseID, productID uuid.UUID) (bool, error)
}

// Manager is the token management face of the scenario (index, show,
// regenerate, revoke and the sub-route creations). It is a value over
// the same Store so other scenarios reuse the creation logic.
type Manager struct {
	Scenario
}

// List answers the filtered page of the principal's scope.
func (m Manager) List(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	f Filter,
	p paging.Params,
) (Page, error) {
	if err := access.Require(ctx, "token.read"); err != nil {
		return Page{}, err //nolint:wrapcheck // the port's own error
	}

	rows, err := m.store.ListTokens(ctx, account.ID, f, access.TokenScopeKind(principal), principal.BearerID, p)
	if err != nil {
		return Page{}, fmt.Errorf("tokens: list: %w", err)
	}

	total := len(rows)
	if p.Mode == paging.ModeOffset {
		total, err = m.store.CountTokens(ctx, account.ID, f, access.TokenScopeKind(principal), principal.BearerID)
		if err != nil {
			return Page{}, fmt.Errorf("tokens: count: %w", err)
		}
	}

	return Page{Rows: rows, Total: total}, nil
}

// Show loads one token under the TokenPolicy.
func (m Manager) Show(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.Token, error) {
	if err := access.Require(ctx, "token.read"); err != nil {
		return domain.Token{}, err //nolint:wrapcheck // the port's own error
	}

	token, err := m.load(ctx, account, id)
	if err != nil {
		return domain.Token{}, err
	}

	if err := access.TokenShow(principal, token,
		m.userOfProduct(ctx, account.ID, token),
		m.licenseOfProduct(ctx, account.ID, token),
	); err != nil {
		return domain.Token{}, err //nolint:wrapcheck // the authorization's own error
	}

	return token, nil
}

// CreateForUser mints a token FOR the user (the /users/{id}/tokens
// route): admin, developer, or a product the user is licensed to.
func (m Manager) CreateForUser(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	user domain.User,
	attrs CreateAttrs,
) (domain.Token, string, error) {
	if err := access.Require(ctx, "token.generate"); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the port's own error
	}

	if err := access.TokenGenerateForUser(principal, user, func() bool {
		ok, err := m.store.UserLicensedToProduct(ctx, account.ID, user.ID, principal.BearerID)

		return err == nil && ok
	}); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the authorization's own error
	}

	return m.insert(ctx, account, user.ID, "User", user.Role, attrs)
}

// CreateForLicense mints an activation token for the license (the
// /licenses/{id}/tokens route — owned by the licenses group, kept here
// for reuse).
func (m Manager) CreateForLicense(
	ctx context.Context,
	account domain.Account,
	_ access.Principal,
	licenseID uuid.UUID,
	attrs CreateAttrs,
) (domain.Token, string, error) {
	if err := access.Require(ctx, "token.generate"); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the port's own error
	}

	if attrs.MaxActivations != nil && *attrs.MaxActivations < 0 {
		return domain.Token{}, "", apperr.Unprocessable(
			"MAX_ACTIVATIONS_INVALID", "must be greater than or equal to 0", "/data/attributes/maxActivations",
		)
	}

	if attrs.MaxDeactivations != nil && *attrs.MaxDeactivations < 0 {
		return domain.Token{}, "", apperr.Unprocessable(
			"MAX_DEACTIVATIONS_INVALID", "must be greater than or equal to 0", "/data/attributes/maxDeactivations",
		)
	}

	token, raw, err := m.insert(ctx, account, licenseID, "License", domain.RoleLicense, attrs)
	if err != nil {
		return domain.Token{}, "", err
	}

	// The counters are persisted with the row by the caller through
	// UpdateToken; refresh the in-memory value for the response.
	token.MaxActivations = attrs.MaxActivations
	token.MaxDeactivations = attrs.MaxDeactivations

	return token, raw, nil
}

// Regenerate issues a new secret for the token: a present expiry is
// reset to now + 2 weeks, a nil one stays nil.
func (m Manager) Regenerate(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.Token, string, error) {
	if err := access.Require(ctx, "token.regenerate"); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the port's own error
	}

	token, err := m.loadScoped(ctx, account, principal, id)
	if err != nil {
		return domain.Token{}, "", err
	}

	if err := access.TokenRegenerate(principal, token,
		m.userOfProduct(ctx, account.ID, token),
		m.licenseOfProduct(ctx, account.ID, token),
	); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the authorization's own error
	}

	return m.regenerate(ctx, account, token)
}

// RegenerateCurrent issues a new secret for the acting token; it 404s
// unless the request is token-authenticated.
func (m Manager) RegenerateCurrent(
	ctx context.Context, account domain.Account, principal access.Principal,
) (domain.Token, string, error) {
	if err := access.Require(ctx, "token.regenerate"); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the port's own error
	}

	if !principal.IsTokenAuthn() {
		return domain.Token{}, "", apperr.NotFound("token", "tokens")
	}

	token, err := m.store.TokenByID(ctx, account.ID, principal.TokenID)
	if err != nil {
		return domain.Token{}, "", apperr.NotFound("token", principal.TokenID.String())
	}

	return m.regenerate(ctx, account, token)
}

// Revoke destroys the token.
func (m Manager) Revoke(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) error {
	if err := access.Require(ctx, "token.revoke"); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	token, err := m.loadScoped(ctx, account, principal, id)
	if err != nil {
		return err
	}

	if err := access.TokenRevoke(principal, token,
		m.userOfProduct(ctx, account.ID, token),
		m.licenseOfProduct(ctx, account.ID, token),
	); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := m.store.DeleteToken(ctx, account.ID, token.ID); err != nil {
		return fmt.Errorf("tokens: revoke: %w", err)
	}

	return m.events.Publish(ctx, "token.revoked", token) //nolint:wrapcheck // the port's own error
}

func (m Manager) regenerate(
	ctx context.Context, account domain.Account, token domain.Token,
) (domain.Token, string, error) {
	raw, err := crypto.GenerateTokenSecret(m.rng, string(token.BearerRole))
	if err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: secret: %w", err)
	}

	expiry := token.Expiry
	if expiry != nil {
		t := m.clock().Add(UserDuration)
		expiry = &t
	}

	digest := crypto.DigestV3(account.Keys.SecretKey, raw)
	if err := m.store.RegenerateToken(ctx, account.ID, token.ID, digest, expiry); err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: regenerate: %w", err)
	}

	token.Expiry = expiry

	if err := m.events.Publish(ctx, "token.regenerated", token); err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: regenerate: %w", err)
	}

	return token, raw, nil
}

// insert writes the token row (the shared creation logic).
func (m Manager) insert(
	ctx context.Context,
	account domain.Account,
	bearerID uuid.UUID,
	bearerType string,
	role domain.Role,
	attrs CreateAttrs,
) (domain.Token, string, error) {
	raw, err := crypto.GenerateTokenSecret(m.rng, string(role))
	if err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: secret: %w", err)
	}

	now := m.clock()

	token := domain.Token{
		ID:               uuid.New(),
		AccountID:        account.ID,
		Digest:           crypto.DigestV3(account.Keys.SecretKey, raw),
		BearerID:         bearerID,
		BearerType:       bearerType,
		BearerRole:       role,
		Kind:             crypto.TokenKind(string(role)),
		Name:             attrs.Name,
		Expiry:           expiry(attrs.Expiry, role, now),
		Permissions:      []string{"*"},
		MaxActivations:   attrs.MaxActivations,
		MaxDeactivations: attrs.MaxDeactivations,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := m.store.InsertToken(ctx, token); err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: insert: %w", err)
	}

	if err := m.events.Publish(ctx, "token.generated", token); err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: insert: %w", err)
	}

	return token, raw, nil
}

func (m Manager) load(
	ctx context.Context, account domain.Account, id uuid.UUID,
) (domain.Token, error) {
	token, err := m.store.TokenByID(ctx, account.ID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.Token{}, apperr.NotFound("token", id.String())
	}

	if err != nil {
		return domain.Token{}, fmt.Errorf("tokens: load: %w", err)
	}

	return token, nil
}

// loadScoped loads the token inside the principal's authorized scope:
// like the reference's set_token, an invisible token is the record 404.
func (m Manager) loadScoped(
	ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID,
) (domain.Token, error) {
	token, err := m.load(ctx, account, id)
	if err != nil {
		return domain.Token{}, err
	}

	if !m.inIndexScope(ctx, account, principal, token) {
		return domain.Token{}, apperr.NotFound("token", id.String())
	}

	return token, nil
}

// inIndexScope answers the authorized_scope visibility (the index
// query's scope predicate).
func (m Manager) inIndexScope(
	ctx context.Context, account domain.Account, principal access.Principal, token domain.Token,
) bool {
	switch access.TokenScopeKind(principal) {
	case "":
		return true
	case "user":
		return token.BearerType == "User" && token.BearerID == principal.BearerID
	case "license":
		return token.BearerType == "License" && token.BearerID == principal.BearerID
	case "product":
		if token.BearerType == "Product" {
			return token.BearerID == principal.BearerID
		}

		if m.userOfProduct(ctx, account.ID, token)() {
			return true
		}

		return m.licenseOfProduct(ctx, account.ID, token)()
	default:
		return false
	}
}

func (m Manager) userOfProduct(ctx context.Context, accountID uuid.UUID, token domain.Token) func() bool {
	return func() bool {
		if token.BearerType != "User" {
			return false
		}

		ok, err := m.store.UserLicensedToProduct(ctx, accountID, token.BearerID, m.productScope(ctx))

		return err == nil && ok
	}
}

func (m Manager) licenseOfProduct(ctx context.Context, accountID uuid.UUID, token domain.Token) func() bool {
	return func() bool {
		if token.BearerType != "License" {
			return false
		}

		ok, err := m.store.LicenseOfProduct(ctx, accountID, token.BearerID, m.productScope(ctx))

		return err == nil && ok
	}
}

// productScope answers the acting product bearer's id (zero for
// everyone else; the product clauses are only consulted for product
// bearers).
func (m Manager) productScope(ctx context.Context) uuid.UUID {
	p := access.PrincipalOf(ctx)
	if p.IsProduct() {
		return p.BearerID
	}

	return uuid.Nil
}
