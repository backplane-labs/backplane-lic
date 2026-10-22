// Package testkit holds the helpers tests and operator tools share:
// creating an admin user directly in the database, the replacement for
// the removed configuration admin seeding (admin tokens are issued from
// the console instead).
package testkit

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// SeedAdmin creates the account's bootstrap admin user (bcrypt digest,
// the admin role) when her email is not taken; an existing user is left
// alone, so the call is idempotent per email.
func SeedAdmin(ctx context.Context, db *postgres.Store, accountID uuid.UUID, email, password string) error {
	scenario := users.New(db.Users, time.Now)

	return scenario.SeedAdmin(ctx, accountID, email, password) //nolint:wrapcheck // the scenario's own error
}
