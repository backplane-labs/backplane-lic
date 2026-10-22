package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/secondfactors"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// SecondFactorsRepo adapts the generated second-factor queries to the
// secondfactors scenario's storage port.
type SecondFactorsRepo struct {
	q *db.Queries
}

// SecondFactorByID loads the user's factor.
func (r SecondFactorsRepo) SecondFactorByID(
	ctx context.Context, accountID, userID, id uuid.UUID,
) (domain.SecondFactor, error) {
	row, err := r.q.GetSecondFactorByID(ctx, db.GetSecondFactorByIDParams{
		AccountID: accountID, UserID: userID, ID: id,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SecondFactor{}, notFoundSecondFactor(id)
	}

	if err != nil {
		return domain.SecondFactor{}, fmt.Errorf("postgres: second factor %s: %w", id, err)
	}

	return secondFactor(row.ID, row.AccountID, row.UserID, row.Secret, row.Enabled,
		row.LastVerifiedAt, row.CreatedAt, row.UpdatedAt), nil
}

// AnySecondFactor loads the user's (only) factor.
func (r SecondFactorsRepo) AnySecondFactor(ctx context.Context, userID uuid.UUID) (domain.SecondFactor, error) {
	row, err := r.q.GetSecondFactorAny(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SecondFactor{}, notFoundSecondFactor(userID)
	}

	if err != nil {
		return domain.SecondFactor{}, fmt.Errorf("postgres: second factor of %s: %w", userID, err)
	}

	return secondFactor(row.ID, row.AccountID, row.UserID, row.Secret, row.Enabled,
		row.LastVerifiedAt, row.CreatedAt, row.UpdatedAt), nil
}

// InsertSecondFactor writes the factor.
func (r SecondFactorsRepo) InsertSecondFactor(ctx context.Context, factor domain.SecondFactor) error {
	err := r.q.InsertSecondFactor(ctx, db.InsertSecondFactorParams{
		ID:        factor.ID,
		AccountID: factor.AccountID,
		UserID:    factor.UserID,
		Secret:    factor.Secret,
	})
	if err != nil {
		return fmt.Errorf("postgres: insert second factor %s: %w", factor.ID, err)
	}

	return nil
}

// UpdateSecondFactor writes enabled and the replay guard.
func (r SecondFactorsRepo) UpdateSecondFactor(ctx context.Context, factor domain.SecondFactor) error {
	err := r.q.UpdateSecondFactor(ctx, db.UpdateSecondFactorParams{
		AccountID:      factor.AccountID,
		UserID:         factor.UserID,
		ID:             factor.ID,
		Enabled:        factor.Enabled,
		LastVerifiedAt: factor.LastVerifiedAt,
	})
	if err != nil {
		return fmt.Errorf("postgres: update second factor %s: %w", factor.ID, err)
	}

	return nil
}

// DeleteSecondFactor removes the factor.
func (r SecondFactorsRepo) DeleteSecondFactor(
	ctx context.Context, accountID, userID, id uuid.UUID,
) error {
	err := r.q.DeleteSecondFactor(ctx, db.DeleteSecondFactorParams{
		AccountID: accountID, UserID: userID, ID: id,
	})
	if err != nil {
		return fmt.Errorf("postgres: delete second factor %s: %w", id, err)
	}

	return nil
}

// ListSecondFactors answers the user's factors.
func (r SecondFactorsRepo) ListSecondFactors(
	ctx context.Context, accountID, userID uuid.UUID, limit int,
) ([]domain.SecondFactor, error) {
	rows, err := r.q.ListSecondFactors(ctx, db.ListSecondFactorsParams{
		AccountID: accountID, UserID: userID, Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: list second factors: %w", err)
	}

	out := make([]domain.SecondFactor, 0, len(rows))
	for i := range rows {
		row := rows[i]
		out = append(out, secondFactor(row.ID, row.AccountID, row.UserID, row.Secret, row.Enabled,
			row.LastVerifiedAt, row.CreatedAt, row.UpdatedAt))
	}

	return out, nil
}

func secondFactor(
	id, accountID, userID uuid.UUID, secret string, enabled bool,
	lastVerifiedAt *time.Time, createdAt, updatedAt time.Time,
) domain.SecondFactor {
	return domain.SecondFactor{
		ID:             id,
		AccountID:      accountID,
		UserID:         userID,
		Secret:         secret,
		Enabled:        enabled,
		LastVerifiedAt: lastVerifiedAt,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
}

func notFoundSecondFactor(id uuid.UUID) error {
	return fmt.Errorf("postgres: second factor %s: %w", id, secondfactors.ErrNotFound)
}
