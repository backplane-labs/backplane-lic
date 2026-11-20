package catalog_test

import (
	"context"
	"crypto/rand"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/catalog"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

const (
	valueA = "prod-" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "v3"
	valueB = "prod-" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" + "v3"
)

func spec(productID, policyID uuid.UUID, tokens ...catalog.Token) catalog.Spec {
	return catalog.Spec{Products: []catalog.Product{{
		Input:    products.Input{ID: productID},
		Policies: []policies.Input{{ID: policyID}},
		Tokens:   tokens,
	}}}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	product, policy := uuid.New(), uuid.New()

	for _, tc := range []struct {
		name string
		spec catalog.Spec
		want string
	}{
		{"valid", spec(product, policy, catalog.Token{Name: "relay", Value: valueA, Permissions: []string{"machine.read"}}), ""},
		{"wildcard", spec(product, policy, catalog.Token{Name: "licensing", Value: valueA, Permissions: []string{"*"}}), ""},
		{"no product id", spec(uuid.Nil, policy), "product id"},
		{"policy id repeats the product's", spec(product, product), "policy id"},
		{"no policy id", spec(product, uuid.Nil), "policy id"},
		{"no token name", spec(product, policy, catalog.Token{Value: valueA}), "token name"},
		{
			"repeated token name",
			spec(product, policy, catalog.Token{Name: "a", Value: valueA}, catalog.Token{Name: "a", Value: valueB}),
			"token name",
		},
		{"admin token value", spec(product, policy, catalog.Token{Name: "a", Value: "admin" + valueA[4:]}), "prod-"},
		{"short value", spec(product, policy, catalog.Token{Name: "a", Value: valueA[:20] + "v3"}), "prod-"},
		{"uppercase hex", spec(product, policy, catalog.Token{Name: "a", Value: strings.ToUpper(valueA[:5]) + valueA[5:]}), "prod-"},
		{
			"permission outside the product set",
			spec(product, policy, catalog.Token{Name: "a", Value: valueA, Permissions: []string{"account.update"}}),
			"not a product permission",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.spec.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate = %v", err)
				}

				return
			}

			if !errors.Is(err, catalog.ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want ErrInvalid mentioning %q", err, tc.want)
			}
		})
	}
}

// fakeStore keeps the token rows and the ids; the lock serializes like
// the advisory lock does.
type fakeStore struct {
	mu       sync.Mutex
	products []uuid.UUID
	policies []uuid.UUID
	tokens   []domain.Token
	locked   int
	fail     error
}

func (f *fakeStore) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return fn(ctx)
}

func (f *fakeStore) LockCatalog(context.Context, uuid.UUID) error {
	f.locked++

	return f.fail
}

func (f *fakeStore) ProductIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return f.products, nil
}

func (f *fakeStore) PolicyIDs(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return f.policies, nil
}

func (f *fakeStore) ProductTokens(context.Context, uuid.UUID) ([]domain.Token, error) {
	return slices.Clone(f.tokens), nil
}

func (f *fakeStore) InsertToken(_ context.Context, token domain.Token) error {
	f.tokens = append(f.tokens, token)

	return nil
}

func (f *fakeStore) SetDeclaredToken(_ context.Context, _, id uuid.UUID, digest string, permissions []string) error {
	for i := range f.tokens {
		if f.tokens[i].ID == id {
			f.tokens[i].Digest, f.tokens[i].Permissions = digest, permissions
		}
	}

	return nil
}

// fakeDeclarer records the declared ids and creates each once.
type fakeDeclarer[In any] struct {
	id   func(In) uuid.UUID
	ids  *[]uuid.UUID
	fail error
}

func (f fakeDeclarer[In]) Declare(_ context.Context, _ domain.Account, in In) (bool, error) {
	if f.fail != nil {
		return false, f.fail
	}

	if slices.Contains(*f.ids, f.id(in)) {
		return false, nil
	}

	*f.ids = append(*f.ids, f.id(in))

	return true, nil
}

type recorder struct{ events []string }

func (r *recorder) Publish(_ context.Context, event string, _ any) error {
	r.events = append(r.events, event)

	return nil
}

func reconciler(store *fakeStore, publisher *recorder, fail error) catalog.Reconciler {
	return catalog.New(store,
		fakeDeclarer[products.Input]{id: func(in products.Input) uuid.UUID { return in.ID }, ids: &store.products, fail: fail},
		fakeDeclarer[policies.Input]{id: func(in policies.Input) uuid.UUID { return in.ID }, ids: &store.policies},
		publisher)
}

