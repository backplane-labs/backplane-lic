package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/search"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// The attributes several resources are searched by.
const (
	attrMetadata = "metadata"
	attrName     = "name"
	attrProduct  = "product"
)

var _ search.Store = (*Store)(nil)

var errUnsearchable = errors.New("attribute is not searchable")

// lookup is one attribute search: the term in the forms the queries take.
type lookup struct {
	q       *db.Queries
	account uuid.UUID
	t       search.Term
	id      *uuid.UUID
	like    string
}

// SearchIDs answers the ids the term matches in one attribute of a resource.
func (s *Store) SearchIDs(
	ctx context.Context, accountID uuid.UUID, resource, attribute string, t search.Term,
) ([]uuid.UUID, error) {
	w := lookup{q: s.Q, account: accountID, t: t, id: nullUUIDPtr(t.ID), like: t.Like}

	var (
		ids []uuid.UUID
		err error
	)

	switch resource {
	case search.Licenses:
		ids, err = w.licenses(ctx, attribute)
	case search.Users:
		ids, err = w.users(ctx, attribute)
	case search.Products:
		ids, err = w.products(ctx, attribute)
	case search.Policies:
		ids, err = w.policies(ctx, attribute)
	case search.Groups:
		ids, err = w.groups(ctx, attribute)
	case search.Entitlements:
		ids, err = w.entitlements(ctx, attribute)
	case search.Machines:
		ids, err = w.machines(ctx, attribute)
	default:
		err = errUnsearchable
	}

	if err != nil {
		return nil, fmt.Errorf("postgres: search %s.%s: %w", resource, attribute, err)
	}

	return ids, nil
}

