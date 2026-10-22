// Package tokens is the token scenario: generation (the POST /tokens
// endpoint). Regeneration and revocation land with their routes.
package tokens

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// UserDuration is the default expiry of user tokens; every other kind
// never expires by default.
const UserDuration = 14 * 24 * time.Hour

// The stored polymorphic bearer types.
const (
	storedBearerTypeUser    = "User"
	storedBearerTypeLicense = "License"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// GenerateInput is the request body of POST /tokens (already decoded and
// shape-validated by the handler).
type GenerateInput struct {
	// ExpirySet distinguishes explicit null (non-expiring) from the default.
	ExpirySet bool
	Name      *string
	Expiry    *time.Time
	// BearerType/BearerID carry an admin's relationships.bearer override
	// (wire types: users, products, licenses); empty when absent.
	BearerType string
	BearerID   uuid.UUID
}

// Scenario generates tokens.
type Scenario struct {
	store  Store
	events events.Publisher
	rng    io.Reader
	clock  func() time.Time
}

// New builds the scenario (rng and clock injectable for tests).
func New(store Store, publisher events.Publisher, rng io.Reader, clock func() time.Time) Scenario {
	return Scenario{store: store, events: publisher, rng: rng, clock: clock}
}

// Manager returns the management face (index, show, regenerate, revoke,
// sub-route creations) over the same store.
func (s Scenario) Manager() Manager {
	return Manager{Scenario: s}
}

// Generate creates a token for the principal (or the admin's override
// bearer) and answers the row plus the raw secret, returned exactly
// once, in the creation response.
func (s Scenario) Generate(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	in GenerateInput,
) (domain.Token, string, error) {
	if err := access.Require(ctx, "token.generate"); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the authorization's own error
	}

	bearerID, bearerRole, bearerType, err := s.resolveBearer(ctx, account, principal, in)
	if err != nil {
		return domain.Token{}, "", err
	}

	raw, err := crypto.GenerateTokenSecret(s.rng, string(bearerRole))
	if err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: secret: %w", err)
	}

	now := s.clock()

	token := domain.Token{
		ID:          uuid.New(),
		AccountID:   account.ID,
		Digest:      crypto.DigestV3(account.Keys.SecretKey, raw),
		BearerID:    bearerID,
		BearerType:  bearerType,
		BearerRole:  bearerRole,
		Kind:        crypto.TokenKind(string(bearerRole)),
		Name:        in.Name,
		Expiry:      expiry(in.Expiry, in.ExpirySet, bearerRole, now),
		Permissions: []string{"*"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.InsertToken(ctx, token); err != nil {
			return fmt.Errorf("tokens: insert: %w", err)
		}

		return s.events.Publish(ctx, "token.generated", token)
	}); err != nil {
		return domain.Token{}, "", fmt.Errorf("tokens: insert: %w", err)
	}

	return token, raw, nil
}

// resolveBearer answers the token's bearer: the override the request
// selected (mirroring the reference's TokenPolicy#generate? — only
// admin/developer may mint for a plain user) or the principal itself.
func (s Scenario) resolveBearer(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	in GenerateInput,
) (uuid.UUID, domain.Role, string, error) {
	if in.BearerType == "" {
		if principal.Role == domain.RoleUser && principal.UserBanned {
			return uuid.Nil, "", "", apperr.ForbiddenReason(" (user is banned)")
		}

		if !allowsGenerateSelf(principal) {
			// The policy admits no self-token for this bearer kind (a
			// product or license bearer cannot mint its own token).
			return uuid.Nil, "", "", apperr.ForbiddenReason(access.ReasonAllResources)
		}

		return principal.BearerID, principal.Role, storedBearerType(principal.BearerType), nil
	}

	if in.BearerType != access.BearerUser {
		// The typed shape allows product/license bearers, but the
		// token policy never admits them (their sub-routes arrive
		// with their own scenarios).
		return uuid.Nil, "", "", apperr.ForbiddenReason(access.ReasonAllResources)
	}

	user, err := s.store.BearerUser(ctx, account.ID, in.BearerID)
	if errors.Is(err, ErrNotFound) {
		return uuid.Nil, "", "", apperr.NotFound("user", in.BearerID.String())
	}

	if err != nil {
		return uuid.Nil, "", "", fmt.Errorf("tokens: bearer user: %w", err)
	}

	// The caller's own bearer always passes the first clause; an
	// override to another bearer only the admin/developer-for-user
	// clause.
	self := principal.BearerType == access.BearerUser && user.ID == principal.BearerID
	if !self && !allowsGenerateFor(principal, user.Role) {
		return uuid.Nil, "", "", apperr.ForbiddenReason(access.ReasonAllResources)
	}

	if self && principal.Role == domain.RoleUser && principal.UserBanned {
		return uuid.Nil, "", "", apperr.ForbiddenReason(" (user is banned)")
	}

	return user.ID, user.Role, storedBearerTypeUser, nil
}

func expiry(explicit *time.Time, present bool, role domain.Role, now time.Time) *time.Time {
	if present || explicit != nil {
		return explicit
	}

	if role == domain.RoleUser {
		t := now.Add(UserDuration)

		return &t
	}

	return nil
}

func storedBearerType(wire string) string {
	switch wire {
	case access.BearerUser:
		return storedBearerTypeUser
	case access.BearerProduct:
		return "Product"
	case access.BearerLicense:
		return storedBearerTypeLicense
	default:
		return ""
	}
}

// allowsGenerateSelf mirrors the reference's TokenPolicy#generate? first
// clause: these roles may mint a token whose bearer is themselves. The
// user role has its own clause; product and license bearers have none at
// all — they cannot mint tokens for themselves.
func allowsGenerateSelf(p access.Principal) bool {
	switch p.Role {
	case domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent,
		domain.RoleSupportRole, domain.RoleReadOnly, domain.RoleUser:
		return p.BearerType == access.BearerUser
	default:
		return false
	}
}

// allowsGenerateFor mirrors the override clauses of TokenPolicy#generate?:
// an admin or developer may mint tokens for plain users (and for
// themselves); sales/support/read-only and everyone else may not mint
// for anyone but themselves (the self case never reaches an override).
func allowsGenerateFor(p access.Principal, target domain.Role) bool {
	switch p.Role {
	case domain.RoleAdmin, domain.RoleDeveloper:
		return target == domain.RoleUser
	default:
		return false
	}
}
