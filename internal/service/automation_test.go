package service //nolint:testpackage // exercises the production HTTP and outbox composition seams

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/backplanetest"
	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	outbox "github.com/gopherex/pg-outbox"
	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	app "github.com/backplane-labs/backplane-lic/internal/app/automation"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	bp "github.com/backplane-labs/backplane-lic/internal/infra/automation"
	liccache "github.com/backplane-labs/backplane-lic/internal/infra/cache"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
)

const portalBase = "https://licenses.example.com/portal"

// testBrand is the vendor identity the automation tests deliver with.
var testBrand = domain.Brand{
	Name: "Acme", SupportEmail: "help@acme.test", AccentColor: "#3366ff",
}

func automationFixture(t *testing.T) (*postgres.Store, domain.Account, domain.User) {
	t.Helper()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LIC_TEST_DATABASE_URL not set")
	}

	store, err := postgres.Open(t.Context(), xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(store.Close)

	id := uuid.New()

	account, err := accounts.Bootstrap(t.Context(), store.Accounts, accounts.Spec{ID: id, Name: "Automation", Slug: "auto-" + id.String()})
	if err != nil {
		t.Fatal(err)
	}

	user := domain.User{
		ID: uuid.New(), AccountID: id, FirstName: "Alex", LastName: "Example",
		Email: "alex@example.com", Role: domain.RoleUser, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := store.InsertUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}

	return store, account, user
}

func automationQueue(t *testing.T, store *postgres.Store) *outbox.Outbox {
	t.Helper()

	queue, err := newOutbox(store, uuid.NewString(), slog.New(slog.DiscardHandler), discardPublisher{})
	if err != nil {
		t.Fatal(err)
	}

	return queue
}

type discardPublisher struct{}

func (discardPublisher) Publish(context.Context, []outbox.Message) error { return nil }

