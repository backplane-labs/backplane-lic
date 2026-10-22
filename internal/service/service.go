// Package service is the composition root: it wires the configuration,
// the dependencies (PostgreSQL, Valkey) and the scenarios into the tree
// the Backplane SDK runs.
package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/backplane-labs/backplane/pkg/backplane/build"
	"github.com/backplane-labs/backplane/pkg/backplane/deps"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/valkey"
	"github.com/google/uuid"

	outbox "github.com/gopherex/pg-outbox"
	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/app/entitlements"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/groups"
	"github.com/backplane-labs/backplane-lic/internal/app/jobs"
	"github.com/backplane-labs/backplane-lic/internal/app/keys"
	"github.com/backplane-labs/backplane-lic/internal/app/passwords"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/app/secondfactors"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/portal"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	whwebhook "github.com/backplane-labs/backplane-lic/internal/infra/webhook"
)

// Service is the built tree plus what main declares on the platform.
type Service struct {
	Store deps.Dependency[*postgres.Store]
	Cache deps.Optional[valkey.AsideClient]
	// HTTP is the public JSON:API (the full middleware pipeline: request
	// id, negotiation, authentication, response signing); it answers 503
	// until the tree has started.
	HTTP http.Handler
}

// New builds the tree under root: the store and cache dependencies, and
// a start step that creates the account row from configuration and
// builds the HTTP handler over the started store.
func New(root backplane.Root[Config]) (*Service, error) {
	cfg := root.Config()

	store := deps.NewDependency(root, postgres.New(cfg.Store), deps.Name("store"))
	cache := deps.NewOptional(root, valkey.NewAside(cfg.Cache), deps.Name("cache"))

	id, err := uuid.Parse(cfg.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("account.id: %w", err)
	}

	spec := accounts.Spec{ID: id, Name: cfg.Account.Name, Slug: cfg.Account.Slug}
	late := &lateHandler{}

	public, err := portal.New(cfg.HTTP.Portal, id.String(), late)
	if err != nil {
		return nil, fmt.Errorf("http.portal: %w", err)
	}

	var relayOutbox *outbox.Outbox

	bootstrap := deps.NewComponent(root, "bootstrap")
	bootstrap.OnStart(func(ctx context.Context) error {
		if _, err := accounts.Bootstrap(ctx, store.Get().Accounts, spec); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		relay, err := webhookOutbox(store.Get(), spec.ID, cfg.HTTP, root.Identity().Instance,
			slog.New(xlog.NewSlogHandler(bootstrap.Log())))
		if err != nil {
			return err
		}

		relayOutbox = relay

		throttler := httpapi.Throttler(httpapi.NewMemoryThrottler())
		if client, ok := cache.Get(); ok {
			throttler = httpapi.NewValkeyThrottler(client.Client())
		}

		late.set(handler(store.Get(), throttler, spec, cfg.HTTP, cfg.DocsHost, build.Get().Commit, relay))

		return nil
	})

	// The constructor starts nothing. The SDK owns Run and cancellation.
	delivery := deps.NewComponent(root, "webhook-delivery")
	delivery.Go(func(ctx context.Context) error {
		err := relayOutbox.Run(ctx)
		if errors.Is(err, context.Canceled) {
			return nil
		}

		return fmt.Errorf("webhook outbox: %w", err)
	})

	runner := deps.NewComponent(root, "jobs")
	runner.Go(func(ctx context.Context) error {
		jobsRunner := jobs.NewRunner(store.Get(), time.Now)

		return loop(ctx, time.Minute, jobsRunner.Tick)
	})

	return &Service{Store: store, Cache: cache, HTTP: public}, nil
}

// loop runs a periodic pass until the context ends.
func loop(ctx context.Context, every time.Duration, pass func(context.Context) error) error {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := pass(ctx); err != nil {
				return fmt.Errorf("background: %w", err)
			}
		}
	}
}

// allowPrivate parses the configured private-address allowlist (bad
// entries are skipped, like the reference's filter_map).
func allowPrivate(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err != nil {
			continue
		}

		out = append(out, network)
	}

	return out
}

