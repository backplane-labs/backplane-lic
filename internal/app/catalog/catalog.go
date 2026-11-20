// Package catalog is the declared catalog: products, their policies and
// named product tokens of fixed value, taken from the configuration and
// reconciled at startup. Declared objects are created when missing and
// updated to match; a token whose value changed is rotated; an object the
// configuration does not declare is reported, never deleted.
package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// Spec is the declared catalog.
type Spec struct {
	Products []Product
}

// Product is a declared product: its attributes (Input.ID set), its
// policies and its named tokens.
type Product struct {
	Input    products.Input
	Policies []policies.Input
	Tokens   []Token
}

// Token is a named product token of fixed value.
type Token struct {
	Name string
	// Value is the raw secret: "prod-" + 64 hex digits + "v3".
	Value string
	// Permissions narrow the product's set; empty or "*" keeps all of it.
	Permissions []string
}

// ErrInvalid: the catalog cannot be declared as configured.
var ErrInvalid = errors.New("catalog: invalid declaration")

// tokenValue is the v3 product secret GenerateTokenSecret mints.
var tokenValue = regexp.MustCompile(`^prod-[0-9a-f]{64}v3$`)

// Validate checks what the scenarios do not: unique ids and token names,
// the token value's form and permissions the product role holds.
func (s Spec) Validate() error {
	ids := map[uuid.UUID]bool{}
	product := access.RolePermissions(domain.RoleProduct)

	for i := range s.Products {
		p := &s.Products[i]
		if p.Input.ID == uuid.Nil || ids[p.Input.ID] {
			return fmt.Errorf("%w: product id %q is empty or repeated", ErrInvalid, p.Input.ID)
		}

		ids[p.Input.ID] = true

		for j := range p.Policies {
			id := p.Policies[j].ID
			if id == uuid.Nil || ids[id] {
				return fmt.Errorf("%w: policy id %q is empty or repeated", ErrInvalid, id)
			}

			ids[id] = true
		}

		names := map[string]bool{}
		for _, token := range p.Tokens {
			if token.Name == "" || names[token.Name] {
				return fmt.Errorf("%w: product %s: token name %q is empty or repeated", ErrInvalid, p.Input.ID, token.Name)
			}

			names[token.Name] = true

			if !tokenValue.MatchString(token.Value) {
				return fmt.Errorf("%w: token %q: the value is not prod-<64 hex>v3", ErrInvalid, token.Name)
			}

			for _, perm := range token.Permissions {
				if _, ok := product[perm]; !ok && perm != "*" {
					return fmt.Errorf("%w: token %q: %q is not a product permission", ErrInvalid, token.Name, perm)
				}
			}
		}
	}

	return nil
}

// Store is the reconcile's storage port.
type Store interface {
	// Do runs fn in one transaction.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
	// LockCatalog serializes reconciles across replicas until the
	// transaction ends.
	LockCatalog(ctx context.Context, accountID uuid.UUID) error
	// ProductIDs and PolicyIDs list every row of the account.
	ProductIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error)
	PolicyIDs(ctx context.Context, accountID uuid.UUID) ([]uuid.UUID, error)
	// ProductTokens lists every product token of the account, oldest first.
	ProductTokens(ctx context.Context, accountID uuid.UUID) ([]domain.Token, error)
	// InsertToken writes a new token row.
	InsertToken(ctx context.Context, token domain.Token) error
	// SetDeclaredToken writes a token's digest and permissions.
	SetDeclaredToken(ctx context.Context, accountID, id uuid.UUID, digest string, permissions []string) error
}

// Declarer is a scenario's create-or-update of one declared object.
type Declarer[In any] interface {
	Declare(ctx context.Context, account domain.Account, in In) (bool, error)
}

// Report is what one reconcile did and what it found undeclared.
type Report struct {
	Written     int
	TokensMoved bool
	// Declared are the catalog's objects as stored.
	Declared []Object
	// Undeclared counts what the catalog does not declare, by kind.
	Undeclared map[string]int64
}

// Object is one declared object: kind product, policy or token.
type Object struct {
	Kind        string
	ID          uuid.UUID
	Permissions []string
}

// Reconciler applies a spec to the account.
type Reconciler struct {
	store    Store
	products Declarer[products.Input]
	policies Declarer[policies.Input]
	events   events.Publisher
}

// New builds the reconciler over the scenarios that own the objects.
func New(
	store Store, productScenario Declarer[products.Input], policyScenario Declarer[policies.Input],
	publisher events.Publisher,
) Reconciler {
	return Reconciler{store: store, products: productScenario, policies: policyScenario, events: publisher}
}

// Reconcile applies the spec in one transaction under the catalog lock,
// so replicas starting together apply it once and the others find
// nothing to write. An empty spec changes and reports nothing.
func (r Reconciler) Reconcile(ctx context.Context, account domain.Account, spec Spec) (Report, error) {
	if len(spec.Products) == 0 {
		return Report{}, nil
	}

	if err := spec.Validate(); err != nil {
		return Report{}, err
	}

	ctx = access.With(ctx, access.System())

	var report Report

	err := r.store.Do(ctx, func(ctx context.Context) error {
		report = Report{}

		if err := r.store.LockCatalog(ctx, account.ID); err != nil {
			return fmt.Errorf("catalog: lock: %w", err)
		}

		tokens, err := r.store.ProductTokens(ctx, account.ID)
		if err != nil {
			return fmt.Errorf("catalog: tokens: %w", err)
		}

		for i := range spec.Products {
			if err := r.product(ctx, account, &spec.Products[i], tokens, &report); err != nil {
				return err
			}
		}

		return r.undeclared(ctx, account.ID, spec, tokens, &report)
	})

	return report, err
}

