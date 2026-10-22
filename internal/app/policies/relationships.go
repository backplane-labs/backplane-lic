package policies

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/entitlements"
	"github.com/backplane-labs/backplane-lic/internal/app/keys"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Product answers the policy's product.
func (s Scenario) Product(ctx context.Context, account domain.Account, id uuid.UUID) (domain.Product, error) {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Product{}, err
	}

	if err := access.Require(ctx, "product.read"); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyRelationship(access.PrincipalOf(ctx), policy); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	product, err := s.products.ProductByID(ctx, account.ID, policy.ProductID)
	if err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the product lookup's own error
	}

	return product, nil
}

// KeyList is one pool page.
type KeyList struct {
	Items []domain.Key
	Count int
	Next  uuid.NullUUID
}

// Pool answers the policy's key pool page.
func (s Scenario) Pool(ctx context.Context, account domain.Account, id uuid.UUID, params paging.Params) (KeyList, error) { //nolint:lll // the wire name is long
	if err := access.Require(ctx, "key.read"); err != nil {
		return KeyList{}, err //nolint:wrapcheck // the authorization's own error
	}

	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return KeyList{}, err
	}

	principal := access.PrincipalOf(ctx)

	if err := access.PolicyRelationship(principal, policy); err != nil {
		return KeyList{}, err //nolint:wrapcheck // the authorization's own error
	}

	page := params.Window()

	items, err := s.pool.ListPolicyPool(ctx, account.ID, policy.ID, page)
	if err != nil {
		return KeyList{}, fmt.Errorf("policies: pool: %w", err)
	}

	if err := access.PolicyPoolPostCheck(principal, policy, items); err != nil {
		return KeyList{}, err //nolint:wrapcheck // the authorization's own error
	}

	out := KeyList{Items: items, Count: len(items)}

	switch params.Mode { //nolint:exhaustive // the default branch covers the rest
	case paging.ModeOffset:
		count, err := s.pool.CountPolicyPool(ctx, account.ID, policy.ID)
		if err != nil {
			return KeyList{}, fmt.Errorf("policies: pool count: %w", err)
		}

		out.Count = count
	case paging.ModeCursor:
		if len(items) > int(page.Limit) {
			out.Items = items[:page.Limit]
			out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].ID, Valid: true}
		}
	}

	return out, nil
}

// PoolKey answers one pooled key.
func (s Scenario) PoolKey(
	ctx context.Context, account domain.Account, id, keyID uuid.UUID,
) (domain.Key, error) {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Key{}, err
	}

	if err := access.Require(ctx, "key.read"); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	if err := access.PolicyRelationship(principal, policy); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	whole := paging.Page{Limit: 1 << 30, Fetch: 1 << 30} //nolint:mnd // the whole pool

	pool, err := s.pool.ListPolicyPool(ctx, account.ID, policy.ID, whole)
	if err != nil {
		return domain.Key{}, fmt.Errorf("policies: pool key: %w", err)
	}

	if err := access.PolicyPoolPostCheck(principal, policy, pool); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	for _, key := range pool { //nolint:gocritic // mirrors the reference's shape
		if key.ID == keyID {
			return key, nil
		}
	}

	return domain.Key{}, apperr.NotFound("key", keyID.String())
}

// Pop takes the newest key off the pool (destroying its row).
func (s Scenario) Pop(ctx context.Context, account domain.Account, id uuid.UUID) (domain.Key, error) {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Key{}, err
	}

	if err := access.Require(ctx, "policy.pool.pop"); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyPoolPop(access.PrincipalOf(ctx), policy); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	if !policy.UsePool {
		return domain.Key{}, &apperr.T{
			Status: 422, //nolint:mnd // the reference's literal bound
			Title:  apperr.TitleUnprocessable,
			Detail: "pool is not supported",
			Source: apperr.Source{Pointer: "/data/relationships/pool"},
		}
	}

	key, err := s.pool.PopPolicyKey(ctx, account.ID, policy.ID)
	if errors.Is(err, keys.ErrPoolEmpty) {
		return domain.Key{}, &apperr.T{
			Status: 422, //nolint:mnd // the reference's literal bound
			Title:  apperr.TitleUnprocessable,
			Detail: "pool is empty",
			Source: apperr.Source{Pointer: "/data/relationships/pool"},
		}
	}

	if err != nil {
		return domain.Key{}, fmt.Errorf("policies: pop: %w", err)
	}

	key.ProductID = policy.ProductID

	_ = s.events.Publish(ctx, "policy.pool.popped", key)

	return key, nil
}

