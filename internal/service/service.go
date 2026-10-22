// Package service is the composition root: it wires the configuration,
// the dependencies (PostgreSQL, Valkey) and the scenarios into the tree
// the Backplane SDK runs.
package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/valkey"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// Service is the built tree plus what main declares on the platform.
type Service struct {
	Store deps.Dependency[*postgres.Store]
	Cache deps.Optional[valkey.AsideClient]
	// HTTP is the public API. The middleware pipeline (phase A2) wraps it
	// before svc.HTTP mounts it under /v1/.
	HTTP http.Handler
}

// New builds the tree under root: the store and cache dependencies, the
// account bootstrap (the row is created from configuration at start) and
// the HTTP handler.
func New(root backplane.Root[Config]) (*Service, error) {
	cfg := root.Config()

	store := deps.NewDependency(root, postgres.New(cfg.Store), deps.Name("store"))
	cache := deps.NewOptional(root, valkey.NewAside(cfg.Cache), deps.Name("cache"))

	id, err := uuid.Parse(cfg.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("account.id: %w", err)
	}

	spec := accounts.Spec{ID: id, Name: cfg.Account.Name, Slug: cfg.Account.Slug}

	bootstrap := deps.NewComponent(root, "bootstrap")
	bootstrap.OnStart(func(ctx context.Context) error {
		if _, err := accounts.Bootstrap(ctx, store.Get().Accounts, spec); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		return nil
	})

	viewer := accountViewer{store: store, spec: spec}

	return &Service{
		Store: store,
		Cache: cache,
		HTTP:  httpapi.New(viewer, httpapi.AccountSpec{ID: spec.ID, Slug: spec.Slug}),
	}, nil
}

// accountViewer reads the account lazily: the store dependency is provided
// only when the tree starts, after constructors have run.
type accountViewer struct {
	store deps.Dependency[*postgres.Store]
	spec  accounts.Spec
}

// Account loads the configured account.
func (v accountViewer) Account(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	return v.store.Get().Accounts.Account(ctx, id) //nolint:wrapcheck // the repository's own error
}