func subscribe(t *testing.T, store *postgres.Store, accountID uuid.UUID, names ...string) {
	t.Helper()

	now := time.Now()

	endpoint := domain.WebhookEndpoint{
		ID: uuid.New(), AccountID: accountID, URL: "https://webhook.example.com/events",
		Subscriptions: names, SignatureAlgorithm: domain.SignatureEd25519, APIVersion: "1.8", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.InsertWebhookEndpoint(t.Context(), endpoint); err != nil {
		t.Fatal(err)
	}
}

func queuedAutomation(t *testing.T, store *postgres.Store, accountID uuid.UUID) []db.ListAutomationMessagesRow {
	t.Helper()

	id := accountID.String()

	rows, err := store.Q.ListAutomationMessages(t.Context(), &id)
	if err != nil {
		t.Fatal(err)
	}

	return rows
}

func TestPasswordAutomation(t *testing.T) { //nolint:paralleltest // shared integration database
	for _, tc := range []struct { //nolint:paralleltest // subtests share integration relay fixtures
		name     string
		bound    bool
		deliver  string
		wantMail int
	}{
		{name: "bound", bound: true, wantMail: 1},
		{name: "unbound"},
		{name: "disabled", bound: true, deliver: ",\"deliver\":false"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, account, user := automationFixture(t)
			subscribe(t, store, account.ID, "user.password-reset")
			harness := backplanetest.New(t, backplanetest.Name("lic"))
			contracts := bp.Declare(harness.Root())
			calls := 0

			var delivered port.DeliverIn

			if tc.bound {
				backplanetest.Answer(harness, contracts.DeliverHook, func(ctx context.Context, in port.DeliverIn) (port.DeliverOut, error) {
					calls++
					delivered = in

					if backplanetest.CallKey(ctx) == "" {
						t.Error("missing stable hook key")
					}

					return port.DeliverOut{}, nil
				})
			}

			queue := automationQueue(t, store)
			decorate := func(publisher events.Publisher) events.Publisher {
				return app.NewPublisher(store, publisher, queue, contracts, portalBase, "ru", testBrand, time.Now)
			}
			handler := handler(store, httpapi.NewMemoryThrottler(), liccache.None{},
				accounts.Spec{ID: account.ID, Slug: account.Slug}, HTTP{}, "", "automation-test", queue, decorate)
			request := httptest.NewRequest(http.MethodPost, "/v1/accounts/"+account.ID.String()+"/passwords",
				strings.NewReader(`{"meta":{"email":"alex@example.com"`+tc.deliver+`}}`))
			request.Header.Set("Content-Type", "application/vnd.api+json")
			request.Header.Set("Accept", "application/vnd.api+json")

			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
				t.Fatalf("password reset = %d %s", response.Code, response.Body.String())
			}

			if calls != 0 || len(backplanetest.Events(harness, contracts.PasswordResetRequested)) != 0 {
				t.Fatal("automation ran in the HTTP transaction")
			}

			webhookCount, err := store.CountWebhookEvents(t.Context(), account.ID, webhooks.EventFilter{}, uuid.NullUUID{})
			if err != nil || webhookCount != 1 {
				t.Fatalf("webhook count = %d: %v", webhookCount, err)
			}

			worker := bp.NewWorker(contracts, discardPublisher{})
			for _, row := range queuedAutomation(t, store, account.ID) {
				if err := worker.Publish(t.Context(), []outbox.Message{{
					ID: row.ID.String(), Topic: row.Topic,
					MessageType: *row.MessageType, Payload: row.Payload,
				}}); err != nil {
					t.Fatal(err)
				}
			}

			if calls != tc.wantMail {
				t.Fatalf("mail calls = %d, want %d", calls, tc.wantMail)
			}

			published := backplanetest.Events(harness, contracts.PasswordResetRequested)
			if len(published) != 1 || published[0].UserID != user.ID {
				t.Fatalf("events = %#v", published)
			}

			if tc.wantMail == 0 {
				return
			}

			if delivered.Template != "lic.password-reset" || delivered.Recipient != user.Email || delivered.Locale != "ru" ||
				delivered.Data.UserName != "Alex Example" {
				t.Fatalf("delivery = %#v", delivered)
			}

			if delivered.Data.BrandName != testBrand.Name || delivered.Data.SupportEmail != testBrand.SupportEmail ||
				delivered.Data.PortalURL != portalBase {
				t.Fatalf("delivery brand = %#v", delivered.Data)
			}

			link, err := url.Parse(delivered.Data.ResetURL)
			if err != nil {
				t.Fatal(err)
			}

			fragment, err := url.ParseQuery(link.Fragment)
			if err != nil {
				t.Fatal(err)
			}

			if link.RawQuery != "" {
				t.Fatal("reset token must not reach request logs")
			}

			if link.Scheme+"://"+link.Host+link.Path != portalBase+"/reset" ||
				fragment.Get("user") != user.ID.String() || fragment.Get("account") != account.ID.String() {
				t.Fatalf("bad reset link: %s", link)
			}

			stored, err := store.UserByID(t.Context(), account.ID, user.ID)
			if err != nil {
				t.Fatal(err)
			}

			if crypto.DigestV3(account.Keys.SecretKey, fragment.Get("token")) != stored.PasswordResetToken {
				t.Fatal("delivery token does not match committed reset digest")
			}
		})
	}
}

