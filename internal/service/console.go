package service

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/backplane-labs/backplane/pkg/backplane/build"
	"github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/console"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

func newConsole(
	root deps.Scope, store deps.Dependency[*postgres.Store], id uuid.UUID, handler http.Handler,
) *console.Server {
	component := deps.NewComponent(root, "console")

	return console.New(handler, consoleStore{store: store}, id, build.Get().Version,
		slog.New(xlog.NewSlogHandler(component.Log())))
}

// consoleStore defers access to the store until the SDK has started it.
type consoleStore struct {
	store deps.Dependency[*postgres.Store]
}

func (s consoleStore) Account(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	if !s.store.Ready() {
		return domain.Account{}, fmt.Errorf("console: store not ready: %w", accounts.ErrNotFound)
	}

	return s.store.Get().Account(ctx, id) //nolint:wrapcheck // the store's own error
}

func (s consoleStore) ConsoleCounts(ctx context.Context, id uuid.UUID) (map[string]int64, error) {
	if !s.store.Ready() {
		return nil, fmt.Errorf("console: store not ready: %w", accounts.ErrNotFound)
	}

	return s.store.Get().ConsoleCounts(ctx, id) //nolint:wrapcheck // the store's own error
}
