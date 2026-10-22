package conformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/service"
	"github.com/backplane-labs/backplane-lic/internal/testkit"
	conformance "github.com/backplane-labs/backplane-lic/test/conformance"
)

var errNotificationPublisher = errors.New("test: notification outbox unavailable")

type notificationPublisher struct {
	failAt int
	events []string
}

func (p *notificationPublisher) Publish(_ context.Context, event string, _ any) error {
	p.events = append(p.events, event)
	if len(p.events) == p.failAt {
		return errNotificationPublisher
	}

	return nil
}

func TestLicenseScheduledNotifications(t *testing.T) { //nolint:paralleltest // serializes database bootstrap
	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIC_TEST_DATABASE_URL")
	}

	store, err := postgres.Open(t.Context(), xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(store.Close)

	id := uuid.New()

	spec := accounts.Spec{ID: id, Slug: id.String(), Name: "license notifications"}
	if _, err := accounts.Bootstrap(t.Context(), store.Accounts, spec); err != nil {
		t.Fatal(err)
	}

	adminToken, err := testkit.SeedAdmin(t.Context(), store, id, "notifications@example.com")
	if err != nil {
		t.Fatal(err)
	}

	handler, err := service.Handler(store, spec, service.HTTP{}, "keygen.test", "notifications")
	if err != nil {
		t.Fatal(err)
	}

	target := &conformance.HandlerTarget{Handler: handler, AdminToken: adminToken}
	admin := conformance.Auth{Kind: conformance.AuthAdmin}
	base := "/v1/accounts/" + id.String()
	now := time.Now().UTC()
	start := now
	fixture := conformance.Scenario{Name: "license_notifications", Steps: []conformance.Step{
		{
			Name: "product", Method: http.MethodPost, Path: base + "/products", Auth: admin,
			Body:    `{"data":{"type":"product","attributes":{"name":"notifications"}}}`,
			Capture: map[string]string{"product": "data.id"},
		},
		{
			Name: "policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
			Body:    `{"data":{"type":"policy","attributes":{"name":"expiry"},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{"policy": "data.id"},
		},
		{
			Name: "expiring license", Method: http.MethodPost, Path: base + "/licenses", Auth: admin,
			Body: `{"data":{"type":"license","attributes":{"expiry":"` + now.Add(2*time.Hour).Format(time.RFC3339Nano) + `"},"relationships":{"policy":{"data":{"type":"policy","id":"{{policy}}"}}}}}`,
		},
		{
			Name: "check-in policy", Method: http.MethodPost, Path: base + "/policies", Auth: admin,
			Body:    `{"data":{"type":"policy","attributes":{"name":"checkin","requireCheckIn":true,"checkInInterval":"day","checkInIntervalCount":1},"relationships":{"product":{"data":{"type":"product","id":"{{product}}"}}}}}`,
			Capture: map[string]string{"checkinPolicy": "data.id"},
		},
		{
			Name: "check-in license", Method: http.MethodPost, Path: base + "/licenses", Auth: admin,
			Body: `{"data":{"type":"license","relationships":{"policy":{"data":{"type":"policy","id":"{{checkinPolicy}}"}}}}}`,
		},
	}}

	transcript := conformance.Run(t, target, fixture, nil)
	for _, step := range transcript.Steps {
		if step.Status != http.StatusCreated {
			t.Fatalf("%s: status %d: %+v", step.Name, step.Status, step.Body)
		}
	}

	assertLicenseUsageLock(t, store, target, id, transcript.Steps[2].Body)

	publisher := &notificationPublisher{failAt: 2}

	notifier := licenses.NewNotifier(store.Licenses, publisher, id, func() time.Time { return now }, store.Do)
	if err := notifier.Tick(t.Context()); !errors.Is(err, errNotificationPublisher) {
		t.Fatalf("notification failure = %v", err)
	}

	publisher.failAt = 0

	publisher.events = nil
	if err := notifier.Tick(t.Context()); err != nil || len(publisher.events) != 2 {
		t.Fatalf("retry after rollback: %v, events = %v", err, publisher.events)
	}

	if err := notifier.Tick(t.Context()); err != nil || len(publisher.events) != 2 {
		t.Fatalf("daily throttle: %v, events = %v", err, publisher.events)
	}

	now = start.Add(3 * time.Hour)

	if err := notifier.Tick(t.Context()); err != nil || len(publisher.events) != 3 {
		t.Fatalf("expired notification: %v, events = %v", err, publisher.events)
	}

	now = start.Add(25 * time.Hour)

	if err := notifier.Tick(t.Context()); err != nil || len(publisher.events) != 4 {
		t.Fatalf("overdue check-in notification: %v, events = %v", err, publisher.events)
	}

	slices.Sort(publisher.events)

	if want := []string{
		"license.check-in-overdue", "license.check-in-required-soon", "license.expired", "license.expiring-soon",
	}; !slices.Equal(publisher.events, want) {
		t.Fatalf("scheduled events = %v, want %v", publisher.events, want)
	}
}

func assertLicenseUsageLock(
	t *testing.T, store *postgres.Store, target conformance.Target, accountID uuid.UUID, body any,
) {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	var document struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}

	id := uuid.MustParse(document.Data.ID)
	scenario := conformance.Scenario{Name: "license_usage_lock", Steps: []conformance.Step{{
		Name: "increment", Method: http.MethodPost,
		Path: "/v1/accounts/" + accountID.String() + "/licenses/" + id.String() + "/actions/increment-usage",
		Auth: conformance.Auth{Kind: conformance.AuthAdmin}, Body: `{}`,
	}}}
	status := 0

	err = store.Do(t.Context(), func(ctx context.Context) error {
		if _, err := store.Licenses.LicenseUsesForUpdate(ctx, accountID, id); err != nil {
			return err
		}

		// The competing HTTP request must not join the lock holder's context.
		result := conformance.Run(t, target, scenario, nil) //nolint:contextcheck // independent transaction
		status = result.Steps[0].Status

		return nil
	})
	if err != nil || status != http.StatusConflict {
		t.Fatalf("NOWAIT contention: status %d, error %v", status, err)
	}

	result := conformance.Run(t, target, scenario, nil)
	if result.Steps[0].Status != http.StatusOK {
		t.Fatalf("increment after lock release: %+v", result.Steps[0])
	}
}
