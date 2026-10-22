// Package httpapi is the HTTP face of the service: the ogen handler
// (thin: extract params, call one scenario, translate the outcome),
// JSON:API rendering, error mapping and — in a later phase — the full
// middleware pipeline (request id, path normalisation, content
// negotiation, authentication, response signing).
package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// Accounts is the consumer-side port of the account scenario. TEMPORARY
// (phase A1): no authentication yet, so the handler serves the configured
// account directly; A2 resolves the principal through app/access.
type Accounts interface {
	// Account loads the account by id.
	Account(ctx context.Context, id uuid.UUID) (domain.Account, error)
}

// API implements the ogen handler over the app's scenarios.
type API struct {
	accounts Accounts
	account  AccountSpec
}

// AccountSpec identifies the configured account (id or slug both address
// it on the wire).
type AccountSpec struct {
	ID   uuid.UUID
	Slug string
}

// New builds the service's HTTP handler: the ogen server. The middleware
// pipeline of the architecture contract wraps this value (phase A2) — it
// is the seam between routing and the platform's HTTP mounting.
func New(accounts Accounts, account AccountSpec) http.Handler {
	server, err := oas.NewServer(&API{accounts: accounts, account: account})
	if err != nil {
		// A static spec cannot fail to build a server.
		panic(fmt.Sprintf("httpapi: ogen server: %v", err))
	}

	return server
}

// Ping implements GET /v1/ping: an empty 200.
func (a *API) Ping(context.Context) error {
	return nil
}

// Health implements GET /v1/health: a 204.
func (a *API) Health(context.Context) error {
	return nil
}

// GetAccount implements GET /v1/accounts/{account}. TEMPORARY (phase A1):
// the configured account is returned without authentication.
func (a *API) GetAccount(ctx context.Context, _ oas.GetAccountParams) (*oas.AccountDocument, error) {
	account, err := a.accounts.Account(ctx, a.account.ID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return accountDocument(account), nil
}

// NewError maps handler errors to Keygen error documents. TEMPORARY
// (phase A1): everything is an internal error; A2 interprets typed app
// errors (status, title, detail, code, source, links.about) through the
// app/apperr contract.
func (a *API) NewError(_ context.Context, _ error) *oas.ErrorStatusCode {
	return &oas.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response: oas.ErrorDocument{
			Meta: oas.ErrorDocumentMeta{ID: ""}, // request id arrives with the A2 pipeline
			Errors: []oas.Error{{
				Title: "Internal server error",
				Detail: "Looks like something went wrong! Our engineers have been notified. " +
					"If you continue to have problems, please contact support@keygen.sh.",
			}},
		},
	}
}

// accountDocument renders the account exactly as the Keygen CE
// AccountSerializer does: attributes camelCase, every has-many
// relationship as a related link, public keys base64-encoded in the
// top-level meta.
func accountDocument(account domain.Account) *oas.AccountDocument {
	base := "/v1/accounts/" + account.ID.String()

	related := func(path string) oas.ToManyRelationship {
		return oas.ToManyRelationship{Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)}}
	}

	return &oas.AccountDocument{
		Data: oas.AccountResource{
			Type: oas.AccountResourceTypeAccounts,
			ID:   account.ID.String(),
			Attributes: oas.AccountAttributes{
				Name:       nilString(account.Name),
				Slug:       nilString(account.Slug),
				ApiVersion: nilString(account.APIVersion),
				Status:     oas.NilString{Null: true}, // billing&.state — always null in CE
				Protected:  oas.NewNilBool(account.Protected),
				Created:    timestamp(account.CreatedAt),
				Updated:    timestamp(account.UpdatedAt),
			},
			Relationships: oas.AccountRelationships{
				Settings:         related("/settings"),
				WebhookEndpoints: related("/webhook-endpoints"),
				WebhookEvents:    related("/webhook-events"),
				Products:         related("/products"),
				Platforms:        related("/platforms"),
				Arches:           related("/arches"),
				Channels:         related("/channels"),
				Releases:         related("/releases"),
				Artifacts:        related("/artifacts"),
				Policies:         related("/policies"),
				Users:            related("/users"),
				Keys:             related("/keys"),
				Licenses:         related("/licenses"),
				Machines:         related("/machines"),
				Components:       related("/components"),
				Processes:        related("/processes"),
				Tokens:           related("/tokens"),
			},
			Links: oas.ResourceLinks{Self: base},
		},
		Meta: oas.AccountMeta{
			// Base64 of the stored string in every case: the RSA and
			// ECDSA PEMs, and — for ed25519 — the hex string itself
			// (keygen-go base64-decodes, then hex-decodes).
			PublicKey: base64Std(account.Keys.RSAPublic),
			Keys: oas.AccountKeysMeta{
				Ed25519: base64Std(account.Keys.Ed25519Public),
				Rsa2048: base64Std(account.Keys.RSAPublic),
				Ecdsa:   base64Std(account.Keys.ECDSAPublic),
			},
		},
	}
}

// timestamp renders t as Keygen does: RFC3339 with exactly 3 fractional
// digits and a Z suffix, e.g. 2026-10-22T11:23:22.953Z.
func timestamp(t time.Time) oas.Timestamp {
	return oas.Timestamp(t.UTC().Format("2006-01-02T15:04:05.000") + "Z")
}

func nilString(s string) oas.NilString {
	if s == "" {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(s)
}

func base64Std(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}
