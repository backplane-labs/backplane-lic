// Package keys is the key scenario: the members of pooled policies'
// key pools (create requires a pooled policy; the pool itself lives
// under /policies/{id}/pool).
package keys

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// ErrTaken is the unique-index violation of (account, key).
var ErrTaken = errors.New("keys: key taken")

// ErrPoolEmpty: the pool pop found nothing to take.
var ErrPoolEmpty = errors.New("keys: pool empty")

// Store is the scenario's consumer-side storage port.
type Store interface {
	// KeyByID loads a key (with its policy's product); ErrNotFound.
	KeyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Key, error)
	// InsertKey writes a new row; ErrTaken on a collision.
	InsertKey(ctx context.Context, key domain.Key) error
	// UpdateKey changes the key value.
	UpdateKey(ctx context.Context, accountID, id uuid.UUID, key string) error
	// DeleteKey destroys the row; false when gone.
	DeleteKey(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	// ListKeys pages the account's keys with the policy/product filters.
	ListKeys(
		ctx context.Context, accountID uuid.UUID,
		filterPolicyID, filterProductID *uuid.UUID, page paging.Page,
	) ([]domain.Key, error)
	// CountKeys counts the same scope.
	CountKeys(
		ctx context.Context, accountID uuid.UUID, filterPolicyID, filterProductID *uuid.UUID,
	) (int, error)
	// KeyIsLicenseID answers a collision with a license's identifier.
	KeyIsLicenseID(ctx context.Context, accountID uuid.UUID, key string) (bool, error)
	// KeyIsLicenseKey answers a collision with a license's key.
	KeyIsLicenseKey(ctx context.Context, accountID uuid.UUID, key string) (bool, error)
}

// PolicyStore is the policy seam (the policies repository).
type PolicyStore interface {
	// PolicyByID loads a policy (the create target and the policy
	// relationship); policies.ErrNotFound.
	PolicyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Policy, error)
}

// ProductStore is the product seam (the products repository).
type ProductStore interface {
	// ProductByID loads a product (the key relationship); products.ErrNotFound.
	ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error)
}

// Input is the request body of create and update (decoded and
// shape-validated by the handler).
type Input struct {
	Key      string
	KeySet   bool
	PolicyID uuid.UUID
}

// Scenario runs the key reads and writes.
type Scenario struct {
	store    Store
	policies PolicyStore
	products ProductStore
	events   events.Publisher
	clock    func() time.Time
}

// New builds the scenario over the keys, policies and products
// repositories.
func New(
	store Store, policyStore PolicyStore, productStore ProductStore,
	publisher events.Publisher, clock func() time.Time,
) Scenario {
	return Scenario{store: store, policies: policyStore, products: productStore, events: publisher, clock: clock}
}

// List is one index page.
type List struct {
	Items []domain.Key
	Count int
	Next  uuid.NullUUID
}

// Index answers the keys page (the product bearer's post-check
// narrows it to its own).
func (s Scenario) Index(
	ctx context.Context, account domain.Account, filterPolicyID, filterProductID *uuid.UUID, params paging.Params,
) (List, error) {
	if err := access.Require(ctx, "key.read"); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	if principal.Role == domain.RoleUser || principal.Role == domain.RoleLicense {
		// The key scope is empty for user and license bearers; the
		// index post-check would deny anyway.
		return emptyList()
	}

	window := params.Window()
	filterProduct := filterProductID
	if principal.Role == domain.RoleProduct { //nolint:wsl_v5 // dense by design
		id := principal.BearerID
		filterProduct = &id
	}

	items, err := s.store.ListKeys(ctx, account.ID, filterPolicyID, filterProduct, window)
	if err != nil {
		return List{}, fmt.Errorf("keys: index: %w", err)
	}

	if err := access.KeyIndexPostCheck(principal, items); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	out := List{Items: items, Count: len(items)}

	switch params.Mode { //nolint:exhaustive // the default branch covers the rest
	case paging.ModeOffset:
		count, err := s.store.CountKeys(ctx, account.ID, filterPolicyID, filterProduct)
		if err != nil {
			return List{}, fmt.Errorf("keys: index count: %w", err)
		}

		out.Count = count
	case paging.ModeCursor:
		if len(items) > int(window.Limit) {
			out.Items = items[:window.Limit]
			out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].ID, Valid: true}
		}
	}

	return out, nil
}

// Show answers one key by id.
// Resolve loads the key within scope (the before_action's scoped
// find; a miss is the 404 that precedes the typed-params decode and
// the authorization).
func (s Scenario) Resolve(ctx context.Context, account domain.Account, id uuid.UUID) (domain.Key, error) {
	return s.load(ctx, account.ID, id)
}

func (s Scenario) Show(ctx context.Context, account domain.Account, id uuid.UUID) (domain.Key, error) {
	key, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Key{}, err
	}

	if err := access.Require(ctx, "key.read"); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.KeyShow(access.PrincipalOf(ctx), key); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	return key, nil
}

