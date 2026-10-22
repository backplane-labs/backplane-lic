package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// ConsoleCounts returns the operator's account-scoped resource overview.
func (s *Store) ConsoleCounts(ctx context.Context, accountID uuid.UUID) (map[string]int64, error) {
	row, err := s.Q.ConsoleCounts(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("postgres: console counts: %w", err)
	}

	return map[string]int64{
		"products": row.Products, "policies": row.Policies, "licenses": row.Licenses,
		"users": row.Users, "groups": row.Groups, "entitlements": row.Entitlements,
		"tokens": row.Tokens, "webhook-endpoints": row.WebhookEndpoints, "webhook-events": row.WebhookEvents,
	}, nil
}
