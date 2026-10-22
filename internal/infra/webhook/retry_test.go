package webhook //nolint:testpackage // shares the delivery fixtures

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"

	outbox "github.com/gopherex/pg-outbox"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/jobs"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

func TestManualRetryCreatesAndEnqueuesNewEvent(t *testing.T) { //nolint:paralleltest // shared database
	ctx := t.Context()
	store, account := deliveryAccount(ctx, t)
	user := deliveryFixture(ctx, t, store, account)
	queue := testQueue(t, store, account.ID)

	publisher := webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now)
	if err := publisher.Publish(ctx, "user.created", user); err != nil {
		t.Fatal(err)
	}

	original := eventOf(ctx, t, store, account, "https://"+testHost+"/hook")
	if err := store.SetWebhookEventFailure(ctx, account.ID, original.ID, domain.EventFailed, nil, "EOF_ERROR"); err != nil {
		t.Fatal(err)
	}

	principal := access.Principal{
		BearerType: access.BearerUser, BearerID: user.ID,
		Role: domain.RoleAdmin, Permissions: access.RolePermissions(domain.RoleAdmin),
	}
	ctx = access.With(ctx, principal)
	scenario := webhooks.New(store, publicResolver{}, []*net.IPNet{}, queue, time.Now)

	retry, err := scenario.Retry(ctx, account, principal, original.ID)
	if err != nil {
		t.Fatal(err)
	}

	if retry.ID == original.ID || retry.Status != domain.EventDelivering || retry.Attempts != 0 ||
		retry.Payload != original.Payload || retry.IdempotencyToken != original.IdempotencyToken ||
		retry.Endpoint != original.Endpoint || retry.APIVersion != original.APIVersion {
		t.Fatalf("retry did not copy the original into a fresh resource: %+v", retry)
	}

	message, err := store.Q.GetWebhookDeliveryMessage(ctx, retry.ID)
	if err != nil || string(message.Payload) != retry.ID.String() || message.Status != "pending" {
		t.Fatalf("retry message = %+v, err = %v", message, err)
	}

	unchanged, err := store.WebhookEventByID(ctx, account.ID, original.ID)
	if err != nil || unchanged.Status != domain.EventFailed || unchanged.Attempts != 1 {
		t.Fatalf("original mutated: %+v, err = %v", unchanged, err)
	}

	failed := webhooks.New(store, publicResolver{}, []*net.IPNet{}, failedQueue{}, time.Now)
	if _, err := failed.Retry(ctx, account, principal, original.ID); err == nil {
		t.Fatal("enqueue failure must surface")
	}

	count, err := store.CountWebhookEvents(ctx, account.ID, webhooks.EventFilter{}, uuid.NullUUID{})
	if err != nil || count != 2 {
		t.Fatalf("enqueue failure left an orphan retry: count = %d, err = %v", count, err)
	}
}

type failedQueue struct{}

func (failedQueue) Enqueue(context.Context, outbox.Message) error {
	return errors.New("simulated enqueue failure")
}

func TestPublishEnqueueFailureRollsBackAPIResource(t *testing.T) { //nolint:paralleltest // shared database
	ctx := t.Context()
	store, account := deliveryAccount(ctx, t)
	user := deliveryFixture(ctx, t, store, account)

	publisher := webhooks.NewPublisher(store, failedQueue{}, account.ID, rand.Reader, time.Now)
	if err := publisher.Publish(ctx, "user.created", user); err == nil {
		t.Fatal("enqueue failure must surface")
	}

	count, err := store.CountWebhookEvents(ctx, account.ID, webhooks.EventFilter{}, uuid.NullUUID{})
	if err != nil || count != 0 {
		t.Fatalf("enqueue failure left an orphan API resource: count=%d err=%v", count, err)
	}
}

func TestFinalSigningFailureMarksEventFailed(t *testing.T) { //nolint:paralleltest // shared database
	ctx := t.Context()
	store, account := deliveryAccount(ctx, t)
	user := deliveryFixture(ctx, t, store, account)

	publisher := webhooks.NewPublisher(store, testQueue(t, store, account.ID), account.ID, rand.Reader, time.Now)
	if err := publisher.Publish(ctx, "user.created", user); err != nil {
		t.Fatal(err)
	}

	event := eventOf(ctx, t, store, account, "https://"+testHost+"/hook")
	worker := NewWorker(invalidSigningStore{Store: store}, account.ID, publicResolver{}, []*net.IPNet{}, time.Now)
	err := worker.Publish(ctx, []outbox.Message{{Payload: []byte(event.ID.String()), Attempts: MaxAttempts}})

	var permanent *outbox.PermanentError
	if !errors.As(err, &permanent) {
		t.Fatalf("final signing failure = %v, want Permanent", err)
	}

	recorded, err := store.WebhookEventByID(ctx, account.ID, event.ID)
	if err != nil || recorded.Status != domain.EventFailed || recorded.LastResponseCode != nil {
		t.Fatalf("final signing failure did not mark FAILED: %+v, err=%v", recorded, err)
	}
}

type invalidSigningStore struct{ *postgres.Store }

func (s invalidSigningStore) Account(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	account, err := s.Store.Account(ctx, id)
	account.Keys.Ed25519Private = "invalid signing key"

	return account, err
}

func TestPruneRunnerStillRuns(t *testing.T) { //nolint:paralleltest // shared database
	store, _ := deliveryAccount(t.Context(), t)
	if err := jobs.NewRunner(store, time.Now).Tick(t.Context()); err != nil {
		t.Fatalf("prune runner: %v", err)
	}
}