func (w lookup) licenses(ctx context.Context, attribute string) ([]uuid.UUID, error) {
	switch attribute {
	case "id":
		return collect(w.q.SearchLicensesByID(ctx, db.SearchLicensesByIDParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
		}))
	case "key":
		return collect(w.q.SearchLicensesByKey(ctx, db.SearchLicensesByKeyParams{
			AccountID: w.account,
			Term:      w.t.Text,
		}))
	case attrName:
		return collect(w.q.SearchLicensesByName(ctx, db.SearchLicensesByNameParams{
			AccountID: w.account,
			Like:      &w.like,
		}))
	case attrMetadata:
		return collect(w.q.SearchLicensesByMetadata(ctx, db.SearchLicensesByMetadataParams{
			AccountID: w.account,
			Raw:       w.t.Raw,
			Cast:      w.t.Cast,
		}))
	case "owner":
		return collect(w.q.SearchLicensesByOwner(ctx, db.SearchLicensesByOwnerParams{
			AccountID: w.account,
			ID:        w.id,
			Term:      &w.t.Text,
			Like:      &w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	case "user":
		return collect(w.q.SearchLicensesByUser(ctx, db.SearchLicensesByUserParams{
			AccountID: w.account,
			ID:        w.id,
			Term:      &w.t.Text,
			Like:      &w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	case attrProduct:
		return collect(w.q.SearchLicensesByProduct(ctx, db.SearchLicensesByProductParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      &w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	case "policy":
		return collect(w.q.SearchLicensesByPolicy(ctx, db.SearchLicensesByPolicyParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	default:
		return nil, errUnsearchable
	}
}

func (w lookup) users(ctx context.Context, attribute string) ([]uuid.UUID, error) {
	switch attribute {
	case "id":
		return collect(w.q.SearchUsersByID(ctx, db.SearchUsersByIDParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
		}))
	case "email":
		return collect(w.q.SearchUsersByEmail(ctx, db.SearchUsersByEmailParams{AccountID: w.account, Like: &w.like}))
	case "first_name":
		return collect(w.q.SearchUsersByFirstName(ctx, db.SearchUsersByFirstNameParams{
			AccountID: w.account,
			Like:      &w.like,
		}))
	case "last_name":
		return collect(w.q.SearchUsersByLastName(ctx, db.SearchUsersByLastNameParams{
			AccountID: w.account,
			Like:      &w.like,
		}))
	case attrMetadata:
		return collect(w.q.SearchUsersByMetadata(ctx, db.SearchUsersByMetadataParams{
			AccountID: w.account,
			Raw:       w.t.Raw,
			Cast:      w.t.Cast,
		}))
	case "role":
		if w.t.ID.Valid {
			return []uuid.UUID{}, nil
		}

		return collect(w.q.SearchUsersByRole(ctx, db.SearchUsersByRoleParams{
			AccountID: w.account,
			Like:      w.like,
		}))
	default:
		return nil, errUnsearchable
	}
}

func (w lookup) products(ctx context.Context, attribute string) ([]uuid.UUID, error) {
	switch attribute {
	case "id":
		return collect(w.q.SearchProductsByID(ctx, db.SearchProductsByIDParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
		}))
	case attrName:
		return collect(w.q.SearchProductsByName(ctx, db.SearchProductsByNameParams{
			AccountID: w.account,
			Like:      &w.like,
		}))
	case attrMetadata:
		return collect(w.q.SearchProductsByMetadata(ctx, db.SearchProductsByMetadataParams{
			AccountID: w.account,
			Raw:       w.t.Raw,
			Cast:      w.t.Cast,
		}))
	default:
		return nil, errUnsearchable
	}
}

func (w lookup) policies(ctx context.Context, attribute string) ([]uuid.UUID, error) {
	switch attribute {
	case "id":
		return collect(w.q.SearchPoliciesByID(ctx, db.SearchPoliciesByIDParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
		}))
	case attrName:
		return collect(w.q.SearchPoliciesByName(ctx, db.SearchPoliciesByNameParams{
			AccountID: w.account,
			Like:      w.like,
		}))
	case attrMetadata:
		return collect(w.q.SearchPoliciesByMetadata(ctx, db.SearchPoliciesByMetadataParams{
			AccountID: w.account,
			Raw:       w.t.Raw,
			Cast:      w.t.Cast,
		}))
	case attrProduct:
		return collect(w.q.SearchPoliciesByProduct(ctx, db.SearchPoliciesByProductParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      &w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	default:
		return nil, errUnsearchable
	}
}

func (w lookup) groups(ctx context.Context, attribute string) ([]uuid.UUID, error) {
	switch attribute {
	case attrName:
		return collect(w.q.SearchGroupsByName(ctx, db.SearchGroupsByNameParams{AccountID: w.account, Like: &w.like}))
	default:
		return nil, errUnsearchable
	}
}

func (w lookup) entitlements(ctx context.Context, attribute string) ([]uuid.UUID, error) {
	switch attribute {
	case "id":
		return collect(w.q.SearchEntitlementsByID(ctx, db.SearchEntitlementsByIDParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
		}))
	case "code":
		return collect(w.q.SearchEntitlementsByCode(ctx, db.SearchEntitlementsByCodeParams{
			AccountID: w.account,
			Like:      w.like,
		}))
	case attrName:
		return collect(w.q.SearchEntitlementsByName(ctx, db.SearchEntitlementsByNameParams{
			AccountID: w.account,
			Like:      w.like,
		}))
	default:
		return nil, errUnsearchable
	}
}

func (w lookup) machines(ctx context.Context, attribute string) ([]uuid.UUID, error) {
	switch attribute {
	case "id":
		return collect(w.q.SearchMachinesByID(ctx, db.SearchMachinesByIDParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
		}))
	case "fingerprint":
		return collect(w.q.SearchMachinesByFingerprint(ctx, db.SearchMachinesByFingerprintParams{
			AccountID: w.account,
			Like:      w.like,
		}))
	case attrName:
		return collect(w.q.SearchMachinesByName(ctx, db.SearchMachinesByNameParams{
			AccountID: w.account,
			Like:      &w.like,
		}))
	case attrMetadata:
		return collect(w.q.SearchMachinesByMetadata(ctx, db.SearchMachinesByMetadataParams{
			AccountID: w.account,
			Raw:       w.t.Raw,
			Cast:      w.t.Cast,
		}))
	case "license":
		return collect(w.q.SearchMachinesByLicense(ctx, db.SearchMachinesByLicenseParams{
			AccountID: w.account,
			ID:        w.id,
			Term:      w.t.Text,
			Like:      w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	case "owner":
		return collect(w.q.SearchMachinesByOwner(ctx, db.SearchMachinesByOwnerParams{
			AccountID: w.account,
			ID:        w.id,
			Term:      &w.t.Text,
			Like:      &w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	case "user":
		return collect(w.q.SearchMachinesByUser(ctx, db.SearchMachinesByUserParams{
			AccountID: w.account,
			ID:        w.id,
			Term:      &w.t.Text,
			Like:      &w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	case attrProduct:
		return collect(w.q.SearchMachinesByProduct(ctx, db.SearchMachinesByProductParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      &w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	case "policy":
		return collect(w.q.SearchMachinesByPolicy(ctx, db.SearchMachinesByPolicyParams{
			AccountID: w.account,
			ID:        w.id,
			Like:      w.like,
			Prefix:    w.t.Prefix,
			Tsv:       w.t.TSV,
		}))
	default:
		return nil, errUnsearchable
	}
}

func collect[R ~struct{ ID uuid.UUID }](rows []R, err error) ([]uuid.UUID, error) {
	if err != nil {
		return nil, err
	}

	out := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		out = append(out, struct{ ID uuid.UUID }(row).ID)
	}

	return out, nil
}
