package webhook //nolint:testpackage // drives the worker's unexported client

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/pkg/licensing"
)

// testHost is a hostname that passes the endpoint URL rules (a dotted
// alpha suffix, not blacklisted); the stub resolver maps it to the
// loopback receiver.
const testHost = "hooks.backplane-lic.test"

// stubResolver resolves the test host to the loopback.
type stubResolver struct{}

func (stubResolver) LookupHost(host string) ([]string, error) {
	if host == testHost {
		return []string{"127.0.0.1"}, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: host}
}

// capture records one delivery the receiver saw.
type capture struct {
	method string
	uri    string
	host   string
	header http.Header
	body   []byte
}

// TestDeliveryWithSignature publishes an event, delivers it with the
// worker to a local HTTPS receiver, and verifies the delivery with the
// licensing package's webhook verifier — the same verification a
// Keygen client performs, for every signature algorithm. Requires
// PostgreSQL (LIC_TEST_DATABASE_URL); skipped without it.
func TestDeliveryWithSignature(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LIC_TEST_DATABASE_URL not set (make up first)")
	}

	ctx := context.Background()

	store, err := postgres.Open(ctx, xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(store.Close)

	id := uuid.Must(uuid.NewV7())
	spec := accounts.Spec{ID: id, Name: "Webhook Test", Slug: "wh-" + id.String()[24:36]}

	if _, err := accounts.Bootstrap(ctx, store.Accounts, spec); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	account, err := store.Account(ctx, id)
	if err != nil {
		t.Fatalf("account: %v", err)
	}

	algorithms := []string{
		domain.SignatureEd25519, domain.SignatureRSAPSS,
		domain.SignatureRSAPKCS1v5, domain.SignatureECDSAP256,
	}

	// The subtests share one account and one claim namespace, so they
	// run in sequence (a parallel sibling's worker would claim and
	// deliver another subtest's event mid-assertion).
	for _, algorithm := range algorithms {
		t.Logf("algorithm %s", algorithm)

		deliverAndVerify(ctx, t, store, account, algorithm)
	}
}

func deliverAndVerify(
	ctx context.Context, t *testing.T, store *postgres.Store, account domain.Account, algorithm string,
) {
	t.Helper()

	delivered := make(chan capture, 8)

	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("receiver read: %v", err)
		}

		select {
		case delivered <- capture{
			method: r.Method, uri: r.URL.RequestURI(), host: r.Host,
			header: r.Header.Clone(), body: body,
		}:
		default: // the test reads one delivery; extras are dropped
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	t.Cleanup(receiver.Close)

	// The endpoint URL points at the receiver through the test host.
	endpointURL := strings.Replace(receiver.URL, "127.0.0.1", testHost, 1) + "/hook?events=1"

	now := time.Now()

	endpoint := domain.WebhookEndpoint{
		ID: uuid.New(), AccountID: account.ID, URL: endpointURL,
		Subscriptions: []string{"user.created"}, SignatureAlgorithm: algorithm,
		APIVersion: accounts.CurrentAPIVersion, CreatedAt: now, UpdatedAt: now,
	}

	if err := store.InsertWebhookEndpoint(ctx, endpoint); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	// A user row (the payload's status lookup needs one) and the
	// published event.
	user := domain.User{
		ID: uuid.New(), AccountID: account.ID,
		Email: "wh-" + algorithm + "-" + account.ID.String()[:8] + "@example.com",
		Role:  domain.RoleUser, CreatedAt: now, UpdatedAt: now,
	}

	if err := store.InsertUser(ctx, user); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	publisher := webhooks.NewPublisher(store, account.ID, rand.Reader, func() time.Time {
		return now
	})

	if err := publisher.Publish(ctx, "user.created", user); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The worker: the loopback receiver is allowed by the private
	// allowlist (the escape hatch), the test host resolves to it, and
	// the client trusts exactly the receiver's own certificate.
	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatalf("cidr: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())

	worker := NewWorker(store, account.ID, stubResolver{}, []*net.IPNet{loopback}, func() time.Time {
		return now
	})

	// The transport dials the test host as the receiver (real DNS
	// knows nothing of it) and trusts exactly its certificate.
	receiverAddr := receiver.Listener.Addr().String()
	dialer := &net.Dialer{Timeout: requestTimeout}

	worker.client = &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect },
		Transport: &http.Transport{
			// The receiver's certificate covers example.com (httptest's
			// fixture), so the handshake presents that name.
			TLSClientConfig: &tls.Config{RootCAs: roots, ServerName: "example.com"},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				if host, _, err := net.SplitHostPort(addr); err == nil && host == testHost {
					addr = receiverAddr
				}

				return dialer.DialContext(ctx, network, addr)
			},
		},
	}

	if err := worker.DeliverDue(ctx); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	var got capture

	select {
	case got = <-delivered:
	case <-time.After(10 * time.Second):
		t.Fatal("no delivery arrived")
	}

	verifyDelivery(t, account, got)

	// The outbox row of THIS endpoint records the success (the account
	// carries the earlier subtests' events too).
	events, err := store.ListWebhookEvents(ctx, account.ID, webhooks.EventFilter{Events: []string{"user.created"}},
		uuid.NullUUID{}, paging.Params{Limit: 100, Order: paging.OrderDesc})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	var event domain.WebhookEvent

	for i := range events {
		if events[i].Endpoint == endpointURL {
			event = events[i]
		}
	}

	if event.ID == uuid.Nil {
		t.Fatalf("no event for endpoint %s among %d", endpointURL, len(events))
	}

	if event.Status != domain.EventDelivered {
		t.Errorf("status = %s, want DELIVERED", event.Status)
	}

	if event.LastResponseCode == nil || *event.LastResponseCode != http.StatusOK {
		t.Errorf("last response code = %v, want 200", event.LastResponseCode)
	}

	if event.LastResponseBody == nil || *event.LastResponseBody != "ok" {
		t.Errorf("last response body = %v, want ok", event.LastResponseBody)
	}
}