func (r Reconciler) product(
	ctx context.Context, account domain.Account, product *Product, tokens []domain.Token, report *Report,
) error {
	written, err := r.products.Declare(ctx, account, product.Input)
	if err != nil {
		return fmt.Errorf("catalog: product %s: %w", product.Input.ID, err)
	}

	report.add(written)
	report.Declared = append(report.Declared, Object{Kind: "product", ID: product.Input.ID})

	for i := range product.Policies {
		policy := product.Policies[i]
		policy.ProductID = product.Input.ID

		changed, err := r.policies.Declare(ctx, account, policy)
		if err != nil {
			return fmt.Errorf("catalog: policy %s: %w", policy.ID, err)
		}

		report.add(changed)
		report.Declared = append(report.Declared, Object{Kind: "policy", ID: policy.ID})
	}

	for _, token := range product.Tokens {
		if err := r.token(ctx, account, product.Input.ID, token, tokens, report); err != nil {
			return err
		}
	}

	return nil
}

// token creates the named token or rotates it to the declared value and
// permissions; the oldest token of that name on the product is the
// declared one.
func (r Reconciler) token(
	ctx context.Context, account domain.Account, productID uuid.UUID, declared Token, tokens []domain.Token,
	report *Report,
) error {
	digest := crypto.DigestV3(account.Keys.SecretKey, declared.Value)

	permissions := declared.Permissions
	if len(permissions) == 0 {
		permissions = []string{"*"}
	}

	if i := find(tokens, productID, declared.Name); i >= 0 {
		existing := tokens[i]
		report.Declared = append(report.Declared, Object{Kind: "token", ID: existing.ID, Permissions: permissions})

		if existing.Digest == digest && slices.Equal(existing.Permissions, permissions) {
			return nil
		}

		if err := r.store.SetDeclaredToken(ctx, account.ID, existing.ID, digest, permissions); err != nil {
			return fmt.Errorf("catalog: token %q: %w", declared.Name, err)
		}

		existing.Digest, existing.Permissions = digest, permissions

		report.add(true)
		report.TokensMoved = true

		return r.publish(ctx, "token.regenerated", existing)
	}

	name := declared.Name
	token := domain.Token{
		ID: uuid.New(), AccountID: account.ID, Digest: digest,
		BearerID: productID, BearerType: "Product", BearerRole: domain.RoleProduct,
		Kind: crypto.TokenKind(string(domain.RoleProduct)), Name: &name, Permissions: permissions,
	}

	if err := r.store.InsertToken(ctx, token); err != nil {
		return fmt.Errorf("catalog: token %q: %w", declared.Name, err)
	}

	report.Declared = append(report.Declared, Object{Kind: "token", ID: token.ID, Permissions: permissions})

	report.add(true)
	report.TokensMoved = true

	return r.publish(ctx, "token.generated", token)
}

func (r Reconciler) publish(ctx context.Context, event string, token domain.Token) error {
	if err := r.events.Publish(ctx, event, token); err != nil {
		return fmt.Errorf("catalog: %s: %w", event, err)
	}

	return nil
}

// find answers the index of the oldest token of that name on the
// product; -1 when none.
func find(tokens []domain.Token, productID uuid.UUID, name string) int {
	for i := range tokens {
		if named(&tokens[i], productID, name) {
			return i
		}
	}

	return -1
}

func named(t *domain.Token, productID uuid.UUID, name string) bool {
	return t.BearerID == productID && t.Name != nil && *t.Name == name
}

// undeclared lists the products, policies and product tokens the spec
// does not declare.
func (r Reconciler) undeclared(
	ctx context.Context, accountID uuid.UUID, spec Spec, tokens []domain.Token, report *Report,
) error {
	declared := map[uuid.UUID]bool{}

	for i := range spec.Products {
		p := &spec.Products[i]
		declared[p.Input.ID] = true

		for j := range p.Policies {
			declared[p.Policies[j].ID] = true
		}
	}

	productIDs, err := r.store.ProductIDs(ctx, accountID)
	if err != nil {
		return fmt.Errorf("catalog: products: %w", err)
	}

	policyIDs, err := r.store.PolicyIDs(ctx, accountID)
	if err != nil {
		return fmt.Errorf("catalog: policies: %w", err)
	}

	report.Undeclared = map[string]int64{}

	for kind, ids := range map[string][]uuid.UUID{"product": productIDs, "policy": policyIDs} {
		for _, id := range ids {
			if !declared[id] {
				report.Undeclared[kind]++
			}
		}
	}

	for i := range tokens {
		if !declaredToken(spec, &tokens[i]) {
			report.Undeclared["token"]++
		}
	}

	return nil
}

func declaredToken(spec Spec, t *domain.Token) bool {
	for i := range spec.Products {
		p := &spec.Products[i]
		for _, token := range p.Tokens {
			if named(t, p.Input.ID, token.Name) {
				return true
			}
		}
	}

	return false
}

func (r *Report) add(written bool) {
	if written {
		r.Written++
	}
}
