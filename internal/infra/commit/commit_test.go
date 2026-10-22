package commit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/infra/commit"
)

func TestActionsWaitForTheOutermostCommit(t *testing.T) {
	t.Parallel()

	var ran []string

	err := commit.Scope(t.Context(), func(ctx context.Context) error {
		commit.After(ctx, func(context.Context) { ran = append(ran, "outer") })

		return commit.Scope(ctx, func(ctx context.Context) error {
			commit.After(ctx, func(context.Context) { ran = append(ran, "nested") })

			if len(ran) != 0 {
				t.Fatal("an action ran before the commit")
			}

			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(ran) != 2 || ran[0] != "outer" || ran[1] != "nested" {
		t.Fatalf("ran = %v, want both in order after the commit", ran)
	}
}

func TestRolledBackScopeDropsItsActions(t *testing.T) {
	t.Parallel()

	ran := false
	failure := errors.New("rollback")

	err := commit.Scope(t.Context(), func(ctx context.Context) error {
		commit.After(ctx, func(context.Context) { ran = true })

		return failure
	})
	if !errors.Is(err, failure) || ran {
		t.Fatalf("err = %v, ran = %v; want the failure and no action", err, ran)
	}
}

func TestOutsideAScopeActionsRunAtOnce(t *testing.T) {
	t.Parallel()

	ran := false

	commit.After(t.Context(), func(context.Context) { ran = true })

	if !ran {
		t.Fatal("an action outside a transaction did not run")
	}
}
