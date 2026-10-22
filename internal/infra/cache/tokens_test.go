package cache_test

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	valkeygo "github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/valkeyaside"

	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/cache"
)

// fakeStore counts token lookups and answers the token for its digest.
type fakeStore struct {
	authn.Store

	lookups atomic.Int64
	token   domain.Token
	digest  string
}

// fakeManage and fakeUsers accept every token mutation.
type fakeManage struct{ tokens.Store }

type fakeUsers struct{ users.Store }

func (f *fakeStore) TokenByDigest(_ context.Context, _ uuid.UUID, digest string) (domain.Token, error) {
	f.lookups.Add(1)

	if digest != f.digest {
		return domain.Token{}, authn.ErrNotFound
	}

	return f.token, nil
}

func (fakeManage) DeleteToken(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (fakeManage) RegenerateToken(context.Context, uuid.UUID, uuid.UUID, string, *time.Time) error {
	return nil
}

func (fakeUsers) DeleteUserTokens(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func aside(t *testing.T) valkeyaside.CacheAsideClient { //nolint:ireturn // the client library's interface
	t.Helper()

	addr := os.Getenv("LIC_TEST_VALKEY_ADDR")
	if addr == "" {
		t.Skip("set LIC_TEST_VALKEY_ADDR to exercise the token cache")
	}

	client, err := valkeyaside.NewClient(valkeyaside.ClientOption{ClientOption: valkeygo.ClientOption{InitAddress: []string{addr}}})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(client.Close)

	return client
}

// forget removes the account's generation key the test created.
func forget(t *testing.T, client valkeyaside.CacheAsideClient, accountID uuid.UUID) {
	t.Helper()

	t.Cleanup(func() {
		raw := client.Client()
		_ = raw.Do(context.Background(), raw.B().Del().Key("lic:token-generation:"+accountID.String()).Build()).Error()
	})
}

func TestTokenLookupsAreCachedUntilAMutation(t *testing.T) {
	t.Parallel()

	accountID := uuid.New()
	name := "cached"
	store := &fakeStore{digest: "digest-" + uuid.NewString(), token: domain.Token{ID: uuid.New(), Name: &name, BearerType: "Product"}}
	client := aside(t)
	forget(t, client, accountID)
	tokensCache := cache.NewTokens(client, time.Minute)
	reads, manage, people := tokensCache.Authn(store), tokensCache.Manage(fakeManage{}), tokensCache.Users(fakeUsers{})
	ctx := t.Context()

	lookup := func() domain.Token {
		t.Helper()

		token, err := reads.TokenByDigest(ctx, accountID, store.digest)
		if err != nil {
			t.Fatal(err)
		}

		return token
	}

	if got := lookup(); got.ID != store.token.ID || got.Name == nil || *got.Name != name {
		t.Fatalf("first lookup = %+v", got)
	}

	lookup()

	if n := store.lookups.Load(); n != 1 {
		t.Fatalf("store lookups after a repeat = %d, want 1 (cached)", n)
	}

	mutations := []func() error{
		func() error { return manage.DeleteToken(ctx, accountID, store.token.ID) },
		func() error { return manage.RegenerateToken(ctx, accountID, store.token.ID, "new", nil) },
		func() error { return people.DeleteUserTokens(ctx, accountID, uuid.New()) },
	}

	for i, mutate := range mutations {
		if err := mutate(); err != nil {
			t.Fatal(err)
		}

		lookup()

		if n := store.lookups.Load(); n != int64(i+2) {
			t.Fatalf("mutation %d: store lookups = %d, want %d (a new generation)", i, n, i+2)
		}
	}
}

func TestUnknownTokensAreNotCached(t *testing.T) {
	t.Parallel()

	store := &fakeStore{digest: "known-" + uuid.NewString()}
	reads := cache.NewTokens(aside(t), time.Minute).Authn(store)
	accountID := uuid.New()

	for range 2 {
		if _, err := reads.TokenByDigest(t.Context(), accountID, "unknown"); err == nil {
			t.Fatal("unknown digest answered a token")
		}
	}

	if n := store.lookups.Load(); n != 2 {
		t.Fatalf("store lookups = %d, want 2 (misses are not cached)", n)
	}
}
