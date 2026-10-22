package authn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// errUnknownFlow guards the credential resolver's exhaustive switch.
var errUnknownFlow = errors.New("authn: unknown authentication flow")

// Store is the scenario's consumer-side storage port.
type Store interface {
	VerifySecondFactor(ctx context.Context, userID uuid.UUID, otp string, now time.Time) (bool, error)
	// TokenByDigest loads a token by its digest; ErrNotFound when none.
	TokenByDigest(ctx context.Context, accountID uuid.UUID, digest string) (domain.Token, error)
	// UserByID loads a user; ErrNotFound when none.
	UserByID(ctx context.Context, accountID uuid.UUID, id uuid.UUID) (domain.User, error)
	// UserByEmail loads a user by downcased email; ErrNotFound when none.
	UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error)
	// LicenseByKey loads the license-auth slice by key; ErrNotFound.
	LicenseByKey(ctx context.Context, accountID uuid.UUID, key string) (domain.LicenseAuth, error)
	// LicenseByID loads the license-auth slice by id; ErrNotFound.
	LicenseByID(ctx context.Context, accountID uuid.UUID, id uuid.UUID) (domain.LicenseAuth, error)
	// ProductByID loads a product bearer; ErrNotFound when none.
	ProductByID(ctx context.Context, accountID uuid.UUID, id uuid.UUID) (domain.Product, error)
	// EnabledSecondFactorSecret loads the user's enabled TOTP secret;
	// ok is false when the user has none.
	EnabledSecondFactorSecret(ctx context.Context, userID uuid.UUID) (secret string, ok bool, err error)
}

// Scenario authenticates requests against an account.
type Scenario struct {
	store Store
	clock func() time.Time
}

// New builds the scenario over the store (positional deps, clock
// injectable for tests).
func New(store Store, clock func() time.Time) Scenario {
	return Scenario{store: store, clock: clock}
}

// Result is what the request learned about its caller, beyond the
// principal: the token row (token authn) and the bearer's user or
// license.
type Result struct {
	Token   *domain.Token
	User    *domain.User
	License *domain.LicenseAuth
	// ChallengeScheme feeds WWW-Authenticate on a later 401: Basic for
	// the password flow, Bearer otherwise.
	ChallengeScheme string
}

// Authenticate resolves the credential to a principal, in the reference's
// exact error order. A FlowNone credential yields the TOKEN_MISSING 401.
func (s Scenario) Authenticate(
	ctx context.Context,
	account domain.Account,
	cred Credentials,
	mode Mode,
	otp *string,
) (access.Principal, Result, error) {
	r := cred.Resolve(mode)
	result := Result{ChallengeScheme: r.ChallengeScheme()}

	switch r.Flow {
	case FlowToken:
		p, res, err := s.token(ctx, account, r.TokenValue)
		if err != nil {
			return access.Principal{}, result, err
		}

		return p, res, nil

	case FlowLicense:
		p, res, err := s.license(ctx, account, r.LicenseKey)
		if err != nil {
			return access.Principal{}, result, err
		}

		return p, res, nil

	case FlowPassword:
		p, res, err := s.password(ctx, account, r.Email, r.Password, otp)
		if err != nil {
			return access.Principal{}, result, err
		}

		return p, res, nil

	case FlowNone:
		return access.Principal{}, result, apperr.TokenUnauthorized("TOKEN_MISSING", "")

	default:
		return access.Principal{}, result, fmt.Errorf("%w: %d", errUnknownFlow, r.Flow)
	}
}

