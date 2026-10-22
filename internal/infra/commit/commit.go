// Package commit runs actions after the surrounding transaction commits:
// effects outside the database (a cache invalidation) must not be
// observed before the change they describe.
package commit

import (
	"context"
	"sync"
)

type key struct{}

type hooks struct {
	mu  sync.Mutex
	fns []func(context.Context)
}

// Scope runs fn as a transaction body: actions deferred inside it run
// after fn and the commit succeed. A nested scope joins the outer one, so
// its actions wait for the outermost commit.
func Scope(ctx context.Context, run func(ctx context.Context) error) error {
	if _, nested := ctx.Value(key{}).(*hooks); nested {
		return run(ctx)
	}

	pending := &hooks{}
	if err := run(context.WithValue(ctx, key{}, pending)); err != nil {
		return err
	}

	pending.mu.Lock()
	fns := pending.fns
	pending.mu.Unlock()

	for _, fn := range fns {
		fn(context.WithoutCancel(ctx))
	}

	return nil
}

// After defers fn until the surrounding scope commits; outside any scope
// there is nothing to wait for and fn runs at once.
func After(ctx context.Context, fn func(context.Context)) {
	pending, ok := ctx.Value(key{}).(*hooks)
	if !ok {
		fn(ctx)

		return
	}

	pending.mu.Lock()
	pending.fns = append(pending.fns, fn)
	pending.mu.Unlock()
}