// Create writes a new pooled key.
func (s Scenario) Create(ctx context.Context, account domain.Account, in Input) (domain.Key, error) {
	if err := access.Require(ctx, "key.create"); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	policy, err := s.policies.PolicyByID(ctx, account.ID, in.PolicyID)
	if err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the policy lookup's own error
	}

	key := domain.Key{
		ID:        uuid.New(),
		AccountID: account.ID,
		PolicyID:  policy.ID,
		ProductID: policy.ProductID,
		Key:       in.Key,
	}

	if err := access.KeyWrite(access.PrincipalOf(ctx), key); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.validate(ctx, account.ID, key, policy); err != nil {
		return domain.Key{}, err
	}

	now := s.clock()
	key.CreatedAt = now
	key.UpdatedAt = now

	if err := s.store.InsertKey(ctx, key); err != nil {
		return domain.Key{}, mapWrite(err)
	}

	s.publish(ctx, "key.created", key)

	return key, nil
}

// Update changes the key value (the policy is immutable).
func (s Scenario) Update(ctx context.Context, account domain.Account, id uuid.UUID, in Input) (domain.Key, error) {
	key, err := s.load(ctx, account.ID, id)
	if err != nil {
		return domain.Key{}, err
	}

	if err := access.Require(ctx, "key.update"); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	if err := access.KeyWrite(principal, key); err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the authorization's own error
	}

	if in.KeySet {
		key.Key = in.Key
	}

	policy, err := s.policies.PolicyByID(ctx, account.ID, key.PolicyID)
	if err != nil {
		return domain.Key{}, err //nolint:wrapcheck // the policy lookup's own error
	}

	if err := s.validate(ctx, account.ID, key, policy); err != nil {
		return domain.Key{}, err
	}

	key.UpdatedAt = s.clock()

	if err := s.store.UpdateKey(ctx, account.ID, id, key.Key); err != nil {
		return domain.Key{}, mapWrite(err)
	}

	s.publish(ctx, "key.updated", key)

	return key, nil
}

// Destroy removes the key from the pool.
func (s Scenario) Destroy(ctx context.Context, account domain.Account, id uuid.UUID) error {
	key, err := s.load(ctx, account.ID, id)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "key.delete"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.KeyWrite(access.PrincipalOf(ctx), key); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if _, err := s.store.DeleteKey(ctx, account.ID, id); err != nil {
		return fmt.Errorf("keys: destroy: %w", err)
	}

	s.publish(ctx, "key.deleted", key)

	return nil
}

// validate runs the model validations (Key's rules).
func (s Scenario) validate(ctx context.Context, accountID uuid.UUID, key domain.Key, policy domain.Policy) error {
	var errs []*apperr.T

	attr := func(code, detail string) *apperr.T {
		return apperr.Validation(code, detail, "/data/attributes/key", "keys/#keys-object-attrs-key")
	}

	if key.Key == "" {
		errs = append(errs, attr("KEY_MISSING", "can't be blank"))
	}

	if reserved(domain.ReservedKeys, key.Key) {
		errs = append(errs, attr("KEY_NOT_ALLOWED", "is reserved"))
	}

	switch {
	case key.Key != "" && len(key.Key) < 8:
		errs = append(errs, attr("KEY_TOO_SHORT", "is too short (minimum is 8 characters)"))
	case len(key.Key) > 1024: //nolint:mnd // the reference's literal bound
		errs = append(errs, attr("KEY_TOO_LONG", "is too long (maximum is 1024 characters)"))
	}

	if isLicenseID, err := s.store.KeyIsLicenseID(ctx, accountID, key.Key); err == nil && isLicenseID {
		errs = append(errs, attr("KEY_CONFLICT", "must not conflict with another license's identifier (UUID)"))
	}

	if isLicenseKey, err := s.store.KeyIsLicenseKey(ctx, accountID, key.Key); err == nil && isLicenseKey {
		errs = append(errs, attr("KEY_CONFLICT", "is already being used as a license's key"))
	}

	if !policy.UsePool {
		errs = append(errs, apperr.Validation("POLICY_NOT_SUPPORTED", "cannot be added to an unpooled policy",
			"/data/relationships/policy", "keys/#keys-object-relationships-policy"))
	}

	if err := apperr.MultiOf(errs...); err != nil {
		return err //nolint:wrapcheck // the document itself
	}

	return nil
}

// mapWrite renders the write's index violation as the KEY_TAKEN 422.
func mapWrite(err error) error {
	if errors.Is(err, ErrTaken) {
		return apperr.Validation("KEY_TAKEN", "has already been taken",
			"/data/attributes/key", "keys/#keys-object-attrs-key")
	}

	return fmt.Errorf("keys: write: %w", err)
}

func (s Scenario) publish(ctx context.Context, event string, key domain.Key) {
	_ = s.events.Publish(ctx, event, key)
}

func emptyList() (List, error) {
	return List{Items: []domain.Key{}, Count: 0}, nil
}

func reserved(list []string, value string) bool {
	for _, r := range list {
		if r == value {
			return true
		}
	}

	return false
}