// Handler builds the public API over an opened store: the production tree
// calls it at start, tests call it directly.
func Handler(
	store *postgres.Store, spec accounts.Spec, cfg HTTP, docsHost, revision string,
) (http.Handler, error) {
	instance, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("webhook instance hostname: %w", err)
	}

	queue, err := webhookOutbox(store, spec.ID, cfg, instance, slog.Default())
	if err != nil {
		return nil, err
	}

	api := handler(store, httpapi.NewMemoryThrottler(), spec, cfg, docsHost, revision, queue)

	public, err := portal.New(cfg.Portal, spec.ID.String(), api)
	if err != nil {
		return nil, fmt.Errorf("http.portal: %w", err)
	}

	return public, nil
}

func handler(
	store *postgres.Store, throttler httpapi.Throttler, spec accounts.Spec, cfg HTTP, docsHost, revision string,
	queue port.WebhookQueue,
) http.Handler {
	clock := time.Now
	publisher := events.Publisher(webhooks.NewPublisher(store, queue, spec.ID, rand.Reader, clock))
	resolver := domain.Resolver(domain.NetResolver{})
	allow := allowPrivate(cfg.WebhookAllowPrivate)

	return httpapi.New(httpapi.Deps{
		SecondFactors: secondfactors.New(store, publisher, rand.Reader, clock),
		Passwords:     passwords.New(store, publisher, rand.Reader, clock),
		Throttle:      throttler,
		Webhooks:      webhooks.New(store, resolver, allow, queue, clock),
		Accounts:      store.Accounts,
		Authn:         authn.New(store, clock),
		Users:         users.New(store, publisher, clock),
		Tokens:        tokens.New(store, publisher, rand.Reader, clock),
		Groups:        groups.New(store, publisher, clock),
		Products: products.New(store.Products, store.Tokens, store.Users,
			publisher, resolver, allow, rand.Reader, clock),
		Policies:       policies.New(store.Policies, store.Products, store.Entitlements, store.Keys, publisher, clock),
		Entitlements:   entitlements.New(store.Entitlements, publisher, clock),
		Keys:           keys.New(store.Keys, store.Policies, store.Products, publisher, clock),
		Spec:           httpapi.Spec{ID: spec.ID, Slug: spec.Slug},
		Revision:       revision,
		DocsHost:       docsHost,
		TrustedProxies: cfg.TrustedProxies,
		Clock:          clock,
	})
}

// lateHandler serves the handler built at start; before that, 503.
type lateHandler struct {
	h atomic.Pointer[http.Handler]
}

func (l *lateHandler) set(h http.Handler) { l.h.Store(&h) }

func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := l.h.Load()
	if h == nil {
		http.Error(w, "starting", http.StatusServiceUnavailable)

		return
	}

	(*h).ServeHTTP(w, r)
}

// webhookOutbox configures the relay and the transaction-aware enqueue facade.
func webhookOutbox(store *postgres.Store, accountID uuid.UUID, cfg HTTP, instance string,
	log *slog.Logger,
) (*outbox.Outbox, error) {
	delivery := whwebhook.NewWorker(store, accountID, domain.NetResolver{},
		allowPrivate(cfg.WebhookAllowPrivate), time.Now)

	relay, err := outbox.New(store.Pool, store.DB.DB, delivery,
		outbox.WithSchema(postgres.Schema), outbox.WithInstanceID(instance),
		outbox.WithListenPool(store.Pool), outbox.WithLogger(log),
		outbox.WithBatchSize(1), outbox.WithMaxAttempts(whwebhook.MaxAttempts),
		outbox.WithRetryBackoff(whwebhook.RetryBackoff),
		outbox.WithRetention(jobs.Day), outbox.WithDeadRetention(jobs.WebhookBacklogDays*jobs.Day),
		outbox.WithDiscardRetention(jobs.Day), outbox.WithCleanupInterval(time.Hour),
		outbox.WithClearPayloadOnComplete(true),
	)
	if err != nil {
		return nil, fmt.Errorf("webhook outbox: %w", err)
	}

	return relay, nil
}
