package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/catalog"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/policies"
	"github.com/backplane-labs/backplane-lic/internal/app/products"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

// catalogSpec turns the configuration into the declared catalog, so a
// malformed declaration stops startup before anything is written.
func catalogSpec(cfg Catalog) (catalog.Spec, error) {
	spec := catalog.Spec{Products: make([]catalog.Product, 0, len(cfg.Products))}

	for i := range cfg.Products {
		p := &cfg.Products[i]

		id, err := uuid.Parse(p.ID)
		if err != nil {
			return catalog.Spec{}, fmt.Errorf("catalog.products[%d].id: %w", i, err)
		}

		product := catalog.Product{Input: products.Input{
			ID:     id,
			NameOn: true, Name: &p.Name,
			CodeOn: true, Code: optional(p.Code),
			DistributionOn: true, Distribution: optional(p.DistributionStrategy),
			URLOn: true, URL: optional(p.URL),
			PlatformsOn: true, Platforms: p.Platforms,
			MetadataOn: true, Metadata: p.Metadata,
		}}

		for j, policy := range p.Policies {
			policyID, err := uuid.Parse(policy.ID)
			if err != nil {
				return catalog.Spec{}, fmt.Errorf("catalog.products[%d].policies[%d].id: %w", i, j, err)
			}

			in, err := httpapi.PolicyAttributes(policy.Attributes, id)
			if err != nil {
				return catalog.Spec{}, fmt.Errorf("catalog.products[%d].policies[%d].attributes: %w", i, j, err)
			}

			in.ID = policyID
			product.Policies = append(product.Policies, in)
		}

		for _, token := range p.Tokens {
			product.Tokens = append(product.Tokens, catalog.Token{
				Name: token.Name, Value: token.Value.Reveal(), Permissions: token.Permissions,
			})
		}

		spec.Products = append(spec.Products, product)
	}

	if err := spec.Validate(); err != nil {
		return catalog.Spec{}, fmt.Errorf("catalog: %w", err)
	}

	return spec, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// newCatalog builds the reconciler over the scenarios that own the
// declared objects.
func newCatalog(store *postgres.Store, publisher events.Publisher, cfg HTTP) catalog.Reconciler {
	return catalog.New(store,
		products.New(store.Products, store.Tokens, store.Users,
			publisher, domain.NetResolver{}, allowPrivate(cfg.WebhookAllowPrivate), rand.Reader, time.Now),
		policies.New(store.Policies, store.Products, store.Entitlements, store.Keys, publisher, time.Now),
		publisher)
}

// lastCatalog keeps the last reconcile for the console.
type lastCatalog struct {
	mu     sync.Mutex
	report catalog.Report
}

// Report is the last reconcile; zero before the first.
func (l *lastCatalog) Report() catalog.Report {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.report
}

func (l *lastCatalog) set(report catalog.Report) {
	l.mu.Lock()
	l.report = report
	l.mu.Unlock()
}

// specs parses the configured account and catalog.
func specs(cfg Config) (accounts.Spec, catalog.Spec, error) {
	account, err := accountSpec(cfg.Account)
	if err != nil {
		return accounts.Spec{}, catalog.Spec{}, err
	}

	declared, err := catalogSpec(cfg.Catalog)

	return account, declared, err
}

// seed creates the account row when missing, applies the declared
// catalog and reports what the configuration does not declare; nothing
// undeclared is deleted.
func seed(
	ctx context.Context, store *postgres.Store, spec accounts.Spec, declared catalog.Spec,
	publisher events.Publisher, cfg HTTP, cached tokenCache, last *lastCatalog, log *slog.Logger,
) error {
	account, err := accounts.Bootstrap(ctx, store.Accounts, spec)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	report, err := newCatalog(store, publisher, cfg).Reconcile(ctx, account, declared)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}

	last.set(report)

	if report.TokensMoved {
		if err := cached.Invalidate(ctx, account.ID); err != nil {
			return fmt.Errorf("catalog: token cache: %w", err)
		}
	}

	if report.Written > 0 {
		log.InfoContext(ctx, "catalog reconciled", slog.Int("written", report.Written))
	}

	if len(report.Undeclared) > 0 {
		log.WarnContext(ctx, "catalog: objects not declared in the configuration are kept",
			slog.Int64("products", report.Undeclared["product"]), slog.Int64("policies", report.Undeclared["policy"]),
			slog.Int64("tokens", report.Undeclared["token"]))
	}

	return nil
}