// Entitlements answers the policy's attached entitlements page.
func (s Scenario) Entitlements(
	ctx context.Context, account domain.Account, id uuid.UUID, params paging.Params,
) (EntitlementList, error) {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return EntitlementList{}, err
	}

	if err := access.Require(ctx, "entitlement.read"); err != nil {
		return EntitlementList{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyRelationship(access.PrincipalOf(ctx), policy); err != nil {
		return EntitlementList{}, err //nolint:wrapcheck // the authorization's own error
	}

	joins, err := s.entitlements.ListPolicyEntitlements(ctx, account.ID, policy.ID)
	if err != nil {
		return EntitlementList{}, fmt.Errorf("policies: entitlements: %w", err)
	}

	page := params.Window()
	start, end := pageBounds(len(joins), page)

	items := make([]domain.Entitlement, 0, max(end-start, 0))

	for _, join := range joins[start:end] {
		entitlement, err := s.entitlements.EntitlementByID(ctx, account.ID, join.EntitlementID)
		if err != nil {
			return EntitlementList{}, err //nolint:wrapcheck // the lookup's own error
		}

		items = append(items, entitlement)
	}

	out := EntitlementList{Items: items, Count: len(joins)}

	if params.Mode == paging.ModeCursor && len(items) > 0 && end < len(joins) {
		out.Next = uuid.NullUUID{UUID: joins[end-1].EntitlementID, Valid: true}
	}

	return out, nil
}

// Entitlement shows one of the policy's attached entitlements.
func (s Scenario) Entitlement(
	ctx context.Context, account domain.Account, id uuid.UUID, alias string,
) (domain.Entitlement, error) {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Entitlement{}, err
	}

	if err := access.Require(ctx, "entitlement.read"); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyRelationship(access.PrincipalOf(ctx), policy); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	resolve := func() (uuid.UUID, error) {
		if entitlementID, err := uuid.Parse(alias); err == nil {
			return entitlementID, nil
		}

		entitlement, err := s.entitlements.EntitlementByCode(ctx, account.ID, alias)
		if err != nil {
			return uuid.UUID{}, err //nolint:wrapcheck // the lookup's own error
		}

		return entitlement.ID, nil
	}

	entitlementID, err := resolve()
	if err != nil {
		return domain.Entitlement{}, apperr.NotFound("entitlement", alias)
	}

	if _, err := s.entitlements.PolicyEntitlement(ctx, account.ID, policy.ID, entitlementID); err != nil {
		return domain.Entitlement{}, apperr.NotFound("entitlement", alias)
	}

	return s.entitlements.EntitlementByID(ctx, account.ID, entitlementID) //nolint:wrapcheck // the lookup's own error
}

// EntitlementList is one policy-entitlements page.
type EntitlementList struct {
	Items []domain.Entitlement
	Count int
	Next  uuid.NullUUID
}

