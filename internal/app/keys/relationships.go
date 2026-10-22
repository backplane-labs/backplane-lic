package keys

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// Product answers the key's product (through its policy).
func (s Scenario) Product(
	ctx context.Context, account domain.Account, id uuid.UUID,
) (domain.Key, domain.Product, error) {
	key, product, err := s.relationship(ctx, account, id, "product.read")
	if err != nil {
		return domain.Key{}, domain.Product{}, err
	}

	_ = product

	product, err = s.products.ProductByID(ctx, account.ID, key.ProductID)
	if err != nil {
		return domain.Key{}, domain.Product{}, err //nolint:wrapcheck // the product lookup's own error
	}

	return key, product, nil
}

// Policy answers the key's policy.
func (s Scenario) Policy(
	ctx context.Context, account domain.Account, id uuid.UUID,
) (domain.Key, domain.Policy, error) {
	key, _, err := s.relationship(ctx, account, id, "policy.read")
	if err != nil {
		return domain.Key{}, domain.Policy{}, err
	}

	policy, err := s.policies.PolicyByID(ctx, account.ID, key.PolicyID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Key{}, domain.Policy{}, apperr.NotFound("policy", key.PolicyID.String())
	}

	if err != nil {
		return domain.Key{}, domain.Policy{}, fmt.Errorf("keys: policy: %w", err)
	}

	return key, policy, nil
}

// relationship loads the key within scope and runs the keys/*
// relationship guard.
func (s Scenario) relationship(
	ctx context.Context, account domain.Account, id uuid.UUID, permission string,
) (domain.Key, domain.Product, error) { //nolint:unparam // kept symmetric with the port
	if err := access.Require(ctx, permission); err != nil {
		return domain.Key{}, domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	key, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Key{}, domain.Product{}, err
	}

	if err := access.KeyRelationship(access.PrincipalOf(ctx), key); err != nil {
		return domain.Key{}, domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	return key, domain.Product{}, nil
}

// load fetches the key within scope (user and license bearers see
// nothing: their scope is empty, the 404 of the scoped find).
func (s Scenario) load(ctx context.Context, accountID, id uuid.UUID) (domain.Key, error) {
	principal := access.PrincipalOf(ctx)

	if principal.Role == domain.RoleUser || principal.Role == domain.RoleLicense {
		return domain.Key{}, apperr.NotFound("key", id.String())
	}

	key, err := s.store.KeyByID(ctx, accountID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.Key{}, apperr.NotFound("key", id.String())
	}

	if err != nil {
		return domain.Key{}, fmt.Errorf("keys: load: %w", err)
	}

	if principal.Role == domain.RoleProduct && key.ProductID != principal.BearerID {
		return domain.Key{}, apperr.NotFound("key", id.String())
	}

	return key, nil
}