func (s Scenario) token(ctx context.Context, account domain.Account, value string) (access.Principal, Result, error) {
	result := Result{ChallengeScheme: "Bearer"}

	if value == "" {
		return access.Principal{}, result, apperr.TokenUnauthorized("TOKEN_MISSING", "")
	}

	if crypto.UUIDShaped(value) {
		return access.Principal{}, result, apperr.TokenUnauthorized(
			"TOKEN_FORMAT_INVALID",
			"Token format is invalid (make sure that you're providing a token value, not a token's UUID identifier)",
		)
	}

	token, err := s.tokenLookup(ctx, account, value)
	if errors.Is(err, ErrNotFound) {
		return access.Principal{}, result, apperr.TokenUnauthorized("TOKEN_INVALID", "")
	}

	if err != nil {
		return access.Principal{}, result, fmt.Errorf("authn: token lookup: %w", err)
	}

	if token.Expired(s.clock()) {
		return access.Principal{}, result, apperr.TokenUnauthorized("TOKEN_EXPIRED", "Token is expired")
	}

	// The role and permissions are derived from the live bearer below —
	// never from the token's denormalized snapshot.
	principal := access.Principal{
		BearerID: token.BearerID,
		TokenID:  token.ID,
	}

	result.Token = &token

	switch token.BearerType {
	case "User":
		user, err := s.bearerUser(ctx, account, token.BearerID)
		if err != nil {
			return access.Principal{}, result, err
		}

		principal = userPrincipal(principal, user, token)
		result.User = &user

	case "Product":
		if err := s.bearerProduct(ctx, account, token.BearerID); err != nil {
			return access.Principal{}, result, err
		}

		principal.BearerType = access.BearerProduct
		principal.Role = domain.RoleProduct
		principal.Permissions = access.RolePermissions(domain.RoleProduct)

	case "License":
		license, err := s.activationLicense(ctx, account, token.BearerID)
		if err != nil {
			return access.Principal{}, result, err
		}

		principal.BearerType = access.BearerLicense
		principal.Role = domain.RoleLicense
		principal.Permissions = access.RolePermissions(domain.RoleLicense)
		principal.License = &license
		result.License = &license

	default:
		// Orphaned bearer: the token exists but its bearer is gone.
		principal.Role = domain.RoleUser
		principal.Permissions = access.RolePermissions(domain.RoleUser)
	}

	return principal, result, nil
}

// tokenLookup resolves a raw token value to its row: only v3 secrets
// have a computable digest (v1/v2 would need the legacy schemes and
// authenticate as invalid).
func (s Scenario) tokenLookup(
	ctx context.Context, account domain.Account, value string,
) (domain.Token, error) {
	if crypto.Versioned(value) && version(value) == "v3" {
		return s.store.TokenByDigest( //nolint:wrapcheck // the repository's own error
			ctx, account.ID, crypto.DigestV3(account.Keys.SecretKey, value))
	}

	return domain.Token{}, ErrNotFound
}

// userPrincipal derives a user bearer's authorization from the live
// record (a demoted user's token loses its old privilege immediately):
// the role comes from the user row, the token's own snapshot never does.
func userPrincipal(principal access.Principal, user domain.User, token domain.Token) access.Principal {
	principal.BearerType = access.BearerUser
	principal.Role = user.Role
	principal.Permissions = access.Effective(user.Role, token.Permissions)

	return principal
}

// bearerProduct requires the token's product to exist: a deleted
// product's token no longer authenticates.
func (s Scenario) bearerProduct(ctx context.Context, account domain.Account, id uuid.UUID) error {
	_, err := s.store.ProductByID(ctx, account.ID, id)
	if errors.Is(err, ErrNotFound) {
		return apperr.TokenUnauthorized("TOKEN_INVALID", "")
	}

	if err != nil {
		return fmt.Errorf("authn: bearer product: %w", err)
	}

	return nil
}

// bearerUser runs the post-lookup checks for a user bearer: the banned
// check of the token flow.
func (s Scenario) bearerUser(ctx context.Context, account domain.Account, id uuid.UUID) (domain.User, error) {
	user, err := s.store.UserByID(ctx, account.ID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.User{}, apperr.TokenUnauthorized("TOKEN_INVALID", "")
	}

	if err != nil {
		return domain.User{}, fmt.Errorf("authn: bearer user: %w", err)
	}

	if user.Banned() {
		return domain.User{}, apperr.Forbidden("USER_BANNED", "User is banned")
	}

	return user, nil
}

// activationLicense runs the post-lookup checks for a license bearer
// (activation token): banned owner, then the policy's TOKEN strategy.
func (s Scenario) activationLicense(
	ctx context.Context, account domain.Account, id uuid.UUID,
) (domain.LicenseAuth, error) {
	license, err := s.licenseByID(ctx, account, id)
	if err != nil {
		return domain.LicenseAuth{}, err
	}

	if license.OwnerBanned {
		return domain.LicenseAuth{}, apperr.Forbidden("USER_BANNED", "User is banned")
	}

	if !license.SupportsTokenAuth() {
		return domain.LicenseAuth{}, apperr.Forbidden("TOKEN_NOT_ALLOWED", "Token authentication is not allowed by policy")
	}

	return license, nil
}