// verifyDelivery replays the captured exchange through the licensing
// package's webhook verifier, the way a Keygen client consumes it.
func verifyDelivery(t *testing.T, account domain.Account, got capture) {
	t.Helper()

	algorithm := lastAlgorithm(got)

	verifier, err := licensing.NewVerifier(verifierKey(account, algorithm))
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}

	request, err := http.NewRequest(got.method, "https://"+got.host+got.uri, strings.NewReader(string(got.body)))
	if err != nil {
		t.Fatalf("replay request: %v", err)
	}

	request.Host = hostWithoutPort(got.host)
	request.Header = got.header

	if err := verifier.VerifyWebhook(request, licensing.SkewGuardFor(time.Minute)); err != nil {
		t.Fatalf("VerifyWebhook (%s): %v\nheaders: %v", algorithm, err, got.header)
	}

	if ua := got.header.Get("User-Agent"); ua != "Keygen/1.8 (+https://keygen.sh/docs/api/webhooks/)" {
		t.Errorf("User-Agent = %q", ua)
	}

	if got.header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", got.header.Get("Content-Type"))
	}

	if got.header.Get("Keygen-Version") != "1.8" {
		t.Errorf("Keygen-Version = %q", got.header.Get("Keygen-Version"))
	}

	if !strings.Contains(string(got.body), `"event":"user.created"`) {
		t.Errorf("body misses the event name: %s", got.body)
	}

	// The payload is the user document as a JSON string.
	if !strings.Contains(string(got.body), `\"type\":\"users\"`) {
		t.Errorf("payload is not a user document: %s", got.body)
	}
}

// verifierKey answers the public key form matching the algorithm.
func verifierKey(account domain.Account, algorithm string) string {
	switch algorithm {
	case domain.SignatureRSAPSS, domain.SignatureRSAPKCS1v5:
		return account.Keys.RSAPublic
	case domain.SignatureECDSAP256:
		return account.Keys.ECDSAPublic
	default:
		return account.Keys.Ed25519Public
	}
}

// lastAlgorithm extracts the algorithm from the signature header.
func lastAlgorithm(got capture) string {
	_, rest, _ := strings.Cut(got.header.Get("Keygen-Signature"), `algorithm="`)
	algorithm, _, _ := strings.Cut(rest, `"`)

	return algorithm
}

// hostWithoutPort strips the port the test server's URL carries.
func hostWithoutPort(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}

	return host
}

// publicResolver answers a public address for the test host: the URL
// validation passes while the dial goes to the loopback — the rebinding
// shape.
type publicResolver struct{}

func (publicResolver) LookupHost(host string) ([]string, error) {
	if host == testHost {
		return []string{"93.184.216.34"}, nil
	}

	return nil, &net.DNSError{Err: "no such host", Name: host}
}