func TestAutomationRollback(t *testing.T) { //nolint:paralleltest // shared integration database
	store, account, user := automationFixture(t)
	subscribe(t, store, account.ID, "user.created")
	harness := backplanetest.New(t)
	contracts := bp.Declare(harness.Root())
	queue := automationQueue(t, store)
	publisher := app.NewPublisher(store, webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now),
		queue, contracts, portalBase, "en", testBrand, time.Now)
	rollback := errors.New("rollback")

	err := store.Do(t.Context(), func(ctx context.Context) error {
		if err := publisher.Publish(ctx, "user.created", user); err != nil {
			return err
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("rollback = %v", err)
	}

	if len(queuedAutomation(t, store, account.ID)) != 0 || len(backplanetest.Events(harness, contracts.UserCreated)) != 0 {
		t.Fatal("rolled-back event escaped")
	}

	if err := publisher.Publish(t.Context(), "user.created", user); err != nil {
		t.Fatal(err)
	}

	rows := queuedAutomation(t, store, account.ID)
	if len(rows) != 1 {
		t.Fatalf("committed records = %d", len(rows))
	}

	if len(backplanetest.Events(harness, contracts.UserCreated)) != 0 {
		t.Fatal("published before relay")
	}

	worker := bp.NewWorker(contracts, discardPublisher{})
	if err := worker.Publish(t.Context(), []outbox.Message{{
		ID: rows[0].ID.String(), Topic: rows[0].Topic,
		MessageType: *rows[0].MessageType, Payload: rows[0].Payload,
	}}); err != nil {
		t.Fatal(err)
	}

	emitted := backplanetest.EventsWithMeta(harness, contracts.UserCreated)
	if len(emitted) != 1 || emitted[0].ID != rows[0].ID.String() {
		t.Fatalf("emitted = %#v", emitted)
	}
}

// accountWorker prevents this test relay from invoking hooks for older fixtures.
type accountWorker struct {
	worker    bp.Worker
	accountID string
}

func (p accountWorker) Publish(ctx context.Context, messages []outbox.Message) error {
	if len(messages) == 1 && messages[0].PartitionKey != p.accountID {
		return outbox.Discard(domain.ErrNotFound)
	}

	return p.worker.Publish(ctx, messages)
}

type automationHooks struct {
	outbox.NoopHooks
	done      chan<- struct{}
	messageID string
}

func (h automationHooks) OnPublished(_ context.Context, messages []outbox.Message) {
	for i := range messages {
		if messages[i].ID == h.messageID {
			h.done <- struct{}{}
		}
	}
}

func TestDeliveryOutboxRetries(t *testing.T) { //nolint:paralleltest // exactly one relay on the integration database
	store, account, user := automationFixture(t)
	harness := backplanetest.New(t)
	contracts := bp.Declare(harness.Root())

	var (
		attempts atomic.Int32
		firstKey string
	)

	unavailable := errors.New("mail unavailable")

	backplanetest.Answer(harness, contracts.DeliverHook, func(ctx context.Context, in port.DeliverIn) (port.DeliverOut, error) {
		if in.Data.ResetURL != portalBase+"/reset?fixed-token" {
			t.Error("retry changed message")
		}

		if attempts.Add(1) == 1 {
			firstKey = backplanetest.CallKey(ctx)
			return port.DeliverOut{}, unavailable
		}

		if backplanetest.CallKey(ctx) != firstKey {
			t.Error("retry changed hook key")
		}

		return port.DeliverOut{}, nil
	})

	payload, err := json.Marshal(port.DeliverIn{
		Recipient: user.Email,
		Data:      port.DeliveryData{ResetURL: portalBase + "/reset?fixed-token"},
	})
	if err != nil {
		t.Fatal(err)
	}

	messageID := uuid.NewString()
	done := make(chan struct{}, 1)
	worker := bp.NewWorker(contracts, discardPublisher{})

	relay, err := outbox.New(store.Pool, store.DB.DB, accountWorker{worker: worker, accountID: account.ID.String()},
		outbox.WithSchema(postgres.Schema), outbox.WithInstanceID(uuid.NewString()), outbox.WithBatchSize(1),
		outbox.WithPollInterval(time.Millisecond), outbox.WithRetryBackoff(func(int) time.Duration { return time.Millisecond }),
		outbox.WithHooks(automationHooks{done: done, messageID: messageID}))
	if err != nil {
		t.Fatal(err)
	}

	if err := relay.Enqueue(t.Context(), outbox.Message{
		ID: messageID, Topic: app.DeliveryTopic,
		PartitionKey: account.ID.String(), Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)

	go func() { stopped <- relay.Run(ctx) }()

	t.Cleanup(func() { cancel(); <-stopped })

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("delivery never retried")
	}

	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
}

func TestLicensingActivities(t *testing.T) { //nolint:paralleltest // shared integration database
	store, account, user := automationFixture(t)
	harness := backplanetest.New(t, backplanetest.Name("lic"))
	contracts := bp.Declare(harness.Root())
	queue := automationQueue(t, store)
	publisher := app.NewPublisher(store, webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now),
		queue, contracts, portalBase, "en", testBrand, time.Now)
	product, policy := licenseFixture(t, store, account.ID)
	_ = product
	application := app.NewActivities(store, licenseScenario(store, liccache.None{}, publisher, time.Now), account.ID, time.Now)
	server := bp.NewActivityServer(harness.Root())
	server.Set(application)

	input := port.CreateLicenseIn{PolicyID: policy.ID, OwnerID: &user.ID}

	created, err := backplanetest.Activity[port.CreateLicenseIn, port.LicenseOut](t.Context(), harness, "CreateLicense", input)
	if err != nil {
		t.Fatal(err)
	}

	license := port.LicenseIn{License: created.License.LicenseID.String()}

	shown, err := backplanetest.Activity[port.LicenseIn, port.LicenseOut](t.Context(), harness, "GetLicense", license)
	if err != nil || shown.Key != created.Key {
		t.Fatalf("get = %#v: %v", shown, err)
	}

	_, err = backplanetest.Activity[port.LicenseIn, port.LicenseOut](t.Context(), harness, "RenewLicense", license)
	if err != nil {
		t.Fatal(err)
	}

	_, err = backplanetest.Activity[port.IncrementUsageIn, port.LicenseOut](t.Context(), harness, "IncrementUsage",
		port.IncrementUsageIn{License: license.License, Increment: 2})
	if err != nil {
		t.Fatal(err)
	}

	validated, err := backplanetest.Activity[port.ValidateKeyIn, port.ValidateKeyOut](t.Context(), harness, "ValidateKey",
		port.ValidateKeyIn{Key: created.Key})
	if err != nil || !validated.Valid {
		t.Fatalf("validate = %#v: %v", validated, err)
	}

	_, err = backplanetest.Activity[port.LicenseIn, port.LicenseOut](t.Context(), harness, "SuspendLicense", license)
	if err != nil {
		t.Fatal(err)
	}

	first, err := application.CreateLicense(t.Context(), input, "stable-create")
	if err != nil {
		t.Fatal(err)
	}

	second, err := application.CreateLicense(t.Context(), input, "stable-create")
	if err != nil || first.License.LicenseID != second.License.LicenseID || first.Key != second.Key {
		t.Fatalf("create retry changed result: %#v %#v: %v", first, second, err)
	}

	name := "different input"

	input.Name = &name
	if _, err := application.CreateLicense(t.Context(), input, "stable-create"); !errors.Is(err, app.ErrKeyConflict) {
		t.Fatalf("key conflict = %v", err)
	}

	increment := port.IncrementUsageIn{License: first.License.LicenseID.String(), Increment: 3}
	if _, err := application.IncrementUsage(t.Context(), increment, "stable-increment"); err != nil {
		t.Fatal(err)
	}

	if _, err := application.IncrementUsage(t.Context(), increment, "stable-increment"); err != nil {
		t.Fatal(err)
	}

	final, err := application.GetLicense(t.Context(), port.LicenseIn{License: increment.License}, "")
	if err != nil || final.License.Uses != 3 {
		t.Fatalf("increment retry = %#v: %v", final, err)
	}
}

