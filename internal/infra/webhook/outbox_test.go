package webhook //nolint:testpackage // exercises the transport together with the real relay

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	outbox "github.com/gopherex/pg-outbox"

	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// These tests use the shared integration database sequentially. Other delivery
// tests drive Publish directly, so no parallel relays steal their claims.
func TestEnqueueRollsBackWithBusinessTransaction(t *testing.T) { //nolint:paralleltest // shared database
	ctx := t.Context()
	store, account := deliveryAccount(ctx, t)
	user := deliveryFixture(ctx, t, store, account)
	queue := testQueue(t, store, account.ID)
	publisher := webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now)
	rollback := errors.New("roll back business write")
	accountID := account.ID.String()
	user.ID = uuid.New()
	user.Email = "rollback-" + user.ID.String() + "@example.com"

	err := store.Do(ctx, func(ctx context.Context) error {
		if err := store.InsertUser(ctx, user); err != nil {
			return err
		}

		if err := publisher.Publish(ctx, "user.created", user); err != nil {
			return err
		}
		// Verify the reference really was enqueued before aborting.
		count, err := store.Q.CountWebhookDeliveryMessages(ctx, &accountID)
		if err != nil || count.Total != 1 {
			t.Fatalf("in-transaction outbox count = %d, err = %v", count.Total, err)
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("transaction = %v, want rollback", err)
	}

	if _, err := store.UserByID(ctx, account.ID, user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("business row survived rollback: %v", err)
	}

	count, err := store.CountWebhookEvents(ctx, account.ID, webhooks.EventFilter{}, uuid.NullUUID{})
	if err != nil || count != 0 {
		t.Fatalf("API event count = %d, err = %v", count, err)
	}

	queued, err := store.Q.CountWebhookDeliveryMessages(ctx, &accountID)
	if err != nil || queued.Total != 0 {
		t.Fatalf("outbox count = %d, err = %v", queued.Total, err)
	}
}

func TestOutboxRetryBackoffAndExhaustion(t *testing.T) { //nolint:paralleltest // one relay on the shared database
	ctx := t.Context()
	store, account := deliveryAccount(ctx, t)
	user := deliveryFixture(ctx, t, store, account)
	queue := testQueue(t, store, account.ID)

	publisher := webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now)
	if err := publisher.Publish(ctx, "user.created", user); err != nil {
		t.Fatal(err)
	}

	event := eventOf(ctx, t, store, account, "https://"+testHost+"/hook")
	worker := NewWorker(store, account.ID, publicResolver{}, []*net.IPNet{}, time.Now)
	worker.client = &http.Client{Transport: responseTransport{code: http.StatusServiceUnavailable}}
	observed := make(chan outbox.Message, MaxAttempts+1)
	hooks := outcomeHooks{events: observed, accountID: account.ID.String()}
	run := relayForTest(t, store, account.ID, worker, hooks)
	stop := startRelay(t, run)

	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		before := user.CreatedAt

		if attempt > 1 {
			before = time.Now()

			if err := store.Q.MakeWebhookDeliveryDue(ctx, event.ID); err != nil {
				t.Fatal(err)
			}
		}

		message := awaitOutcome(t, observed)
		if message.Attempts != attempt {
			t.Fatalf("attempts = %d, want %d", message.Attempts, attempt)
		}

		recorded, err := store.WebhookEventByID(ctx, account.ID, event.ID)
		if err != nil {
			t.Fatal(err)
		}

		if recorded.Attempts != attempt {
			t.Fatalf("API attempts = %d, want %d", recorded.Attempts, attempt)
		}

		if attempt == MaxAttempts {
			if message.Status != outbox.StatusDead || recorded.Status != domain.EventFailed {
				t.Fatalf("terminal states = %s/%s, want dead/FAILED", message.Status, recorded.Status)
			}

			break
		}

		if message.Status != outbox.StatusPending || recorded.Status != domain.EventFailing {
			t.Fatalf("retry states = %s/%s, want pending/FAILING", message.Status, recorded.Status)
		}

		lower := Backoff(attempt, 0)

		upper := Backoff(attempt, 1)
		if message.NextAttemptAt.Before(before.Add(lower)) || message.NextAttemptAt.After(time.Now().Add(upper)) {
			t.Fatalf("retry %d scheduled at %v, outside Keygen backoff bounds", attempt, message.NextAttemptAt)
		}
		// The actual curve is minutes long: it must remain pending rather
		// than being claimed again on the relay's next poll.
		persisted, err := store.Q.GetWebhookDeliveryMessage(ctx, event.ID)
		if err != nil || persisted.Attempts != int32(attempt) || persisted.Status != "pending" {
			t.Fatalf("scheduled message = %+v, err = %v", persisted, err)
		}
	}

	stop()
}