// testClient keeps the worker's dial-time address check (newClient) and
// adds the test host mapping (real DNS knows nothing of it) and the
// receiver's own certificate (its fixture covers example.com).
func testClient(allowed []*net.IPNet, roots *x509.CertPool, receiverAddr string) *http.Client {
	client := newClient(allowed)

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		panic("webhook: test: transport")
	}

	transport.TLSClientConfig = &tls.Config{RootCAs: roots, ServerName: "example.com"}

	dial := transport.DialContext
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if host, _, err := net.SplitHostPort(addr); err == nil && host == testHost {
			addr = receiverAddr
		}

		return dial(ctx, network, addr)
	}

	return client
}

// TestRebindingBlocked pins the rebinding defense: the URL validation
// resolves the host to a public address, but the dial reaches the
// loopback — the delivery is refused at dial time (RESOLV_ERROR), and
// no request ever reaches the receiver. Requires PostgreSQL
// (LIC_TEST_DATABASE_URL); skipped without it.
func TestRebindingBlocked(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LIC_TEST_DATABASE_URL not set (make up first)")
	}

	ctx := context.Background()

	store, account := deliveryAccount(ctx, t)

	hit := make(chan capture, 1)

	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hit <- capture{uri: r.URL.RequestURI(), body: body}

		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(receiver.Close)

	now := time.Now()

	endpoint := domain.WebhookEndpoint{
		ID: uuid.New(), AccountID: account.ID,
		URL:                strings.Replace(receiver.URL, "127.0.0.1", testHost, 1) + "/hook",
		Subscriptions:      []string{"user.created"},
		SignatureAlgorithm: domain.SignatureEd25519,
		APIVersion:         accounts.CurrentAPIVersion, CreatedAt: now, UpdatedAt: now,
	}

	if err := store.InsertWebhookEndpoint(ctx, endpoint); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}

	user := domain.User{
		ID: uuid.New(), AccountID: account.ID,
		Email: "rebind-" + account.ID.String()[:8] + "@example.com",
		Role:  domain.RoleUser, CreatedAt: now, UpdatedAt: now,
	}

	if err := store.InsertUser(ctx, user); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	publisher := webhooks.NewPublisher(store, account.ID, rand.Reader, func() time.Time { return now })

	if err := publisher.Publish(ctx, "user.created", user); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Validation sees a public address; NO private range is allowed.
	worker := NewWorker(store, account.ID, publicResolver{}, nil, func() time.Time { return now })

	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	worker.client = testClient(nil, roots, receiver.Listener.Addr().String())

	if err := worker.DeliverDue(ctx); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	select {
	case got := <-hit:
		t.Fatalf("rebinding delivery reached the receiver: %s", got.body)
	default:
	}

	event := eventOf(ctx, t, store, account, endpoint.URL)
	if event.Status != domain.EventFailed {
		t.Errorf("status = %s, want FAILED", event.Status)
	}

	if event.LastResponseBody == nil || *event.LastResponseBody != "RESOLV_ERROR" {
		t.Errorf("last response body = %v, want RESOLV_ERROR", event.LastResponseBody)
	}

	if event.LastResponseCode != nil {
		t.Errorf("last response code = %v, want nil", event.LastResponseCode)
	}
}

// failingOutcomeStore fails the delivered-recording of one event and
// delegates everything else.
type failingOutcomeStore struct {
	*postgres.Store

	failDeliveredFor uuid.UUID
}

func (s failingOutcomeStore) SetWebhookEventDelivered(
	ctx context.Context, accountID, id uuid.UUID, code int, body string,
) error {
	if id == s.failDeliveredFor {
		return errors.New("store: simulated outcome failure")
	}

	return s.Store.SetWebhookEventDelivered(ctx, accountID, id, code, body)
}

