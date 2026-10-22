// Package webhook delivers the outbox rows: it claims due webhook
// events (FOR UPDATE SKIP LOCKED), POSTs the serialized event to the
// endpoint with Keygen's headers and signature, and records the
// outcome, the retry schedule and the auto-disable rules.
package webhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// maxResponseBytes is the response body bound: larger bodies are stored
// as the literal RES_BODY_TOO_LARGE.
const maxResponseBytes = 2048

// maxAttempts is the reference's retry budget: the first delivery plus
// 15 sidekiq retries.
const maxAttempts = 16

// claimBatch is the number of due rows one pass claims.
const claimBatch = 10

// requestTimeout bounds each of the connect, write and read phases,
// like the reference's HTTParty timeouts.
const requestTimeout = 15 * time.Second

// userAgentFormat is the reference's webhook User-Agent.
const userAgentFormat = "Keygen/%s (+https://keygen.sh/docs/api/webhooks/)"

// Store is the worker's storage port (satisfied by *postgres.Store).
type Store interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error

	Account(ctx context.Context, id uuid.UUID) (domain.Account, error)
	ClaimDueWebhookEvents(
		ctx context.Context, accountID uuid.UUID, limit int, now, lease time.Time,
	) ([]webhooks.Delivery, error)
	SetWebhookEventDelivered(ctx context.Context, accountID, id uuid.UUID, code int, body string) error
	SetWebhookEventFailure(
		ctx context.Context, accountID, id uuid.UUID, status domain.WebhookEventStatus,
		code *int, body string, nextAttempt *time.Time,
	) error
	DisableWebhookEndpoint(ctx context.Context, accountID, id uuid.UUID) error
}

// claimLease is how long a claimed row stays invisible to other
// workers: the batch's worst case (every delivery sits out the full
// request timeout) plus headroom. A crashed worker's rows become due
// again when the lease passes.
const claimLease = 5 * time.Minute

// Worker delivers due webhook events. It is immutable after
// construction; the composition root runs DeliverDue periodically.
type Worker struct {
	store     Store
	accountID uuid.UUID
	client    *http.Client
	resolver  domain.Resolver
	allowed   []*net.IPNet
	clock     func() time.Time
	random    func() float64
}

// NewWorker builds the worker over the store (the account whose keys
// sign the deliveries).
func NewWorker(
	store Store, accountID uuid.UUID, resolver domain.Resolver, allowed []*net.IPNet,
	clock func() time.Time,
) *Worker {
	return &Worker{
		store:     store,
		accountID: accountID,
		client:    newClient(allowed),
		resolver:  resolver,
		allowed:   allowed,
		clock:     clock,
		random:    rand.Float64,
	}
}

// newClient builds the delivery client: the same timeouts as the
// reference, redirects refused, and a dialer that re-checks the ACTUAL
// remote address against the public-address rule. The URL is validated
// (DNS included) before the request, but the transport resolves again
// on dial — without the dial-time check a rebinding host would pass
// validation and then be delivered to a loopback or link-local
// address.
func newClient(allowed []*net.IPNet) *http.Client {
	dialer := &net.Dialer{
		Timeout: requestTimeout,
		Control: controlAddress(allowed),
	}

	return &http.Client{
		Timeout: requestTimeout,
		// Redirects are never followed: a redirect is the
		// REDIRECT_ERROR outcome, not a delivery.
		CheckRedirect: func(*http.Request, []*http.Request) error { return errRedirect },
		Transport:     &http.Transport{DialContext: dialer.DialContext},
	}
}

// controlAddress answers the dialer's Control hook: the address the
// socket is about to connect to must be one the URL rules accept
// (public, or inside the configured private allowlist).
func controlAddress(allowed []*net.IPNet) func(network, address string, _ syscall.RawConn) error {
	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("%w: %w", errForbiddenAddress, err)
		}

		ip := net.ParseIP(host)
		if ip == nil || !domain.PublicAddress(ip, allowed) {
			return fmt.Errorf("%w: %s", errForbiddenAddress, address)
		}

		return nil
	}
}

// errForbiddenAddress aborts a dial to an address the URL rules refuse
// (the rebinding case); the worker maps it to RESOLV_ERROR.
var errForbiddenAddress = errors.New("webhook: address refused by the URL rules")

// errRedirect aborts a redirect chain; the worker maps it to the
// reference's RedirectionTooDeep.
var errRedirect = errors.New("webhook: redirect")

