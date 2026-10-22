// lic-seed creates the bootstrap admin user directly in the database —
// the operator replacement for the removed configuration admin seeding
// (admin tokens are issued from the console; this covers fresh installs
// and conformance runs before the console exists). Flags: -dsn, -account,
// -email, -password.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
)

func run(ctx context.Context) error {
	var (
		dsn      = flag.String("dsn", "", "PostgreSQL DSN of the lic database")
		account  = flag.String("account", "", "UUID of the single account")
		email    = flag.String("email", "", "Email of the admin user")
		password = flag.String("password", "", "Password of the admin user")
	)

	flag.Parse()

	id, err := uuid.Parse(*account)
	if err != nil {
		return fmt.Errorf("lic-seed: -account: %w", err)
	}

	store, err := postgres.Open(
		ctx,
		xlog.NewJSON(xlog.WithWriter(io.Discard)),
		sdkpg.Config{DSN: config.Secret(*dsn)},
	)
	if err != nil {
		return fmt.Errorf("lic-seed: %w", err)
	}

	defer store.Close()

	if err := testkit.SeedAdmin(ctx, store, id, *email, *password); err != nil {
		return fmt.Errorf("lic-seed: %w", err)
	}

	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