// Attach attaches entitlements to the policy; the answer carries the
// join rows (one policy-entitlements document per requested id).
func (s Scenario) Attach( //nolint:funlen // the attribute table
	ctx context.Context, account domain.Account, id uuid.UUID, entitlementIDs []uuid.UUID,
) ([]domain.PolicyEntitlement, error) {
	if err := access.Require(ctx, "policy.entitlements.attach"); err != nil {
		return nil, err //nolint:wrapcheck // the authorization's own error
	}

	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return nil, err
	}

	if err := access.PolicyEntitlementsWrite(access.PrincipalOf(ctx), policy); err != nil {
		return nil, err //nolint:wrapcheck // the authorization's own error
	}

	var errs []*apperr.T

	entitlementsOK := make([]domain.Entitlement, 0, len(entitlementIDs))

	for _, entitlementID := range entitlementIDs {
		entitlement, err := s.entitlements.EntitlementByID(ctx, account.ID, entitlementID)
		if errors.Is(err, entitlements.ErrNotFound) {
			errs = append(errs, apperr.Validation("ENTITLEMENT_NOT_FOUND", "must exist",
				"/data/relationships/entitlement", "entitlements/#entitlements-object-relationships-entitlement"))

			continue
		}

		if err != nil {
			return nil, fmt.Errorf("policies: attach: %w", err)
		}

		entitlementsOK = append(entitlementsOK, entitlement)
	}

	if err := apperr.MultiOf(errs...); err != nil {
		return nil, err //nolint:wrapcheck // the document itself
	}

	now := s.clock()

	for _, entitlement := range entitlementsOK {
		if err := s.entitlements.InsertPolicyEntitlements(
			ctx, account.ID, policy.ID, uuid.New(), []uuid.UUID{entitlement.ID},
		); err != nil {
			if errors.Is(err, ErrEntitlementTaken) {
				return nil, apperr.Validation("ENTITLEMENT_TAKEN", "already exists",
					"/data/relationships/entitlement", "entitlements/#entitlements-object-relationships-entitlement")
			}

			return nil, fmt.Errorf("policies: attach: %w", err)
		}

		join := domain.PolicyEntitlement{
			ID:            uuid.Nil, // rendered from the store below
			AccountID:     account.ID,
			PolicyID:      policy.ID,
			EntitlementID: entitlement.ID,
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		_ = s.events.Publish(ctx, "policy.entitlements.attached", join)
	}

	joins, err := s.entitlements.ListPolicyEntitlements(ctx, account.ID, policy.ID)
	if err != nil {
		return nil, fmt.Errorf("policies: attach: %w", err)
	}

	byEntitlement := make(map[uuid.UUID]domain.PolicyEntitlement, len(joins))
	for _, join := range joins {
		byEntitlement[join.EntitlementID] = join
	}

	out := make([]domain.PolicyEntitlement, 0, len(entitlementsOK))
	for _, entitlement := range entitlementsOK {
		if join, ok := byEntitlement[entitlement.ID]; ok {
			out = append(out, join)
		}
	}

	return out, nil
}

// Detach detaches entitlements from the policy (204 on success).
func (s Scenario) Detach(
	ctx context.Context, account domain.Account, id uuid.UUID, entitlementIDs []uuid.UUID,
) error {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "policy.entitlements.detach"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyEntitlementsWrite(access.PrincipalOf(ctx), policy); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	for index, entitlementID := range entitlementIDs {
		attached, err := s.isAttached(ctx, account.ID, policy.ID, entitlementID)
		if err != nil {
			return err
		}

		if !attached {
			return &apperr.T{
				Status: 422, //nolint:mnd // the reference's literal bound
				Title:  apperr.TitleUnprocessable,
				Detail: fmt.Sprintf("cannot detach entitlement '%s' (entitlement is not attached)", entitlementID),
				Source: apperr.Source{Pointer: fmt.Sprintf("/data/%d", index)},
			}
		}
	}

	if _, err := s.entitlements.DeletePolicyEntitlements(ctx, account.ID, policy.ID, entitlementIDs); err != nil {
		return fmt.Errorf("policies: detach: %w", err)
	}

	for _, entitlementID := range entitlementIDs {
		_ = s.events.Publish(ctx, "policy.entitlements.detached", domain.PolicyEntitlement{
			AccountID:     account.ID,
			PolicyID:      policy.ID,
			EntitlementID: entitlementID,
		})
	}

	return nil
}

func (s Scenario) isAttached(
	ctx context.Context, accountID, policyID, entitlementID uuid.UUID,
) (bool, error) {
	joins, err := s.entitlements.ListPolicyEntitlements(ctx, accountID, policyID)
	if err != nil {
		return false, fmt.Errorf("policies: detach: %w", err)
	}

	for _, join := range joins {
		if join.EntitlementID == entitlementID {
			return true, nil
		}
	}

	return false, nil
}

func pageBounds(total int, page paging.Page) (int, int) {
	start := int(page.Offset)
	if start > total {
		start = total
	}

	end := start + int(page.Limit)
	if end > total {
		end = total
	}

	return start, end
}

// RequireRelationship loads the scoped policy and runs the
// relationship guard (permission plus the policies/*_policy.rb role
// checks) for its sub-resources.
func (s Scenario) RequireRelationship(
	ctx context.Context, account domain.Account, id uuid.UUID, permission string,
) (domain.Policy, error) {
	policy, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Policy{}, err
	}

	if err := access.Require(ctx, permission); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.PolicyRelationship(access.PrincipalOf(ctx), policy); err != nil {
		return domain.Policy{}, err //nolint:wrapcheck // the authorization's own error
	}

	return policy, nil
}
