// License-file checkouts: parameter validation, the included
// relationships and the side effects; the certificate itself is built
// by the renderer + domain crypto.
package licenses

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// includeParams pages the included collections generously (the
// reference embeds every associated record).
var includeParams = paging.Params{Mode: paging.ModeLimit, Limit: includeWindow} //nolint:gochecknoglobals // a constant

// includePage is the include window of the entitlement union.
var includePage = paging.Page{Limit: includeWindow, Fetch: includeWindow} //nolint:gochecknoglobals // a constant

const (
	includeUserAlias = "user"
	includeProduct   = "product"
	includeWindow    = 100
)

// The checkout include whitelist.
var checkoutIncludes = map[string]bool{ //nolint:gochecknoglobals // a constant table
	"entitlements": true,
	"product":      true,
	"policy":       true,
	"group":        true,
	"owner":        true,
	"users":        true,
}

// CheckoutParams are the merged query and meta parameters of a
// checkout.
type CheckoutParams struct {
	Encrypt   *bool
	Algorithm string
	TTL       *int32
	// TTLSet distinguishes an absent ttl (the default month) from an
	// explicit null (perpetual).
	TTLSet  bool
	Include []string
}

// Checkout is the built license file: the license, its requested
// includes and the resolved parameters.
type Checkout struct {
	License  domain.License
	Includes []string
	// Product/Policy/Owner/Users/Group/Entitlements carry the included
	// documents (nil when not requested or absent).
	Product      *domain.Product
	Policy       *domain.Policy
	Owner        *domain.User
	OwnerStatus  string
	Users        []domain.User
	UserStatuses []string
	Group        *domain.Group
	Entitlements []domain.Entitlement

	Algorithm string
	Encrypted bool
	TTL       *int32
	IssuedAt  time.Time
	Expiry    *time.Time

	// FileID is the license-file resource id.
	FileID [16]byte
}

// Checkout validates the parameters, loads the includes and stamps the
// check-out.
func (s Scenario) Checkout( //nolint:cyclop,funlen // the parameter ladder, verbatim
	ctx context.Context, account domain.Account, idOrKey string, params CheckoutParams,
) (Checkout, error) {
	license, err := s.load(ctx, account, idOrKey)
	if err != nil {
		return Checkout{}, err
	}

	if err := access.Require(ctx, "license.check-out"); err != nil {
		return Checkout{}, err //nolint:wrapcheck // the authorization's own error
	}

	attached, err := s.userAttached(ctx, account.ID, access.PrincipalOf(ctx).BearerID, license)
	if err != nil {
		return Checkout{}, err
	}

	if err := access.LicenseAction(access.PrincipalOf(ctx), license, "check-out", attached); err != nil {
		return Checkout{}, err //nolint:wrapcheck // the authorization's own error
	}

	// "user" is the legacy spelling of the owner include.
	includes := make([]string, 0, len(params.Include))
	for _, include := range params.Include {
		if include == includeUserAlias {
			include = "owner"
		}

		includes = append(includes, include)
	}

	for _, include := range includes {
		if !checkoutIncludes[include] {
			return Checkout{}, checkoutError("CHECKOUT_INCLUDE_INVALID", "invalid includes", "include")
		}
	}

	encrypt := params.Encrypt != nil && *params.Encrypt

	ttl := params.TTL
	if !params.TTLSet {
		// An absent ttl means the default month.
		month := crypto.DefaultTTL()
		ttl = &month
	}

	algorithm, err := crypto.ResolveFileAlgorithm(
		params.Algorithm, encrypt, crypto.SchemeSigningAlgorithm(license.Policy.Scheme), ttl)
	if err != nil {
		var algErr *crypto.FileAlgorithmError

		var ttlErr *crypto.FileTTLError

		switch {
		case errors.As(err, &algErr):
			return Checkout{}, checkoutError("CHECKOUT_ALGORITHM_INVALID", algErr.Detail, "algorithm")
		case errors.As(err, &ttlErr):
			return Checkout{}, checkoutError("CHECKOUT_TTL_INVALID",
				"must be greater than or equal to 3600 (1 hour)", "ttl")
		default:
			return Checkout{}, fmt.Errorf("licenses: checkout: %w", err)
		}
	}

	now := s.now()

	out := Checkout{
		License: license, Includes: includes, Algorithm: algorithm,
		Encrypted: crypto.FileAlgorithmEncrypted(algorithm), TTL: ttl, IssuedAt: now,
	}

	if ttl != nil {
		expiry := now.Add(time.Duration(*ttl) * time.Second)
		out.Expiry = &expiry
	}

	if err := s.loadIncludes(ctx, account, license, &out); err != nil {
		return Checkout{}, err
	}

	if err := s.write(ctx, func(ctx context.Context) error {
		if err := s.store.SetLicenseLastCheckOut(ctx, account.ID, license.ID, now, now); err != nil {
			return fmt.Errorf("licenses: checkout: %w", err)
		}

		license.LastCheckOutAt = &now
		out.License = license

		return s.publish(ctx, "license.checked-out", license)
	}); err != nil {
		return Checkout{}, err
	}

	return out, nil
}

// loadIncludes resolves the requested included relationships.
func (s Scenario) loadIncludes( //nolint:cyclop,funlen // one branch per include
	ctx context.Context, account domain.Account, license domain.License, out *Checkout,
) error {
	for _, include := range out.Includes {
		switch include {
		case includeProduct:
			product, err := s.products.ProductByID(ctx, account.ID, license.ProductID)
			if err != nil {
				return fmt.Errorf("licenses: checkout product: %w", err)
			}

			out.Product = &product
		case "policy":
			policy := license.Policy
			out.Policy = &policy
		case "owner":
			if !license.OwnerID.Valid {
				continue
			}

			owner, err := s.users.UserByID(ctx, account.ID, license.OwnerID.UUID)
			if err != nil {
				continue // an absent owner renders no include
			}

			active, err := s.users.UserHasActiveLicense(ctx, account.ID, owner.ID)
			if err != nil {
				return fmt.Errorf("licenses: checkout owner: %w", err)
			}

			out.Owner = &owner
			out.OwnerStatus = owner.Status(active, s.now())
		case "users":
			f := users.Filter{LicenseID: uuid.NullUUID{UUID: license.ID, Valid: true}}

			listed, err := s.users.ListUsers(ctx, account.ID, f, "", uuid.Nil, includeParams)
			if err != nil {
				return fmt.Errorf("licenses: checkout users: %w", err)
			}

			for i := range listed {
				out.Users = append(out.Users, listed[i].User)
				out.UserStatuses = append(out.UserStatuses, listed[i].Status)
			}
		case "group":
			if !license.GroupID.Valid {
				continue
			}

			group, err := s.groups.GroupByID(ctx, account.ID, license.GroupID.UUID)
			if err != nil {
				continue
			}

			out.Group = &group
		case "entitlements":
			entitlements, err := s.entlist.ListLicenseEntitlementsOf(ctx, account.ID,
				license.PolicyID, license.ID, includePage)
			if err != nil {
				return fmt.Errorf("licenses: checkout entitlements: %w", err)
			}

			out.Entitlements = entitlements
		}
	}

	return nil
}

func checkoutError(code, detail, parameter string) error {
	return &apperr.T{
		Status: 400, Title: apperr.TitleBadRequest, Detail: detail, Code: code, //nolint:mnd // the reference's literal status
		Source: apperr.Source{Parameter: parameter},
	}
}
