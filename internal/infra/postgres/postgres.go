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
	"github.com/google/uuid"

	"github.com/gopherex/pgtx/pkg/tx"
	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

// Schema every table lives in.
const Schema = "lic"

// txRunner gives a repository the Store's Do: one read-committed
// transaction whose context every query on the pool joins.
type txRunner struct {
	trm tx.Trm
}

// Do runs fn in one transaction (nested calls join the open one).
func (r txRunner) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return tx.DoReadCommitted(ctx, r.trm, fn) //nolint:wrapcheck // fn's own error
}

//go:embed migrations/*.sql
var migrations embed.FS

// Store is the connection with the typed queries and repositories.
type Store struct {
	*postgres.DB

	// Q runs on DB: inside the context's transaction, else the pool.
	Q *db.Queries

	// Accounts is the account repository.
	Accounts AccountsRepo
	// Users is the users repository.
	Users UsersRepo
	// Tokens is the tokens repository.
	Tokens TokensRepo
	// Licenses is the license-auth repository.
	Licenses LicensesRepo
	// Products is the products repository.
	Products ProductsRepo
	// Policies is the policies repository.
	Policies PoliciesRepo
	// Entitlements is the entitlements and policy_entitlements repository.
	Entitlements EntitlementsRepo
	// Keys is the keys repository.
	Keys KeysRepo
	// Groups is the groups repository.
	Groups GroupsRepo
	// Webhooks is the webhook endpoints and events repository.
	Webhooks WebhooksRepo
	// Jobs is the periodic-jobs repository.
	Jobs JobsRepo
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

	queries := db.New(pg.DB)
	runner := txRunner{trm: pg.Trm}

	return &Store{
		DB:           pg,
		Q:            queries,
		Accounts:     AccountsRepo{q: queries},
		Users:        UsersRepo{q: queries},
		Tokens:       TokensRepo{q: queries},
		Licenses:     LicensesRepo{q: queries},
		Groups:       GroupsRepo{q: queries},
		Products:     ProductsRepo{q: queries, txRunner: runner},
		Policies:     PoliciesRepo{q: queries, txRunner: runner},
		Entitlements: EntitlementsRepo{q: queries, txRunner: runner},
		Keys:         KeysRepo{q: queries, txRunner: runner},
		Webhooks:     WebhooksRepo{q: queries},
	}, nil
}

// Do runs fn in one read-committed transaction; every store query that
// carries the transaction's context joins it (pgtx). Nested calls join
// the open transaction.
func (s *Store) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return tx.DoReadCommitted(ctx, s.Trm, fn) //nolint:wrapcheck // fn's own error
}

// The Store delegates the app scenarios' storage ports to its
// repositories, so the tree's single dependency satisfies them all.

// TokenByDigest loads a token by its digest.
func (s *Store) TokenByDigest(ctx context.Context, accountID uuid.UUID, digest string) (domain.Token, error) {
	return s.Tokens.TokenByDigest(ctx, accountID, digest)
}

// InsertToken writes a new token row.
func (s *Store) InsertToken(ctx context.Context, token domain.Token) error {
	return s.Tokens.InsertToken(ctx, token)
}

// BearerUser loads the override bearer (a user).
func (s *Store) BearerUser(ctx context.Context, accountID, id uuid.UUID) (domain.User, error) {
	return s.Users.UserByID(ctx, accountID, id)
}

// UserByID loads a user; ErrNotFound when none.
func (s *Store) UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error) {
	return s.Users.UserByID(ctx, accountID, id)
}

// UserByEmail loads a user by downcased email; ErrNotFound when none.
func (s *Store) UserByEmail(ctx context.Context, accountID uuid.UUID, email string) (domain.User, error) {
	return s.Users.UserByEmail(ctx, accountID, email)
}

// InsertUser writes a new user row.
func (s *Store) InsertUser(ctx context.Context, user domain.User) error {
	return s.Users.InsertUser(ctx, user)
}

// UserHasActiveLicense answers the user's status input.
func (s *Store) UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error) {
	return s.Users.UserHasActiveLicense(ctx, accountID, id)
}

// LicenseByKey loads the license-auth slice by key.
func (s *Store) LicenseByKey(ctx context.Context, accountID uuid.UUID, key string) (domain.LicenseAuth, error) {
	return s.Licenses.AuthByKey(ctx, accountID, key)
}

// LicenseByID loads the license-auth slice by id.
func (s *Store) LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.LicenseAuth, error) {
	return s.Licenses.AuthByID(ctx, accountID, id)
}

// Account loads the account by id.
func (s *Store) Account(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	return s.Accounts.Account(ctx, id)
}

// InsertAccount writes a new account row.
func (s *Store) InsertAccount(ctx context.Context, account domain.Account) error {
	return s.Accounts.InsertAccount(ctx, account)
}
