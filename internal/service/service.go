// Package service is the composition root: it wires the configuration,
// the dependencies (PostgreSQL, Valkey) and the scenarios into the tree
// the Backplane SDK runs.
package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/backplane-labs/backplane/pkg/backplane/build"
	"github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/valkey"
	"github.com/google/uuid"

	"github.com/gopherex/pgtx/pkg/tx"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/entitlements"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/groups"
	"github.com/backplane-labs/backplane-lic/internal/app/keys"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/passwords"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/app/secondfactors"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// Service is the built tree plus what main declares on the platform.
type Service struct {
	Store deps.Dependency[*postgres.Store]
	Cache deps.Optional[valkey.AsideClient]
	// HTTP is the public JSON:API (the full middleware pipeline: request
	// id, negotiation, authentication, response signing); it answers 503
	// until the tree has started.
	HTTP http.Handler
}

// New builds the tree under root: the store and cache dependencies, and
// a start step that creates the account row from configuration and
// builds the HTTP handler over the started store.
func New(root backplane.Root[Config]) (*Service, error) {
	cfg := root.Config()

	store := deps.NewDependency(root, postgres.New(cfg.Store), deps.Name("store"))
	cache := deps.NewOptional(root, valkey.NewAside(cfg.Cache), deps.Name("cache"))

	id, err := uuid.Parse(cfg.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("account.id: %w", err)
	}

	spec := accounts.Spec{ID: id, Name: cfg.Account.Name, Slug: cfg.Account.Slug}
	late := &lateHandler{}

	bootstrap := deps.NewComponent(root, "bootstrap")
	bootstrap.OnStart(func(ctx context.Context) error {
		if _, err := accounts.Bootstrap(ctx, store.Get().Accounts, spec); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		throttler := httpapi.Throttler(httpapi.NewMemoryThrottler())
		if client, ok := cache.Get(); ok {
			throttler = httpapi.NewValkeyThrottler(client.Client())
		}

		late.set(Handler(store.Get(), throttler, spec, cfg.HTTP, cfg.DocsHost, build.Get().Commit))

		return nil
	})

	return &Service{Store: store, Cache: cache, HTTP: late}, nil
}

// Handler builds the public API over an opened store: the production tree
// calls it at start, tests call it directly.
func Handler(
	store *postgres.Store, throttler httpapi.Throttler, spec accounts.Spec, cfg HTTP, docsHost, revision string,
) http.Handler {
	clock := time.Now
	publisher := events.Publisher(events.None{})

	tokenManager := tokens.Manager{Scenario: tokens.New(store, publisher, rand.Reader, clock)}

	return httpapi.New(httpapi.Deps{
		Accounts: store.Accounts,
		Authn:    authn.New(store, clock),
		Users:    users.New(store, publisher, clock),
		Tokens:   tokenManager.Scenario,
		Licenses: licenses.New(
			store.Licenses, store.Licenses, store.Policies, store.Products, store,
			store, store.Entitlements, store.Keys, tokenManager, publisher, rand.Reader, clock,
			txDo(store),
		),
		Groups:         groups.New(store, publisher, clock),
		SecondFactors:  secondfactors.New(store, publisher, rand.Reader, clock),
		Passwords:      passwords.New(store, publisher, rand.Reader, clock),
		Products:       products.New(store.Products, store.Tokens, store.Users, publisher, rand.Reader, clock),
		Policies:       policies.New(store.Policies, store.Products, store.Entitlements, store.Keys, publisher, clock),
		Entitlements:   entitlements.New(store.Entitlements, publisher, clock),
		Keys:           keys.New(store.Keys, store.Policies, store.Products, publisher, clock),
		Spec:           httpapi.Spec{ID: spec.ID, Slug: spec.Slug},
		Revision:       revision,
		DocsHost:       docsHost,
		Throttle:       throttler,
		TrustedProxies: cfg.TrustedProxies,
		Clock:          clock,
	})
}

// txDo wraps pgtx's serializable boundary for the scenarios' writes.
func txDo(store *postgres.Store) func(context.Context, func(context.Context) error) error {
	return func(ctx context.Context, fn func(context.Context) error) error {
		return tx.DoSerializable(ctx, store.Trm, fn)
	}
}

// lateHandler serves the handler built at start; before that, 503.
type lateHandler struct {
	h atomic.Pointer[http.Handler]
}

func (l *lateHandler) set(h http.Handler) { l.h.Store(&h) }

func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := l.h.Load()
	if h == nil {
		http.Error(w, "starting", http.StatusServiceUnavailable)

		return
	}

	(*h).ServeHTTP(w, r)
}