func TestOutboxPermanentAndDiscard(t *testing.T) { //nolint:paralleltest // one relay on the shared database
	for _, mode := range []string{"gone-event", "gone-endpoint", "auto-disable", "delivered"} { //nolint:paralleltest // shared relay claims
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			store, account := deliveryAccount(ctx, t)
			user := deliveryFixture(ctx, t, store, account)

			publisher := webhooks.NewPublisher(store, testQueue(t, store, account.ID), account.ID, rand.Reader, time.Now)
			if err := publisher.Publish(ctx, "user.created", user); err != nil {
				t.Fatal(err)
			}

			event := eventOf(ctx, t, store, account, "https://"+testHost+"/hook")
			code := http.StatusGone
			want := outbox.StatusDead

			switch mode {
			case "gone-event":
				if err := store.DeleteWebhookEvent(ctx, account.ID, event.ID); err != nil {
					t.Fatal(err)
				}

				want = outbox.StatusDiscarded
			case "gone-endpoint":
				if err := store.DeleteWebhookEndpoint(ctx, account.ID, event.EndpointID.UUID); err != nil {
					t.Fatal(err)
				}

				want = outbox.StatusDiscarded
			case "delivered":
				code, want = http.StatusOK, outbox.StatusPublished
			}

			worker := NewWorker(store, account.ID, publicResolver{}, []*net.IPNet{}, time.Now)
			worker.client = &http.Client{Transport: responseTransport{code: code}}
			observed := make(chan outbox.Message, 1)
			run := relayForTest(t, store, account.ID, worker,
				outcomeHooks{events: observed, accountID: account.ID.String()})
			stop := startRelay(t, run)
			message := awaitOutcome(t, observed)

			stop()

			if message.Status != want || message.Attempts != 1 {
				t.Fatalf("outcome = %s after %d attempts, want %s after 1", message.Status, message.Attempts, want)
			}

			if mode == "auto-disable" {
				endpoint, err := store.WebhookEndpointByID(ctx, account.ID, event.EndpointID.UUID)
				if err != nil || len(endpoint.Subscriptions) != 0 {
					t.Fatalf("endpoint not disabled: %+v, err = %v", endpoint, err)
				}
			}
		})
	}
}

func deliveryFixture(ctx context.Context, t *testing.T, store *postgres.Store, account domain.Account) domain.User {
	t.Helper()

	now := time.Now()

	endpoint := domain.WebhookEndpoint{
		ID: uuid.New(), AccountID: account.ID, URL: "https://" + testHost + "/hook",
		Subscriptions: []string{"user.created"}, SignatureAlgorithm: domain.SignatureEd25519,
		APIVersion: "1.8", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertWebhookEndpoint(ctx, endpoint); err != nil {
		t.Fatal(err)
	}

	user := domain.User{
		ID: uuid.New(), AccountID: account.ID, Email: account.ID.String() + "@example.com",
		Role: domain.RoleUser, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertUser(ctx, user); err != nil {
		t.Fatal(err)
	}

	return user
}

type responseTransport struct{ code int }

func (r responseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: r.code, Body: http.NoBody, Header: http.Header{}, Request: request}, nil
}

type outcomeHooks struct {
	outbox.NoopHooks
	events    chan<- outbox.Message
	accountID string
}

func (h outcomeHooks) OnFailed(_ context.Context, message outbox.Message, _ error) { h.send(message) }
func (h outcomeHooks) OnDead(_ context.Context, message outbox.Message)            { h.send(message) }
func (h outcomeHooks) OnPublished(_ context.Context, messages []outbox.Message) {
	for i := range messages {
		h.send(messages[i])
	}
}

func (h outcomeHooks) OnDiscarded(_ context.Context, messages []outbox.Message, _ error) {
	for i := range messages {
		h.send(messages[i])
	}
}

func (h outcomeHooks) send(message outbox.Message) {
	if message.PartitionKey == h.accountID {
		select {
		case h.events <- message:
		default:
		}
	}
}

// Relays share the isolated test database. Old fixture messages are discarded
// rather than posting to endpoints owned by another account or an earlier run.
type accountPublisher struct {
	worker    *Worker
	accountID string
}

func (p accountPublisher) Publish(ctx context.Context, messages []outbox.Message) error {
	if len(messages) == 1 && messages[0].PartitionKey != p.accountID {
		return outbox.Discard(domain.ErrNotFound)
	}

	return p.worker.Publish(ctx, messages)
}

func relayForTest(t *testing.T, store *postgres.Store, accountID uuid.UUID,
	worker *Worker, hooks outcomeHooks,
) *outbox.Outbox {
	t.Helper()

	relay, err := outbox.New(store.Pool, store.DB.DB, accountPublisher{worker: worker, accountID: accountID.String()},
		outbox.WithSchema(postgres.Schema), outbox.WithBatchSize(1),
		outbox.WithMaxAttempts(MaxAttempts), outbox.WithHooks(hooks),
		outbox.WithRetryBackoff(func(attempt int) time.Duration { return Backoff(attempt, 0) }),
		outbox.WithPollInterval(time.Millisecond), outbox.WithListenPool(store.Pool))
	if err != nil {
		t.Fatal(err)
	}

	return relay
}

func startRelay(t *testing.T, relay *outbox.Outbox) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)

	go func() { done <- relay.Run(ctx) }()

	var once sync.Once

	stop := func() {
		once.Do(func() {
			cancel()

			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("relay shutdown: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Error("relay did not stop")
			}
		})
	}

	t.Cleanup(stop)

	return stop
}

func awaitOutcome(t *testing.T, events <-chan outbox.Message) outbox.Message {
	t.Helper()

	select {
	case message := <-events:
		return message
	case <-time.After(10 * time.Second):
		t.Fatal("no relay outcome")
		return outbox.Message{}
	}
}

// Compile-time transport assertion.
var _ outbox.Publisher = (*Worker)(nil)
