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
	appautomation "github.com/backplane-labs/backplane-lic/internal/app/automation"
	"github.com/backplane-labs/backplane-lic/internal/app/entitlements"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/groups"
	"github.com/backplane-labs/backplane-lic/internal/app/jobs"
	"github.com/backplane-labs/backplane-lic/internal/app/keys"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/passwords"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/app/search"
	"github.com/backplane-labs/backplane-lic/internal/app/secondfactors"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/app/webhooks"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	bpautomation "github.com/backplane-labs/backplane-lic/internal/infra/automation"
	liccache "github.com/backplane-labs/backplane-lic/internal/infra/cache"
	"github.com/backplane-labs/backplane-lic/internal/infra/console"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/portal"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	whwebhook "github.com/backplane-labs/backplane-lic/internal/infra/webhook"
)

// Service is the built tree plus what main declares on the platform.
type Service struct {
	Console *console.Server
	Store   deps.Dependency[*postgres.Store]
	Cache   deps.Optional[valkey.AsideClient]
	// HTTP is the public JSON:API (the full middleware pipeline: request
	// id, negotiation, authentication, response signing); it answers 503
	// until the tree has started.
	HTTP http.Handler
}

// newCache declares the Valkey dependency; absent unless configured.
func newCache(root backplane.Root[Config], cfg *valkey.Config) deps.Optional[valkey.AsideClient] {
	if cfg == nil {
		return deps.Optional[valkey.AsideClient]{}
	}

	return deps.NewOptional(root, valkey.NewAside(*cfg), deps.Name("cache"))
}

// New builds the tree under root: the store and cache dependencies, and
// a start step that creates the account row from configuration and
// builds the HTTP handler over the started store.
func New(root backplane.Root[Config]) (*Service, error) {
	cfg := root.Config()

	store := deps.NewDependency(root, postgres.New(cfg.Store), deps.Name("store"))
	cache := newCache(root, cfg.Cache)

	id, err := uuid.Parse(cfg.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("account.id: %w", err)
	}

	spec := accounts.Spec{ID: id, Name: cfg.Account.Name, Slug: cfg.Account.Slug}
	late := &lateHandler{}
	contracts := bpautomation.Declare(root)
	activities := bpautomation.NewActivityServer(root)

	brand, public, err := customerFacing(cfg, id.String(), late)
	if err != nil {
		return nil, err
	}

	var relayOutbox *outbox.Outbox

	bootstrap := deps.NewComponent(root, "bootstrap")
	bootstrap.OnStart(func(ctx context.Context) error {
		if _, err := accounts.Bootstrap(ctx, store.Get().Accounts, spec); err != nil {
			return fmt.Errorf("bootstrap: %w", err)
		}

		webhookWorker := whwebhook.NewWorker(store.Get(), spec.ID, domain.NetResolver{},
			allowPrivate(cfg.HTTP.WebhookAllowPrivate), time.Now)

		relay, err := newOutbox(store.Get(), root.Identity().Instance,
			slog.New(xlog.NewSlogHandler(bootstrap.Log())), bpautomation.NewWorker(contracts, webhookWorker))
		if err != nil {
			return err
		}

		relayOutbox = relay

		throttler, cached := newThrottler(cache), newTokenCache(cache)

		decorate := func(publisher events.Publisher) events.Publisher {
			return appautomation.NewPublisher(store.Get(), publisher, relay, contracts,
				cfg.Automation.PortalBase, cfg.Automation.Locale, brand, time.Now)
		}
		publisher := decorate(webhooks.NewPublisher(store.Get(), relay, spec.ID, rand.Reader, time.Now))
		activities.Set(appautomation.NewActivities(store.Get(), licenseScenario(store.Get(), cached, publisher, time.Now),
			spec.ID, time.Now))
		late.set(handler(store.Get(), throttler, cached, spec, cfg.HTTP, cfg.DocsHost, build.Get().Commit, relay, decorate))

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
		publisher := appautomation.NewPublisher(store.Get(),
			webhooks.NewPublisher(store.Get(), relayOutbox, spec.ID, rand.Reader, time.Now),
			relayOutbox, contracts, cfg.Automation.PortalBase, cfg.Automation.Locale, brand, time.Now)
		notifier := licenses.NewNotifier(store.Get().Licenses, publisher, spec.ID, time.Now, store.Get().Do)

		return loop(ctx, time.Minute, notificationPass(jobsRunner, notifier))
	})

	return &Service{Console: newConsole(root, store, id, late), Store: store, Cache: cache, HTTP: public}, nil
}

