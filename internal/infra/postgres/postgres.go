// Package postgres is the service's PostgreSQL: the SDK's postgres
// provider with the service's schema (lic) and migrations, plus the typed
// queries sqld generated and the repositories that adapt them to the app's
// ports.
package postgres

import (
	"context"
	"embed"

	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// Schema every table lives in.
const Schema = "lic"

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the connection with the typed queries and repositories.
type Store struct {
	*postgres.DB

	// Q runs on DB: inside the context's transaction, else the pool.
	Q *db.Queries

	// Accounts is the account repository.
	Accounts AccountsRepo
}

// New is the provider: the postgres driver with the schema and the
// migrations.
func New(cfg postgres.Config) sdkpg.Provider[*Store] {
	return provider{cfg: cfg}
}

type provider struct {
	cfg postgres.Config
}

func (provider) Name() string { return "store" }

func (p provider) Provide(ctx context.Context, s sdkpg.Scope) (*Store, error) {
	return Open(ctx, s.Log(), p.cfg)
}

func (provider) Probe(ctx context.Context, st *Store) error {
	return st.Ping(ctx) //nolint:wrapcheck // the driver's own error
}

func (provider) Close(_ context.Context, st *Store) error {
	st.Close()

	return nil
}

// Open connects outside the dependency tree (tests, one-off tools): the
// same schema and embedded migrations as the provider.
func Open(ctx context.Context, log *xlog.Logger, cfg postgres.Config) (*Store, error) {
	pg, err := postgres.Open(ctx, log, cfg, postgres.Schema(Schema), postgres.Migrations(migrations))
	if err != nil {
		return nil, err //nolint:wrapcheck // the driver's own error
	}

	q := db.New(pg.DB)

	return &Store{DB: pg, Q: q, Accounts: AccountsRepo{q: q}}, nil
}
