package licenses //nolint:testpackage // tests the transaction boundary and checked arithmetic helpers

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

var (
	errTestPublish = errors.New("test: outbox unavailable")
	errTestTouch   = errors.New("test: touch unavailable")
	errTestNoTx    = errors.New("test: write outside transaction")
)

type testTxKey struct{}

type mutationStore struct {
	Store
	license  domain.License
	touchErr error
}

func (m *mutationStore) do(ctx context.Context, fn func(context.Context) error) error {
	before := m.license
	if err := fn(context.WithValue(ctx, testTxKey{}, true)); err != nil {
		m.license = before

		return err
	}

	return nil
}

func (m *mutationStore) LicenseByID(context.Context, uuid.UUID, uuid.UUID) (domain.License, error) {
	return m.license, nil
}

func (m *mutationStore) LicenseByKey(context.Context, uuid.UUID, string) (domain.License, error) {
	return m.license, nil
}

func (m *mutationStore) LicenseUsesForUpdate(ctx context.Context, _, _ uuid.UUID) (int32, error) {
	if ctx.Value(testTxKey{}) != true {
		return 0, errTestNoTx
	}

	return m.license.Uses, nil
}

func (m *mutationStore) SetLicenseUses(ctx context.Context, _, _ uuid.UUID, uses int32, _ time.Time) error {
	if ctx.Value(testTxKey{}) != true {
		return errTestNoTx
	}

	m.license.Uses = uses

	return nil
}

func (m *mutationStore) SetLicenseSuspended(ctx context.Context, _, _ uuid.UUID, suspended bool, _ time.Time) error {
	if ctx.Value(testTxKey{}) != true {
		return errTestNoTx
	}

	m.license.Suspended = suspended

	return nil
}

func (m *mutationStore) TouchLicenseValidated(
	ctx context.Context, _, _ uuid.UUID, validatedAt time.Time, _, _ *string,
) error {
	if ctx.Value(testTxKey{}) != true {
		return errTestNoTx
	}

	if m.touchErr != nil {
		return m.touchErr
	}

	m.license.LastValidatedAt = &validatedAt

	return nil
}

type mutationPublisher struct {
	err    error
	events []string
}

func (p *mutationPublisher) Publish(ctx context.Context, event string, _ any) error {
	if ctx.Value(testTxKey{}) != true {
		return errTestNoTx
	}

	p.events = append(p.events, event)

	return p.err
}

func TestMutationRollsBackOnEventFailure(t *testing.T) {
	t.Parallel()

	for _, action := range []string{"suspend", "increment", "validate", "anonymous validate-key"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()

			store := &mutationStore{license: domain.License{ID: uuid.New(), Key: "test-key"}}
			publisher := &mutationPublisher{err: errTestPublish}
			scenario := Scenario{store: store, events: publisher, doTx: store.do, clock: time.Now}
			ctx := access.With(t.Context(), access.Principal{
				Role: domain.RoleAdmin, Permissions: access.RolePermissions(domain.RoleAdmin),
			})

			var err error

			switch action {
			case "suspend":
				_, err = scenario.Suspend(ctx, domain.Account{}, store.license.ID.String())
			case "increment":
				_, err = scenario.IncrementUsage(ctx, domain.Account{}, store.license.ID.String(), 1)
			case "validate":
				_, _, err = scenario.Validate(ctx, domain.Account{}, store.license.ID.String(), Scope{})
			case "anonymous validate-key":
				_, _, err = scenario.ValidateKey(t.Context(), domain.Account{}, store.license.Key, false, Scope{})
			}

			if !errors.Is(err, errTestPublish) {
				t.Fatalf("error = %v, want outbox failure", err)
			}

			if store.license.Suspended || store.license.Uses != 0 || store.license.LastValidatedAt != nil {
				t.Fatalf("mutation committed despite failed event: %+v", store.license)
			}
		})
	}
}

func TestValidationTouchFailurePreventsEvent(t *testing.T) {
	t.Parallel()

	store := &mutationStore{license: domain.License{ID: uuid.New()}, touchErr: errTestTouch}
	publisher := &mutationPublisher{}
	scenario := Scenario{store: store, events: publisher, doTx: store.do, clock: time.Now}
	_, _, err := scenario.ValidateKey(t.Context(), domain.Account{}, "test-key", false, Scope{})

	if !errors.Is(err, errTestTouch) || len(publisher.events) != 0 {
		t.Fatalf("error = %v, events = %v", err, publisher.events)
	}
}

func TestUsageValueBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		current   int32
		amount    int64
		increment bool
		want      int32
		status    int
	}{
		{"negative increment", 0, -1, true, 0, 422},
		{"negative decrement", 0, -2, false, 2, 0},
		{"maximum", 2, math.MaxInt32 - 2, true, math.MaxInt32, 0},
		{"overflow", math.MaxInt32, 1, true, 0, 400},
		{"minimum input", 1, math.MinInt64, true, 0, 422},
		{"minimum decrement", 1, math.MinInt64, false, 0, 400},
		{"maximum input", 1, math.MaxInt64, true, 0, 400},
		{"maximum decrement", 1, math.MaxInt64, false, 0, 422},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := usageValue(test.current, test.amount, test.increment, nil)
			if test.status == 0 {
				if err != nil || got != test.want {
					t.Fatalf("value = %d, error = %v", got, err)
				}

				return
			}

			var apiErr *apperr.T
			if !errors.As(err, &apiErr) || apiErr.Status != test.status {
				t.Fatalf("error = %v, want status %d", err, test.status)
			}
		})
	}
}
