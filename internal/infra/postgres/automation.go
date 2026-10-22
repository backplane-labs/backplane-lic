package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

func (s *Store) ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error) {
	return s.Products.ProductByID(ctx, accountID, id)
}

func (s *Store) LockAutomationActivity(ctx context.Context, accountID uuid.UUID, key string) error {
	if err := s.Q.LockAutomationActivity(ctx, db.LockAutomationActivityParams{
		AccountID: accountID.String(), ExecutionKey: key,
	}); err != nil {
		return fmt.Errorf("postgres: lock automation activity: %w", err)
	}

	return nil
}

func (s *Store) AutomationActivityResult(ctx context.Context, accountID uuid.UUID, key string,
) (port.ActivityReceipt, error) {
	row, err := s.Q.GetAutomationActivityResult(ctx, db.GetAutomationActivityResultParams{
		AccountID: accountID, ExecutionKey: key,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return port.ActivityReceipt{}, domain.ErrNotFound
	}

	if err != nil {
		return port.ActivityReceipt{}, fmt.Errorf("postgres: automation result: %w", err)
	}

	return port.ActivityReceipt{RequestHash: row.RequestHash, Result: row.Result}, nil
}

func (s *Store) SaveAutomationActivityResult(ctx context.Context, accountID uuid.UUID, key string,
	receipt port.ActivityReceipt,
) error {
	if err := s.Q.SaveAutomationActivityResult(ctx, db.SaveAutomationActivityResultParams{
		AccountID: accountID, ExecutionKey: key, RequestHash: receipt.RequestHash, Result: receipt.Result,
	}); err != nil {
		return fmt.Errorf("postgres: save automation result: %w", err)
	}

	return nil
}
