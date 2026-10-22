package service

import (
	"context"
	"fmt"

	"github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

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