func account(t *testing.T) domain.Account {
	t.Helper()

	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return domain.Account{ID: uuid.New(), Keys: keys}
}

// The first reconcile creates everything, a repeat writes nothing, a
// changed value rotates the same token, and an extra object is reported
// and kept.
func TestReconcile(t *testing.T) {
	t.Parallel()

	acc := account(t)
	product, policy := uuid.New(), uuid.New()
	store, publisher := &fakeStore{}, &recorder{}
	r := reconciler(store, publisher, nil)
	relay := catalog.Token{Name: "relay", Value: valueA, Permissions: []string{"machine.read"}}

	report, err := r.Reconcile(t.Context(), acc, spec(product, policy, relay))
	if err != nil || report.Written != 3 || !report.TokensMoved || len(report.Undeclared) != 0 {
		t.Fatalf("first = %+v, %v", report, err)
	}

	first := store.tokens[0]
	if first.Digest != crypto.DigestV3(acc.Keys.SecretKey, valueA) || first.BearerID != product ||
		!slices.Equal(first.Permissions, []string{"machine.read"}) {
		t.Fatalf("token = %+v", first)
	}

	report, err = r.Reconcile(t.Context(), acc, spec(product, policy, relay))
	if err != nil || report.Written != 0 || report.TokensMoved {
		t.Fatalf("repeat = %+v, %v", report, err)
	}

	relay.Value = valueB

	report, err = r.Reconcile(t.Context(), acc, spec(product, policy, relay))
	if err != nil || report.Written != 1 || !report.TokensMoved || len(store.tokens) != 1 ||
		store.tokens[0].ID != first.ID || store.tokens[0].Digest != crypto.DigestV3(acc.Keys.SecretKey, valueB) {
		t.Fatalf("rotate = %+v, %v, tokens %+v", report, err, store.tokens)
	}

	extra := uuid.New()
	store.products = append(store.products, extra)

	report, err = r.Reconcile(t.Context(), acc, spec(product, policy))
	if err != nil || report.Undeclared["product"] != 1 || report.Undeclared["token"] != 1 ||
		report.Undeclared["policy"] != 0 || len(store.tokens) != 1 {
		t.Fatalf("undeclared = %+v, %v", report, err)
	}

	if !slices.Equal(publisher.events, []string{"token.generated", "token.regenerated"}) {
		t.Fatalf("events = %v", publisher.events)
	}
}

func TestReconcileRefusals(t *testing.T) {
	t.Parallel()

	boom := errors.New("boom")

	for _, tc := range []struct {
		name     string
		spec     catalog.Spec
		lock     error
		declare  error
		want     error
		wantLock bool
	}{
		{"empty spec writes nothing", catalog.Spec{}, nil, nil, nil, false},
		{"invalid spec", spec(uuid.Nil, uuid.New()), nil, nil, catalog.ErrInvalid, false},
		{"lock fails", spec(uuid.New(), uuid.New()), boom, nil, boom, true},
		{"declare fails", spec(uuid.New(), uuid.New()), nil, boom, boom, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{fail: tc.lock}

			_, err := reconciler(store, &recorder{}, tc.declare).Reconcile(t.Context(), account(t), tc.spec)
			if !errors.Is(err, tc.want) || (tc.want == nil) != (err == nil) {
				t.Fatalf("Reconcile = %v, want %v", err, tc.want)
			}

			if (store.locked > 0) != tc.wantLock {
				t.Fatalf("locked = %d", store.locked)
			}
		})
	}
}

func BenchmarkReconcileUnchanged(b *testing.B) {
	keys, err := crypto.GenerateAccountKeys(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}

	acc := domain.Account{ID: uuid.New(), Keys: keys}
	store := &fakeStore{}
	r := reconciler(store, &recorder{}, nil)
	declared := spec(uuid.New(), uuid.New(), catalog.Token{Name: "relay", Value: valueA})

	if _, err := r.Reconcile(b.Context(), acc, declared); err != nil {
		b.Fatal(err)
	}

	for b.Loop() {
		if _, err := r.Reconcile(b.Context(), acc, declared); err != nil {
			b.Fatal(err)
		}
	}
}
