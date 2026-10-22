package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// LicensesRepo adapts the license-auth lookups to the authn scenario's
// storage port.
type LicensesRepo struct {
	q *db.Queries
}

// LicenseByKey loads the license-auth slice by key.
func (r LicensesRepo) LicenseByKey(ctx context.Context, accountID uuid.UUID, key string) (domain.LicenseAuth, error) {
	row, err := r.q.GetLicenseAuthByKey(ctx, db.GetLicenseAuthByKeyParams{AccountID: accountID, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LicenseAuth{}, fmt.Errorf("postgres: license: %w", authn.ErrNotFound)
	}

	if err != nil {
		return domain.LicenseAuth{}, fmt.Errorf("postgres: license: %w", err)
	}

	return licenseAuth(row.ID, row.AccountID, row.Key, row.Suspended, row.Expiry,
		row.OwnerBanned, row.AuthenticationStrategy, row.ExpirationStrategy), nil
}

// LicenseByID loads the license-auth slice by id.
func (r LicensesRepo) LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.LicenseAuth, error) {
	row, err := r.q.GetLicenseAuthByID(ctx, db.GetLicenseAuthByIDParams{AccountID: accountID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.LicenseAuth{}, fmt.Errorf("postgres: license: %w", authn.ErrNotFound)
	}

	if err != nil {
		return domain.LicenseAuth{}, fmt.Errorf("postgres: license: %w", err)
	}

	return licenseAuth(row.ID, row.AccountID, row.Key, row.Suspended, row.Expiry,
		row.OwnerBanned, row.AuthenticationStrategy, row.ExpirationStrategy), nil
}

func licenseAuth(
	id, accountID uuid.UUID,
	key string,
	suspended bool,
	expiry *time.Time,
	ownerBanned bool,
	authenticationStrategy, expirationStrategy *string,
) domain.LicenseAuth {
	return domain.LicenseAuth{
		ID:                           id,
		AccountID:                    accountID,
		Key:                          key,
		Suspended:                    suspended,
		Expiry:                       expiry,
		OwnerBanned:                  ownerBanned,
		PolicyAuthenticationStrategy: deref(authenticationStrategy),
		PolicyExpirationStrategy:     deref(expirationStrategy),
	}
}

func jsonList(list []string) json.RawMessage {
	if list == nil {
		list = []string{}
	}

	raw, err := json.Marshal(list)
	if err != nil {
		return json.RawMessage("[]")
	}

	return raw
}

func jsonMap(m map[string]any) json.RawMessage {
	if m == nil {
		m = map[string]any{}
	}

	raw, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage("{}")
	}

	return raw
}
