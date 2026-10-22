package postgres_test

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

func TestStaleProfileUpdatePreservesAuthenticationState(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIC_TEST_DATABASE_URL to test addressed-field updates")
	}

	store, err := postgres.Open(t.Context(), xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(store.Close)

	account := uuid.New()
	if _, err := accounts.Bootstrap(t.Context(), store.Accounts,
		accounts.Spec{ID: account, Slug: account.String(), Name: "stale profile test"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()

	stale := domain.User{
		ID: uuid.New(), AccountID: account, Email: uuid.NewString() + "@example.com", Role: domain.RoleDeveloper,
		PasswordDigest: "original-digest", PasswordResetToken: "original-reset-claim", PasswordResetSentAt: &now,
		Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertUser(t.Context(), stale); err != nil {
		t.Fatal(err)
	}

	if err := store.SetUserPasswordReset(t.Context(), account, stale.ID, stale.PasswordResetToken, now); err != nil {
		t.Fatal(err)
	}

	// A reset, demotion, and ban finish after the PATCH read but before its write.
	if err := store.UpdateUserPassword(t.Context(), account, stale.ID, "rotated-digest"); err != nil {
		t.Fatal(err)
	}

	demoted := stale

	demoted.Role = domain.RoleUser
	if err := store.UpdateUser(t.Context(), demoted, users.Attrs{Role: "user"}); err != nil {
		t.Fatal(err)
	}

	if err := store.SetUserBannedAt(t.Context(), account, stale.ID, &now); err != nil {
		t.Fatal(err)
	}

	stale.FirstName = "patched name"
	if err := store.UpdateUser(t.Context(), stale, users.Attrs{FirstName: &stale.FirstName}); err != nil {
		t.Fatal(err)
	}

	actual, err := store.UserByID(t.Context(), account, stale.ID)
	if err != nil {
		t.Fatal(err)
	}

	if actual.FirstName != stale.FirstName || actual.PasswordDigest != "rotated-digest" ||
		actual.PasswordResetToken != "" || actual.PasswordResetSentAt != nil || actual.Role != domain.RoleUser || !actual.Banned() {
		t.Fatalf("stale PATCH overwrote authentication state: %+v", actual)
	}
}
