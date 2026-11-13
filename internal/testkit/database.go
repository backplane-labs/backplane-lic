package testkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// DatabaseEnv names the PostgreSQL server of the live tests.
const DatabaseEnv = "LIC_TEST_DATABASE_URL"

// dropTimeout bounds dropping a test database.
const dropTimeout = 30 * time.Second

// suffixBytes is the random part of a test database name.
const suffixBytes = 8

// Database answers the DSN of a fresh database on the live-test server,
// dropped when the test ends. Tests that run an outbox relay or read
// rows across accounts get one each, so concurrent packages share no
// queue. It skips the test without DatabaseEnv.
func Database(ctx context.Context, tb testing.TB) string {
	tb.Helper()

	dsn := os.Getenv(DatabaseEnv)
	if dsn == "" {
		tb.Skip(DatabaseEnv + " not set (make up first)")
	}

	suffix := make([]byte, suffixBytes)
	if _, err := rand.Read(suffix); err != nil {
		tb.Fatal(err)
	}

	name := "lic_test_" + hex.EncodeToString(suffix)

	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		tb.Fatalf("testkit: connect: %v", err)
	}

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		tb.Fatalf("testkit: create database: %v", err)
	}

	// The test context is cancelled before cleanups run, so the drop keeps
	// only its values.
	parent := context.WithoutCancel(ctx)

	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(parent, dropTimeout)
		defer cancel()

		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			tb.Errorf("testkit: drop database: %v", err)
		}

		_ = admin.Close(ctx)
	})

	parsed, err := url.Parse(dsn)
	if err != nil {
		tb.Fatalf("testkit: dsn: %v", err)
	}

	parsed.Path = "/" + name

	return parsed.String()
}