func (s Scenario) license(ctx context.Context, account domain.Account, key string) (access.Principal, Result, error) {
	result := Result{ChallengeScheme: "Bearer"}

	if key == "" {
		return access.Principal{}, result, apperr.TokenUnauthorized("TOKEN_MISSING", "")
	}

	lic, err := s.store.LicenseByKey(ctx, account.ID, key)
	if errors.Is(err, ErrNotFound) {
		return access.Principal{}, result, apperr.TokenUnauthorized("LICENSE_INVALID", "")
	}

	if err != nil {
		return access.Principal{}, result, fmt.Errorf("authn: license lookup: %w", err)
	}

	if err := checkLicense(lic, s.clock()); err != nil {
		return access.Principal{}, result, err
	}

	result.License = &lic

	principal := access.Principal{
		BearerType: access.BearerLicense,
		BearerID:   lic.ID,
		Role:       domain.RoleLicense,
		// License-key authn is not a token: the role's default set.
		Permissions: access.RolePermissions(domain.RoleLicense),
		License:     &lic,
	}

	return principal, result, nil
}

func checkLicense(lic domain.LicenseAuth, now time.Time) error {
	switch {
	case lic.OwnerBanned:
		return apperr.Forbidden("LICENSE_BANNED", "License is banned")
	case lic.Suspended:
		return apperr.Forbidden("LICENSE_SUSPENDED", "License is suspended")
	case lic.RestrictsAccess() && lic.Expired(now):
		return apperr.Forbidden("LICENSE_EXPIRED", "License is expired")
	case !lic.SupportsLicenseAuth():
		return apperr.Forbidden("LICENSE_NOT_ALLOWED", "License key authentication is not allowed by policy")
	default:
		return nil
	}
}

func (s Scenario) licenseByID(ctx context.Context, account domain.Account, id uuid.UUID) (domain.LicenseAuth, error) {
	lic, err := s.store.LicenseByID(ctx, account.ID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.LicenseAuth{}, apperr.TokenUnauthorized("TOKEN_INVALID", "")
	}

	if err != nil {
		return domain.LicenseAuth{}, fmt.Errorf("authn: license lookup: %w", err)
	}

	return lic, nil
}

// password runs the Basic email:password flow in the reference's error
// order (email, user, password support, blank, OTP, password). The token
// policy checks the live banned state after typed body validation.
func (s Scenario) password(
	ctx context.Context,
	account domain.Account,
	email, password string, otp *string,
) (access.Principal, Result, error) {
	result := Result{ChallengeScheme: "Basic"}

	if !emailRe.MatchString(email) {
		return access.Principal{}, result, unauthorizedHeader("EMAIL_REQUIRED", "email is required")
	}

	user, err := s.store.UserByEmail(ctx, account.ID, email)
	if errors.Is(err, ErrNotFound) {
		return access.Principal{}, result, unauthorizedHeader("EMAIL_INVALID", "email must be valid")
	}

	if err != nil {
		return access.Principal{}, result, fmt.Errorf("authn: user by email: %w", err)
	}

	if !user.HasPassword() {
		return access.Principal{}, result, unauthorizedHeader("PASSWORD_NOT_SUPPORTED", "password is unsupported")
	}

	if strings.TrimSpace(password) == "" {
		return access.Principal{}, result, unauthorizedHeader("PASSWORD_REQUIRED", "password is required")
	}

	_, enabled, err := s.store.EnabledSecondFactorSecret(ctx, user.ID)
	if err != nil {
		return access.Principal{}, result, fmt.Errorf("authn: second factor: %w", err)
	}

	if enabled {
		if otp == nil {
			return access.Principal{}, result, otpError("OTP_REQUIRED", "second factor is required")
		}

		valid, err := s.store.VerifySecondFactor(ctx, user.ID, *otp, s.clock())
		if err != nil {
			return access.Principal{}, result, fmt.Errorf("authn: verify second factor: %w", err)
		}

		if !valid {
			return access.Principal{}, result, otpError("OTP_INVALID", "second factor must be valid")
		}
	}

	if !user.Authenticates(password) {
		return access.Principal{}, result, unauthorizedHeader("PASSWORD_INVALID", "password must be valid")
	}

	result.User = &user

	return access.Principal{
		UserBanned:  user.Banned(),
		BearerType:  access.BearerUser,
		BearerID:    user.ID,
		Role:        user.Role,
		Permissions: access.RolePermissions(user.Role),
	}, result, nil
}

// unauthorizedHeader builds a password-flow 401 pointing at the
// Authorization header.
func unauthorizedHeader(code, detail string) error {
	return apperr.UnauthorizedSource(code, detail, "Authorization", "")
}

// otpError builds the OTP 401 pointing at the body's /meta/otp.
func otpError(code, detail string) error {
	return apperr.UnauthorizedSource(code, detail, "", "/meta/otp")
}

// version returns the trailing v<digits> marker of a versioned value.
func version(value string) string {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == 'v' && i > 0 {
			return value[i:]
		}

		if value[i] < '0' || value[i] > '9' {
			return ""
		}
	}

	return ""
}