// TestOutcomeIsolation pins the transaction scope: the claim is one
// short transaction and every outcome is recorded on its own — a store
// failure recording one delivery's success must not undo another's.
// Requires PostgreSQL (LIC_TEST_DATABASE_URL); skipped without it.
func TestOutcomeIsolation(t *testing.T) {
	t.Parallel()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LIC_TEST_DATABASE_URL not set (make up first)")
	}

	ctx := context.Background()

	store, account := deliveryAccount(ctx, t)

	okReceiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	failReceiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Cleanup(okReceiver.Close)
	t.Cleanup(failReceiver.Close)

	now := time.Now()

	_, loopback, err := net.ParseCIDR("127.0.0.0/8")
	if err != nil {
		t.Fatalf("cidr: %v", err)
	}

	allowed := []*net.IPNet{loopback}
	roots := x509.NewCertPool()
	roots.AddCert(okReceiver.Certificate())
	roots.AddCert(failReceiver.Certificate())

	newEndpoint := func(receiver *httptest.Server) domain.WebhookEndpoint {
		endpoint := domain.WebhookEndpoint{
			ID: uuid.New(), AccountID: account.ID,
			URL:                strings.Replace(receiver.URL, "127.0.0.1", testHost, 1) + "/hook",
			Subscriptions:      []string{"user.created"},
			SignatureAlgorithm: domain.SignatureEd25519,
			APIVersion:         accounts.CurrentAPIVersion, CreatedAt: now, UpdatedAt: now,
		}

		if err := store.InsertWebhookEndpoint(ctx, endpoint); err != nil {
			t.Fatalf("insert endpoint: %v", err)
		}

		return endpoint
	}

	first := newEndpoint(failReceiver)
	second := newEndpoint(okReceiver)

	user := domain.User{
		ID: uuid.New(), AccountID: account.ID,
		Email: "iso-" + account.ID.String()[:8] + "@example.com",
		Role:  domain.RoleUser, CreatedAt: now, UpdatedAt: now,
	}

	if err := store.InsertUser(ctx, user); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	publisher := webhooks.NewPublisher(store, account.ID, rand.Reader, func() time.Time { return now })

	if err := publisher.Publish(ctx, "user.created", user); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Both events are due; the FIRST endpoint's outcome recording fails.
	firstEvent := eventOf(ctx, t, store, account, first.URL)

	worker := NewWorker(
		failingOutcomeStore{Store: store, failDeliveredFor: firstEvent.ID},
		account.ID, stubResolver{}, allowed, func() time.Time { return now },
	)
	worker.client = testClient(allowed, roots, failReceiver.Listener.Addr().String())

	if err := worker.DeliverDue(ctx); err == nil {
		t.Fatal("DeliverDue: the outcome failure must surface")
	}

	// The first delivery happened (its recording failed): still
	// DELIVERING under its claim lease.
	recorded := eventOf(ctx, t, store, account, first.URL)
	if recorded.Status != domain.EventDelivering {
		t.Errorf("first event status = %s, want DELIVERING (recording failed)", recorded.Status)
	}

	// The second delivery's outcome survived.
	other := eventOf(ctx, t, store, account, second.URL)
	if other.Status != domain.EventDelivered {
		t.Errorf("second event status = %s, want DELIVERED", other.Status)
	}

	if other.LastResponseCode == nil || *other.LastResponseCode != http.StatusOK {
		t.Errorf("second event code = %v, want 200", other.LastResponseCode)
	}

	// The lease keeps the unrecorded row invisible: a second pass (with
	// a healthy store) re-delivers only after the lease, not now.
	worker2 := NewWorker(store, account.ID, stubResolver{}, allowed, func() time.Time { return now })
	worker2.client = worker.client

	if err := worker2.DeliverDue(ctx); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if again := eventOf(ctx, t, store, account, first.URL); again.Status != domain.EventDelivering {
		t.Errorf("first event re-delivered inside the lease: %s", again.Status)
	}
}

// deliveryAccount opens the store and bootstraps a fresh account.
func deliveryAccount(ctx context.Context, t *testing.T) (*postgres.Store, domain.Account) {
	t.Helper()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")

	store, err := postgres.Open(ctx, xlog.NewJSON(xlog.WithWriter(io.Discard)),
		sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(store.Close)

	id := uuid.Must(uuid.NewV7())
	spec := accounts.Spec{ID: id, Name: "Webhook Test", Slug: "wh-" + id.String()[24:36]}

	if _, err := accounts.Bootstrap(ctx, store.Accounts, spec); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	account, err := store.Account(ctx, id)
	if err != nil {
		t.Fatalf("account: %v", err)
	}

	return store, account
}

// eventOf loads the account's event for the endpoint URL.
func eventOf(
	ctx context.Context, t *testing.T, store *postgres.Store, account domain.Account, endpointURL string,
) domain.WebhookEvent {
	t.Helper()

	events, err := store.ListWebhookEvents(ctx, account.ID, webhooks.EventFilter{Events: []string{"user.created"}},
		uuid.NullUUID{}, paging.Params{Limit: 100, Order: paging.OrderDesc})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}

	for i := range events {
		if events[i].Endpoint == endpointURL {
			return events[i]
		}
	}

	t.Fatalf("no event for endpoint %s", endpointURL)

	return domain.WebhookEvent{}
}