// DeliverDue claims the due batch and delivers it. The claim is one
// short transaction (SKIP LOCKED + the lease push); every delivery and
// every outcome recording runs in its own statement-level transaction,
// so one delivery's failure never rolls back another's recorded
// outcome.
func (w *Worker) DeliverDue(ctx context.Context) error {
	now := w.clock()

	batch, err := w.store.ClaimDueWebhookEvents(ctx, w.accountID, claimBatch, now, now.Add(claimLease))
	if err != nil {
		return fmt.Errorf("webhook: claim: %w", err)
	}

	if len(batch) == 0 {
		return nil
	}

	account, err := w.store.Account(ctx, w.accountID)
	if err != nil {
		return fmt.Errorf("webhook: account: %w", err)
	}

	var errs []error

	for i := range batch {
		if err := w.deliver(ctx, account, batch[i]); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// deliver performs one delivery and records its outcome.
func (w *Worker) deliver(
	ctx context.Context, account domain.Account, delivery webhooks.Delivery,
) error {
	// The endpoint URL is re-validated before every delivery (fresh
	// DNS): a gone name is RESOLV_ERROR — FAILED, but retried in case
	// the outage is transient, exactly like the reference.
	if violations := domain.ValidateWebhookURL(
		delivery.EndpointURL, w.resolver, w.allowed,
	); len(violations) > 0 {
		return w.fail(ctx, delivery, domain.EventFailed, nil, "RESOLV_ERROR", true)
	}

	body := w.body(delivery)

	req, err := w.request(ctx, account, delivery, body)
	if err != nil {
		return err
	}

	resp, err := w.client.Do(req)
	if err != nil {
		if errors.Is(err, errRedirect) {
			// The client hands back the redirect response alongside the
			// abort error: its code is the outcome's code.
			code := 0

			if resp != nil {
				code = resp.StatusCode
				resp.Body.Close()
			}

			return w.fail(ctx, delivery, domain.EventFailed, &code, "REDIRECT_ERROR", true)
		}

		return w.transportFailure(ctx, delivery, err)
	}

	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if readErr != nil {
		return w.fail(ctx, delivery, domain.EventFailing, nil, "REQ_TIMEOUT", true)
	}

	recorded := string(raw)
	if len(raw) > maxResponseBytes {
		recorded = "RES_BODY_TOO_LARGE"
	}

	if resp.StatusCode/100 == 2 { //nolint:mnd // the 2xx family
		return w.store.SetWebhookEventDelivered( //nolint:wrapcheck // the store's own error
			ctx, w.accountID, delivery.Event.ID, resp.StatusCode, recorded)
	}

	return w.responseFailure(ctx, delivery, resp.StatusCode, recorded)
}

// body serializes the delivered webhook-event document: the stored
// event with its payload string, rendered fresh for the delivery.
func (w *Worker) body(delivery webhooks.Delivery) string {
	encoder := jx.Encoder{}
	document := oas.WebhookEventDocument{
		Data: render.WebhookEvent(w.accountID, delivery.Event),
	}

	document.Encode(&encoder)

	return encoder.String()
}

// request builds the signed POST: the endpoint URL's path and host,
// the digest of the body, the signature over the signing string and
// Keygen's header set.
func (w *Worker) request(
	ctx context.Context, account domain.Account, delivery webhooks.Delivery, body string,
) (*http.Request, error) {
	target, err := url.Parse(delivery.EndpointURL)
	if err != nil {
		return nil, w.fail(ctx, delivery, domain.EventFailed, nil, "RESOLV_ERROR", true)
	}

	uri := target.Path
	if uri == "" {
		uri = "/"
	}

	if target.RawQuery != "" {
		uri += "?" + target.RawQuery
	}

	date := w.clock().UTC().Format(http.TimeFormat)
	digest := crypto.Digest([]byte(body))

	signature, err := crypto.Sign(account.Keys, delivery.SignatureAlgorithm, crypto.SigningData(
		http.MethodPost, uri, target.Hostname(), date, digest,
	))
	if err != nil {
		return nil, fmt.Errorf("webhook: sign event %s: %w", delivery.Event.ID, err)
	}

	version := delivery.Event.APIVersion
	if version == "" {
		version = accounts.CurrentAPIVersion
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.EndpointURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("webhook: build request for event %s: %w", delivery.Event.ID, err)
	}

	req.Header.Set("User-Agent", fmt.Sprintf(userAgentFormat, version))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Date", date)
	req.Header.Set("Digest", digest)
	req.Header.Set("Keygen-Date", date)
	req.Header.Set("Keygen-Digest", digest)
	req.Header.Set("Keygen-Signature", fmt.Sprintf(
		`keyid=%q, algorithm=%q, signature=%q, headers="(request-target) host date digest"`,
		w.accountID.String(), delivery.SignatureAlgorithm, signature,
	))
	req.Header.Set("Keygen-Version", version)

	return req, nil
}

// responseFailure applies the non-2xx rules: the auto-disable cases,
// else FAILING with a retry (FAILED once the budget is spent).
func (w *Worker) responseFailure(
	ctx context.Context, delivery webhooks.Delivery, code int, body string,
) error {
	event := delivery.Event
	endpointURL := event.Endpoint

	switch {
	case strings.Contains(endpointURL, ".ngrok.io") && code == http.StatusNotFound &&
		ngrokTunnelNotFound.MatchString(body):
		if err := w.fail(ctx, delivery, domain.EventFailed, &code, body, false); err != nil {
			return err
		}

		return w.disable(ctx, delivery)
	case strings.Contains(endpointURL, ".ngrok.io") && code == http.StatusGatewayTimeout,
		strings.Contains(endpointURL, ".loca.lt") && code == http.StatusGatewayTimeout:
		return w.fail(ctx, delivery, domain.EventFailed, &code, body, false)
	case code == http.StatusGone, code == 530: //nolint:mnd // Cloudflare's "Frozen" code
		if err := w.fail(ctx, delivery, domain.EventFailed, &code, body, false); err != nil {
			return err
		}

		return w.disable(ctx, delivery)
	default:
		return w.fail(ctx, delivery, domain.EventFailing, &code, body, true)
	}
}

// ngrokTunnelNotFound matches the dead-tunnel page ngrok answers with.
var ngrokTunnelNotFound = regexp.MustCompile(`(?i)tunnel .+?\.ngrok\.io not found`)

// transportFailure classifies a delivery that never got a response,
// with the reference's pseudo response bodies.
func (w *Worker) transportFailure(ctx context.Context, delivery webhooks.Delivery, err error) error {
	var dnsErr *net.DNSError

	switch {
	case errors.Is(err, errForbiddenAddress):
		// The dial reached an address the URL rules refuse (the
		// rebinding case): refused like an unresolvable endpoint, and
		// retried in case the record was transient.
		return w.fail(ctx, delivery, domain.EventFailed, nil, "RESOLV_ERROR", true)
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return w.fail(ctx, delivery, domain.EventFailed, nil, "EOF_ERROR", false)
	case isSSL(err):
		return w.fail(ctx, delivery, domain.EventFailing, nil, "SSL_ERROR", true)
	case isTimeout(err):
		return w.fail(ctx, delivery, domain.EventFailing, nil, "REQ_TIMEOUT", true)
	case errors.As(err, &dnsErr):
		return w.fail(ctx, delivery, domain.EventFailed, nil, "DNS_ERROR", false)
	case hasErrno(err, syscall.ECONNREFUSED):
		return w.fail(ctx, delivery, domain.EventFailed, nil, "CONN_REFUSED", false)
	case hasErrno(err, syscall.ECONNRESET):
		return w.fail(ctx, delivery, domain.EventFailed, nil, "CONN_RESET", false)
	case hasErrno(err, syscall.ENETUNREACH):
		return w.fail(ctx, delivery, domain.EventFailed, nil, "NET_UNREACH", false)
	case hasErrno(err, syscall.EHOSTUNREACH):
		return w.fail(ctx, delivery, domain.EventFailed, nil, "HOST_UNREACH", false)
	default:
		return w.fail(ctx, delivery, domain.EventFailing, nil, "REQ_TIMEOUT", true)
	}
}

// fail records a failure outcome; retry schedules the next attempt
// with the reference's backoff unless the budget is spent.
func (w *Worker) fail(
	ctx context.Context,
	delivery webhooks.Delivery,
	status domain.WebhookEventStatus,
	code *int,
	body string,
	retry bool,
) error {
	var next *time.Time

	if retry && delivery.Event.Attempts+1 < maxAttempts {
		backoff := Backoff(delivery.Event.Attempts+1, w.random())
		due := w.clock().Add(backoff)
		next = &due
	}

	return w.store.SetWebhookEventFailure( //nolint:wrapcheck // the store's own error
		ctx, w.accountID, delivery.Event.ID, status, code, body, next)
}

// disable empties the endpoint's subscriptions (auto-disable).
func (w *Worker) disable(ctx context.Context, delivery webhooks.Delivery) error {
	if !delivery.Event.EndpointID.Valid {
		return nil
	}

	return w.store.DisableWebhookEndpoint( //nolint:wrapcheck // the store's own error
		ctx, w.accountID, delivery.Event.EndpointID.UUID)
}

// Backoff is the reference's sidekiq retry curve: count**4 seconds
// plus a jitter of 10 minutes times a uniform 1..10.
func Backoff(count int, random float64) time.Duration {
	jitter := time.Duration((1 + random*9) * float64(10*time.Minute)) //nolint:mnd // rand(1..10) minutes
	seconds := time.Duration(count*count*count*count) * time.Second

	return seconds + jitter
}

func isTimeout(err error) bool {
	var netErr net.Error

	return errors.As(err, &netErr) && netErr.Timeout()
}

func isSSL(err error) bool {
	certMsg := err.Error()

	return strings.Contains(certMsg, "x509") || strings.Contains(certMsg, "tls:")
}

func hasErrno(err error, errno syscall.Errno) bool {
	var sysErr syscall.Errno

	return errors.As(err, &sysErr) && sysErr == errno
}