// customerFacing validates the vendor identity and mounts the portal over
// the public API.
func customerFacing(cfg Config, account string, api http.Handler) (domain.Brand, portal.Handler, error) {
	brand, err := domain.NewBrand(cfg.Brand)
	if err != nil {
		return domain.Brand{}, portal.Handler{}, err //nolint:wrapcheck // the error names its field
	}

	public, err := portal.New(cfg.HTTP.Portal, brand, account, api)
	if err != nil {
		return domain.Brand{}, portal.Handler{}, fmt.Errorf("http.portal: %w", err)
	}

	return brand, public, nil
}

// newTokenCache caches token lookups in Valkey when it is configured.
func newTokenCache(aside deps.Optional[valkey.AsideClient]) tokenCache { //nolint:ireturn // chosen at runtime
	if client, ok := aside.Get(); ok {
		return liccache.NewTokens(client, liccache.TokenTTL)
	}

	return liccache.None{}
}

// newThrottler counts password attempts in Valkey when it is configured,
// so every replica shares the window; otherwise per process.
func newThrottler(cache deps.Optional[valkey.AsideClient]) httpapi.Throttler { //nolint:ireturn // chosen at runtime
	if client, ok := cache.Get(); ok {
		return httpapi.NewValkeyThrottler(client.Client())
	}

	return httpapi.NewMemoryThrottler()
}

func notificationPass(jobsRunner *jobs.Runner, notifier licenses.Notifier) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := jobsRunner.Tick(ctx); err != nil {
			return err //nolint:wrapcheck // scenario error
		}

		// Keygen schedules expiration and check-in notices every ten minutes.
		const notificationMinutes = 10
		if time.Now().UTC().Minute()%notificationMinutes != 0 {
			return nil
		}

		return notifier.Tick(ctx)
	}
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

	api := handler(store, httpapi.NewMemoryThrottler(), liccache.None{}, spec, cfg, docsHost, revision, queue,
		func(publisher events.Publisher) events.Publisher { return publisher })

	public, err := portal.New(cfg.Portal, domain.Brand{Name: domain.DefaultBrandName}, spec.ID.String(), api)
	if err != nil {
		return nil, fmt.Errorf("http.portal: %w", err)
	}

	return public, nil
}

// tokenCache decorates the ports that read or change tokens.
type tokenCache interface {
	Authn(inner authn.Store) authn.Store
	Manage(inner tokens.Store) tokens.Store
	Users(inner users.Store) users.Store
}

func handler(
	store *postgres.Store, throttler httpapi.Throttler, cached tokenCache, spec accounts.Spec, cfg HTTP,
	docsHost, revision string, queue port.WebhookQueue, decorate func(events.Publisher) events.Publisher,
) http.Handler {
	clock := time.Now
	publisher := decorate(webhooks.NewPublisher(store, queue, spec.ID, rand.Reader, clock))
	resolver := domain.Resolver(domain.NetResolver{})
	allow := allowPrivate(cfg.WebhookAllowPrivate)

	return httpapi.New(httpapi.Deps{
		SecondFactors: secondfactors.New(store, publisher, rand.Reader, clock),
		Passwords:     passwords.New(store, publisher, rand.Reader, clock),
		Throttle:      throttler,
		Webhooks:      webhooks.New(store, resolver, allow, queue, clock),
		Search:        search.New(store),
		Accounts:      store.Accounts,
		Authn:         authn.New(cached.Authn(store), clock),
		Users:         users.New(cached.Users(store), publisher, clock),
		Tokens:        tokens.New(cached.Manage(store), publisher, rand.Reader, clock),
		Groups:        groups.New(store, publisher, clock),
		Licenses:      licenseScenario(store, cached, publisher, clock),
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

func licenseScenario(
	store *postgres.Store, cached tokenCache, publisher events.Publisher, clock func() time.Time,
) licenses.Scenario {
	return licenses.New(store.Licenses, store.Licenses, store.Policies, store.Products, store,
		store, store.Entitlements, store.Keys,
		tokens.Manager{Scenario: tokens.New(cached.Manage(store), publisher, rand.Reader, clock)},
		publisher, rand.Reader, clock, store.Do)
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

	return newOutbox(store, instance, log, delivery)
}

func newOutbox(store *postgres.Store, instance string, log *slog.Logger,
	delivery outbox.Publisher,
) (*outbox.Outbox, error) {
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
