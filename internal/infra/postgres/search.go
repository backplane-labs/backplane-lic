package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/search"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

var _ search.Store = (*Store)(nil)

// SearchIDs answers one attribute's matching ids of a resource type.
func (s *Store) SearchIDs( //nolint:cyclop,funlen // one query per searchable attribute
	ctx context.Context, accountID uuid.UUID, resource, attribute string, t search.Term,
) ([]uuid.UUID, error) {
	queries := s.Q
	id := nullUUIDPtr(t.ID)
	like := t.Like

	var (
		ids []uuid.UUID
		err error
	)

	switch resource + "." + attribute {
	case "licenses.id":
		ids, err = collect(queries.SearchLicensesByID(ctx, db.SearchLicensesByIDParams{
			AccountID: accountID, ID: id, Like: like,
		}))
	case "licenses.key":
		ids, err = collect(queries.SearchLicensesByKey(ctx, db.SearchLicensesByKeyParams{
			AccountID: accountID, Term: t.Text,
		}))
	case "licenses.name":
		ids, err = collect(queries.SearchLicensesByName(ctx, db.SearchLicensesByNameParams{
			AccountID: accountID, Like: &like,
		}))
	case "licenses.metadata":
		ids, err = collect(queries.SearchLicensesByMetadata(ctx, db.SearchLicensesByMetadataParams{
			AccountID: accountID, Raw: t.Raw, Cast: t.Cast,
		}))
	case "licenses.owner":
		ids, err = collect(queries.SearchLicensesByOwner(ctx, db.SearchLicensesByOwnerParams{
			AccountID: accountID, ID: id, Term: &t.Text, Like: &like, Prefix: t.Prefix, Tsv: t.TSV,
		}))
	case "licenses.user":
		ids, err = collect(queries.SearchLicensesByUser(ctx, db.SearchLicensesByUserParams{
			AccountID: accountID, ID: id, Term: &t.Text, Like: &like, Prefix: t.Prefix, Tsv: t.TSV,
		}))
	case "licenses.product":
		ids, err = collect(queries.SearchLicensesByProduct(ctx, db.SearchLicensesByProductParams{
			AccountID: accountID, ID: id, Like: &like, Prefix: t.Prefix, Tsv: t.TSV,
		}))
	case "licenses.policy":
		ids, err = collect(queries.SearchLicensesByPolicy(ctx, db.SearchLicensesByPolicyParams{
			AccountID: accountID, ID: id, Like: like, Prefix: t.Prefix, Tsv: t.TSV,
		}))
	case "users.id":
		ids, err = collect(queries.SearchUsersByID(ctx, db.SearchUsersByIDParams{
			AccountID: accountID, ID: id, Like: like,
		}))
	case "users.email":
		ids, err = collect(queries.SearchUsersByEmail(ctx, db.SearchUsersByEmailParams{
			AccountID: accountID, Like: &like,
		}))
	case "users.first_name":
		ids, err = collect(queries.SearchUsersByFirstName(ctx, db.SearchUsersByFirstNameParams{
			AccountID: accountID, Like: &like,
		}))
	case "users.last_name":
		ids, err = collect(queries.SearchUsersByLastName(ctx, db.SearchUsersByLastNameParams{
			AccountID: accountID, Like: &like,
		}))
	case "users.metadata":
		ids, err = collect(queries.SearchUsersByMetadata(ctx, db.SearchUsersByMetadataParams{
			AccountID: accountID, Raw: t.Raw, Cast: t.Cast,
		}))
	case "users.role":
		if t.ID.Valid {
			return []uuid.UUID{}, nil // roles have no ids of their own here
		}

		ids, err = collect(queries.SearchUsersByRole(ctx, db.SearchUsersByRoleParams{AccountID: accountID, Like: like}))
	case "products.id":
		ids, err = collect(queries.SearchProductsByID(ctx, db.SearchProductsByIDParams{
			AccountID: accountID, ID: id, Like: like,
		}))
	case "products.name":
		ids, err = collect(queries.SearchProductsByName(ctx, db.SearchProductsByNameParams{
			AccountID: accountID, Like: &like,
		}))
	case "products.metadata":
		ids, err = collect(queries.SearchProductsByMetadata(ctx, db.SearchProductsByMetadataParams{
			AccountID: accountID, Raw: t.Raw, Cast: t.Cast,
		}))
	case "policies.id":
		ids, err = collect(queries.SearchPoliciesByID(ctx, db.SearchPoliciesByIDParams{
			AccountID: accountID, ID: id, Like: like,
		}))
	case "policies.name":
		ids, err = collect(queries.SearchPoliciesByName(ctx, db.SearchPoliciesByNameParams{
			AccountID: accountID, Like: like,
		}))
	case "policies.metadata":
		ids, err = collect(queries.SearchPoliciesByMetadata(ctx, db.SearchPoliciesByMetadataParams{
			AccountID: accountID, Raw: t.Raw, Cast: t.Cast,
		}))
	case "policies.product":
		ids, err = collect(queries.SearchPoliciesByProduct(ctx, db.SearchPoliciesByProductParams{
			AccountID: accountID, ID: id, Like: &like, Prefix: t.Prefix, Tsv: t.TSV,
		}))
	case "groups.name":
		ids, err = collect(queries.SearchGroupsByName(ctx, db.SearchGroupsByNameParams{
			AccountID: accountID, Like: &like,
		}))
	case "entitlements.id":
		ids, err = collect(queries.SearchEntitlementsByID(ctx, db.SearchEntitlementsByIDParams{
			AccountID: accountID, ID: id, Like: like,
		}))
	case "entitlements.code":
		ids, err = collect(queries.SearchEntitlementsByCode(ctx, db.SearchEntitlementsByCodeParams{
			AccountID: accountID, Like: like,
		}))
	case "entitlements.name":
		ids, err = collect(queries.SearchEntitlementsByName(ctx, db.SearchEntitlementsByNameParams{
			AccountID: accountID, Like: like,
		}))
	default:
		return nil, fmt.Errorf("postgres: search %s.%s: %w", resource, attribute, errUnsearchable)
	}

	if err != nil {
		return nil, fmt.Errorf("postgres: search %s.%s: %w", resource, attribute, err)
	}

	return ids, nil
}

// errUnsearchable is an attribute the search scenario never routes here.
var errUnsearchable = errors.New("attribute is not searchable")

// collect reads the ids of a search query; every one answers bare ids.
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
