// Package service is the composition root: it wires the configuration,
// the dependencies (PostgreSQL, Valkey) and the scenarios into the tree
// the Backplane SDK runs.
package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/backplane-labs/backplane/pkg/backplane/build"
	"github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/valkey"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// Service is the built tree plus what main declares on the platform.
type Service struct {
	Store deps.Dependency[*postgres.Store]
	Cache deps.Optional[valkey.AsideClient]
	// HTTP is the public JSON:API (the full middleware pipeline: request
	// id, negotiation, authentication, response signing).
	HTTP http.Handler
}

// New builds the tree under root: the store and cache dependencies, the
// account bootstrap (the row is created from configuration at start)
// and the HTTP handler.
func New(root backplane.Root[Config]) (*Service, error) {
	cfg := root.Config()

	store := deps.NewDependency(root, postgres.New(cfg.Store), deps.Name("store"))
	cache := deps.NewOptional(root, valkey.NewAside(cfg.Cache), deps.Name("cache"))

	id, err := uuid.Parse(cfg.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("account.id: %w", err)
	}

	spec := accounts.Spec{ID: id, Name: cfg.Account.Name, Slug: cfg.Account.Slug}
	scenarioStore := lazyStore{store: store}

	bootstrap := deps.NewComponent(root, "bootstrap")
	bootstrap.OnStart(func(ctx context.Context) error {
		if _, err := accounts.Bootstrap(ctx, store.Get().Accounts, spec); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		return nil
	})

	throttler := httpapi.Throttler(httpapi.NewMemoryThrottler())
	if client, ok := cache.Get(); ok {
		throttler = httpapi.NewValkeyThrottler(client.Client())
	}

	clock := time.Now

	handler := httpapi.New(httpapi.Deps{
		Accounts:       lazyStore{store: store},
		Authn:          authn.New(scenarioStore, clock),
		Users:          users.New(scenarioStore, clock),
		Tokens:         tokens.New(scenarioStore, rand.Reader, clock),
		Spec:           httpapi.Spec{ID: id, Slug: spec.Slug},
		Revision:       build.Get().Commit,
		DocsHost:       cfg.DocsHost,
		Throttle:       throttler,
		TrustedProxies: cfg.HTTP.TrustedProxies,
		Clock:          clock,
	})

	return &Service{
		Store: store,
		Cache: cache,
		HTTP:  handler,
	}, nil
}

// lazyStore reads the store lazily: the dependency provides it only
// when the tree starts, after constructors have run, so every scenario
// store port goes through here.
type lazyStore struct {
	store deps.Dependency[*postgres.Store]
}

// The delegations below are the union of the scenario storage ports
// (accounts, authn, users, tokens); each calls into the store the tree
// provides at start.

// Account loads the configured account.
func (l lazyStore) Account(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	return l.store.Get().Accounts.Account(ctx, id) //nolint:wrapcheck // the repository's own error
}

// TokenByDigest loads a token by its digest.
func (l lazyStore) TokenByDigest(ctx context.Context, accountID uuid.UUID, digest string) (domain.Token, error) {
	return l.store.Get().TokenByDigest(ctx, accountID, digest) //nolint:wrapcheck // the repository's own error
}

// InsertToken writes a new token row.
func (l lazyStore) InsertToken(ctx context.Context, token domain.Token) error {
	return l.store.Get().InsertToken(ctx, token) //nolint:wrapcheck // the repository's own error
}

// BearerUser loads the override bearer (a user).
func (l lazyStore) BearerUser(ctx context.Context, accountID, id uuid.UUID) (domain.User, error) {
	return l.store.Get().BearerUser(ctx, accountID, id) //nolint:wrapcheck // the repository's own error
}

// UserByID loads a user; ErrNotFound when none.
func (l lazyStore) UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error) {
	return l.store.Get().UserByID(ctx, accountID, id) //nolint:wrapcheck // the repository's own error
}

// UserByEmail loads a user by downcased email; ErrNotFound when none.
func (l lazyStore) UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error) {
	return l.store.Get().UserByEmail(ctx, accountID, email) //nolint:wrapcheck // the repository's own error
}

// InsertUser writes a new user row.
func (l lazyStore) InsertUser(ctx context.Context, user domain.User) error {
	return l.store.Get().InsertUser(ctx, user) //nolint:wrapcheck // the repository's own error
}

// UserHasActiveLicense answers the user's status input.
func (l lazyStore) UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	return l.store.Get().UserHasActiveLicense(ctx, accountID, id) //nolint:wrapcheck // the repository's own error
}

// EnabledSecondFactorSecret loads the user's enabled TOTP secret.
func (l lazyStore) EnabledSecondFactorSecret(ctx context.Context, userID uuid.UUID) (string, bool, error) {
	return l.store.Get().EnabledSecondFactorSecret(ctx, userID) //nolint:wrapcheck // the repository's own error
}

// LicenseByKey loads the license-auth slice by key.
func (l lazyStore) LicenseByKey(ctx context.Context, accountID uuid.UUID, key string) (domain.LicenseAuth, error) {
	return l.store.Get().LicenseByKey(ctx, accountID, key) //nolint:wrapcheck // the repository's own error
}

// LicenseByID loads the license-auth slice by id.
func (l lazyStore) LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.LicenseAuth, error) {
	return l.store.Get().LicenseByID(ctx, accountID, id) //nolint:wrapcheck // the repository's own error
}