func licenseFixture(t *testing.T, store *postgres.Store, accountID uuid.UUID) (domain.Product, domain.Policy) {
	t.Helper()

	now := time.Now()

	product := domain.Product{ID: uuid.New(), AccountID: accountID, Name: "Example App", CreatedAt: now, UpdatedAt: now}
	if err := store.Products.InsertProduct(t.Context(), product); err != nil {
		t.Fatal(err)
	}

	duration := int32(86400)

	policy := domain.Policy{
		ID: uuid.New(), AccountID: accountID, ProductID: product.ID, Name: "Daily",
		Duration: &duration, ExpirationBasis: "FROM_CREATION", RenewalBasis: "FROM_EXPIRY", CreatedAt: now, UpdatedAt: now,
	}
	if err := store.Policies.InsertPolicy(t.Context(), policy); err != nil {
		t.Fatal(err)
	}

	return product, policy
}

func TestExpiringLicenseAutomation(t *testing.T) { //nolint:paralleltest // shared integration database
	store, account, user := automationFixture(t)
	product, policy := licenseFixture(t, store, account.ID)
	harness := backplanetest.New(t)
	contracts := bp.Declare(harness.Root())
	backplanetest.Answer(harness, contracts.DeliverHook, func(context.Context, port.DeliverIn) (port.DeliverOut, error) {
		return port.DeliverOut{}, nil
	})
	subscribe(t, store, account.ID, "license.expiring-soon")
	queue := automationQueue(t, store)
	publisher := app.NewPublisher(store, webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now),
		queue, contracts, portalBase, "en", testBrand, time.Now)
	application := app.NewActivities(store, licenseScenario(store, liccache.None{}, publisher, time.Now), account.ID, time.Now)

	created, err := application.CreateLicense(t.Context(), port.CreateLicenseIn{PolicyID: policy.ID, OwnerID: &user.ID}, "expiring")
	if err != nil {
		t.Fatal(err)
	}

	notifier := licenses.NewNotifier(store.Licenses, publisher, account.ID, time.Now, store.Do)
	if err := notifier.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := notifier.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}

	eventCount, deliveries := 0, 0

	for _, row := range queuedAutomation(t, store, account.ID) {
		if *row.MessageType == "LicenseExpiringSoon" {
			eventCount++
		}

		if row.Topic != app.DeliveryTopic {
			continue
		}

		deliveries++

		var in port.DeliverIn
		if err := json.Unmarshal(row.Payload, &in); err != nil {
			t.Fatal(err)
		}

		if in.Template != "lic.license-expiring-soon" || in.Data.ProductName != product.Name ||
			in.Data.LicenseKey != created.Key || in.Recipient != user.Email || in.Data.LicenseExpiry == "" {
			t.Fatalf("expiry delivery = %#v", in)
		}
	}

	if eventCount != 1 || deliveries != 1 {
		t.Fatalf("daily throttled events/mail = %d/%d", eventCount, deliveries)
	}
}

