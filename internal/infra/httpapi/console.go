package httpapi

import (
	"context"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

type consoleKey struct{}

// ConsoleContext marks only an in-process console request. Neither HTTP
// headers nor the public API can set this context key. The internal RPC
// checks OperatorOf before calling it; the check here is defense in depth.
func ConsoleContext(ctx context.Context, accountID uuid.UUID) context.Context {
	operator, ok := backplane.OperatorOf(ctx)
	if !ok {
		return ctx
	}

	return context.WithValue(ctx, consoleKey{}, access.Principal{
		Console: true, BearerType: access.BearerUser,
		BearerID: uuid.NewSHA1(accountID, []byte(operator.Actor())),
		Role:     domain.RoleAdmin, Permissions: access.RolePermissions(domain.RoleAdmin),
	})
}

func consolePrincipal(ctx context.Context) (access.Principal, bool) {
	p, ok := ctx.Value(consoleKey{}).(access.Principal)

	return p, ok
}
