// Package cache holds the cache-aside decorators of storage ports. Valkey
// keeps what is read on every request and changes rarely; a decorator
// never answers differently from the port it wraps, only sooner.
package cache

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/infra/valkey"
	"github.com/google/uuid"
	valkeygo "github.com/valkey-io/valkey-go"

	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// TokenTTL bounds how long a cached token row may outlive a change that
// raced the generation bump (bumped before its transaction committed).
const TokenTTL = 30 * time.Second

// Tokens caches token lookups by digest. The cache key carries the
// account's token generation; every token mutation bumps it, so a
// revoked or regenerated token is never answered from the cache. Bearers
// are not cached: authentication always reads them live.
type Tokens struct {
	client valkey.AsideClient
	ttl    time.Duration
}

// NewTokens builds the token cache over a cache-aside client.
func NewTokens(client valkey.AsideClient, ttl time.Duration) Tokens {
	return Tokens{client: client, ttl: ttl}
}

func generationKey(accountID uuid.UUID) string { return "lic:token-generation:" + accountID.String() }

// generation reads the account's token generation through client-side
// caching: Valkey invalidates the local copy when a bump lands.
func (t Tokens) generation(ctx context.Context, accountID uuid.UUID) (int64, error) {
	client := t.client.Client()

	value, err := client.DoCache(ctx, client.B().Get().Key(generationKey(accountID)).Cache(), t.ttl).AsInt64()
	if valkeygo.IsValkeyNil(err) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("cache: token generation: %w", err)
	}

	return value, nil
}

// bump starts a new token generation for the account.
func (t Tokens) bump(ctx context.Context, accountID uuid.UUID) error {
	client := t.client.Client()
	if err := client.Do(ctx, client.B().Incr().Key(generationKey(accountID)).Build()).Error(); err != nil {
		return fmt.Errorf("cache: bump token generation: %w", err)
	}

	return nil
}

// Authn decorates the authentication store's token lookup.
//
//nolint:ireturn // a decorator of the port
func (t Tokens) Authn(inner authn.Store) authn.Store {
	return authnStore{Store: inner, tokens: t}
}

// Manage decorates the token management store: regenerating or revoking
// a token starts a new generation.
//
//nolint:ireturn // a decorator of the port
func (t Tokens) Manage(inner tokens.Store) tokens.Store {
	return manageStore{Store: inner, tokens: t}
}

// Users decorates the users store: revoking a user's tokens (password
// changes and resets) starts a new generation.
//
//nolint:ireturn // a decorator of the port
func (t Tokens) Users(inner users.Store) users.Store {
	return usersStore{Store: inner, tokens: t}
}

type authnStore struct {
	authn.Store

	tokens Tokens
}

// TokenByDigest answers the cached row of the current generation, or
// fills it from the store. A cache failure falls back to the store.
func (s authnStore) TokenByDigest(ctx context.Context, accountID uuid.UUID, digest string) (domain.Token, error) {
	generation, err := s.tokens.generation(ctx, accountID)
	if err != nil {
		return s.Store.TokenByDigest(ctx, accountID, digest) //nolint:wrapcheck // the port's own error
	}

	key := "lic:token:" + accountID.String() + ":" + strconv.FormatInt(generation, 10) + ":" + digest

	raw, err := s.tokens.client.Get(ctx, s.tokens.ttl, key, func(ctx context.Context, _ string) (string, error) {
		token, err := s.Store.TokenByDigest(ctx, accountID, digest)
		if err != nil {
			return "", err //nolint:wrapcheck // the port's own error, answered as is
		}

		var encoded bytes.Buffer
		if err := gob.NewEncoder(&encoded).Encode(token); err != nil {
			return "", fmt.Errorf("cache: encode token: %w", err)
		}

		return encoded.String(), nil
	})
	if errors.Is(err, authn.ErrNotFound) {
		return domain.Token{}, err //nolint:wrapcheck // the port's own sentinel
	}

	var token domain.Token
	if err != nil || gob.NewDecoder(strings.NewReader(raw)).Decode(&token) != nil {
		return s.Store.TokenByDigest(ctx, accountID, digest) //nolint:wrapcheck // the port's own error
	}

	return token, nil
}

type manageStore struct {
	tokens.Store

	tokens Tokens
}

func (s manageStore) RegenerateToken(
	ctx context.Context, accountID, id uuid.UUID, digest string, expiry *time.Time,
) error {
	if err := s.Store.RegenerateToken(ctx, accountID, id, digest, expiry); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	return s.tokens.bump(ctx, accountID)
}

func (s manageStore) DeleteToken(ctx context.Context, accountID, id uuid.UUID) error {
	if err := s.Store.DeleteToken(ctx, accountID, id); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	return s.tokens.bump(ctx, accountID)
}

type usersStore struct {
	users.Store

	tokens Tokens
}

func (s usersStore) DeleteUserTokens(ctx context.Context, accountID, userID uuid.UUID) error {
	if err := s.Store.DeleteUserTokens(ctx, accountID, userID); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	return s.tokens.bump(ctx, accountID)
}

func (s usersStore) DeleteUserTokensExcept(ctx context.Context, accountID, userID, exceptID uuid.UUID) error {
	if err := s.Store.DeleteUserTokensExcept(ctx, accountID, userID, exceptID); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	return s.tokens.bump(ctx, accountID)
}

// None is the absent cache: every store answers directly.
type None struct{}

// Authn answers the store itself.
//
//nolint:ireturn // the port itself
func (None) Authn(inner authn.Store) authn.Store { return inner }

// Manage answers the store itself.
//
//nolint:ireturn // the port itself
func (None) Manage(inner tokens.Store) tokens.Store { return inner }

// Users answers the store itself.
//
//nolint:ireturn // the port itself
func (None) Users(inner users.Store) users.Store { return inner }