func TestLicenseEventContracts(t *testing.T) { //nolint:paralleltest // shared integration database
	store, account, _ := automationFixture(t)
	harness := backplanetest.New(t)
	contracts := bp.Declare(harness.Root())
	queue := automationQueue(t, store)
	publisher := app.NewPublisher(store, webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now),
		queue, contracts, portalBase, "en", testBrand, time.Now)

	snapshot := domain.License{
		ID: uuid.New(), AccountID: account.ID, ProductID: uuid.New(), PolicyID: uuid.New(),
		Uses: 4, Key: "must-not-appear-in-events",
	}
	for webhook, eventName := range map[string]string{
		"license.created": "LicenseCreated", "license.expiring-soon": "LicenseExpiringSoon",
		"license.expired": "LicenseExpired", "license.renewed": "LicenseRenewed",
		"license.suspended": "LicenseSuspended", "license.revoked": "LicenseRevoked",
		"license.validation.failed": "LicenseValidationFailed", "license.usage.incremented": "UsageIncremented",
	} {
		var resource any = snapshot
		if webhook == "license.validation.failed" {
			resource = domain.LicenseValidationEvent{License: snapshot, Code: "SUSPENDED"}
		}

		if err := publisher.Publish(t.Context(), webhook, resource); err != nil {
			t.Fatal(err)
		}

		if len(backplanetest.Events(harness, contracts.LicenseEvents[eventName])) != 0 {
			t.Fatal("published before relay")
		}
	}

	rows := queuedAutomation(t, store, account.ID)
	if len(rows) != 8 {
		t.Fatalf("license event records = %d", len(rows))
	}

	worker := bp.NewWorker(contracts, discardPublisher{})

	for _, row := range rows {
		if strings.Contains(string(row.Payload), snapshot.Key) {
			t.Fatal("license credential escaped into event")
		}

		if err := worker.Publish(t.Context(), []outbox.Message{{
			ID: row.ID.String(), Topic: row.Topic,
			MessageType: *row.MessageType, Payload: row.Payload,
		}}); err != nil {
			t.Fatal(err)
		}

		emitted := backplanetest.EventsWithMeta(harness, contracts.LicenseEvents[*row.MessageType])
		if len(emitted) != 1 || emitted[0].ID != row.ID.String() || emitted[0].Value.LicenseID != snapshot.ID {
			t.Fatalf("license event = %#v", emitted)
		}

		if *row.MessageType == "LicenseValidationFailed" && emitted[0].Value.ValidationCode != "SUSPENDED" {
			t.Fatal("missing validation code")
		}
	}
}

var errReceiptUnavailable = errors.New("receipt persistence unavailable")

type failingReceiptStore struct{ *postgres.Store }

func (failingReceiptStore) SaveAutomationActivityResult(context.Context, uuid.UUID, string, port.ActivityReceipt) error {
	return errReceiptUnavailable
}

func TestActivityReceiptFailureRollsBack(t *testing.T) { //nolint:paralleltest // shared integration database
	store, account, _ := automationFixture(t)
	_, policy := licenseFixture(t, store, account.ID)
	queue := automationQueue(t, store)
	harness := backplanetest.New(t)
	contracts := bp.Declare(harness.Root())
	publisher := app.NewPublisher(store, webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now),
		queue, contracts, portalBase, "en", testBrand, time.Now)
	application := app.NewActivities(failingReceiptStore{Store: store}, licenseScenario(store, liccache.None{}, publisher, time.Now),
		account.ID, time.Now)

	created, err := application.CreateLicense(t.Context(), port.CreateLicenseIn{PolicyID: policy.ID}, "failed-receipt")
	if !errors.Is(err, errReceiptUnavailable) {
		t.Fatalf("receipt error = %v", err)
	}

	if _, err := store.Licenses.LicenseByID(t.Context(), account.ID, created.License.LicenseID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("failed activity left a license: %v", err)
	}

	if len(queuedAutomation(t, store, account.ID)) != 0 {
		t.Fatal("failed activity left events")
	}
}

func TestConcurrentActivityAttempts(t *testing.T) { //nolint:paralleltest // shared integration database
	store, account, _ := automationFixture(t)
	_, policy := licenseFixture(t, store, account.ID)
	harness := backplanetest.New(t)
	contracts := bp.Declare(harness.Root())
	queue := automationQueue(t, store)
	publisher := app.NewPublisher(store, webhooks.NewPublisher(store, queue, account.ID, rand.Reader, time.Now),
		queue, contracts, portalBase, "en", testBrand, time.Now)
	application := app.NewActivities(store, licenseScenario(store, liccache.None{}, publisher, time.Now), account.ID, time.Now)

	type attempt struct {
		result port.LicenseOut
		err    error
	}

	const concurrency = 4

	results := make(chan attempt, concurrency)
	for range concurrency {
		go func() {
			result, err := application.CreateLicense(t.Context(), port.CreateLicenseIn{PolicyID: policy.ID}, "concurrent-create")
			results <- attempt{result: result, err: err}
		}()
	}

	var id uuid.UUID

	for range concurrency {
		outcome := <-results
		if outcome.err != nil {
			t.Fatal(outcome.err)
		}

		if id != uuid.Nil && id != outcome.result.License.LicenseID {
			t.Fatal("concurrent retry created another license")
		}

		id = outcome.result.License.LicenseID
	}

	if rows := queuedAutomation(t, store, account.ID); len(rows) != 1 {
		t.Fatalf("concurrent attempts recorded %d events, want 1", len(rows))
	}
}
