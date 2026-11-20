package service

import (
	"github.com/backplane-labs/backplane/pkg/backplane/config"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/valkey"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/portal"
)

// Config is the whole configuration: file (BACKPLANE_CONFIG_FILE) < env
// LIC_* (BACKPLANE_* for the SDK block) < Consul KV for Live fields.
type Config struct {
	config.Backplane `json:"backplane"`

	Store postgres.Config `json:"store"`
	// Cache is optional: without it tokens are read from the store and
	// the password-attempt windows are kept per process.
	Cache *valkey.Config `json:"cache,omitempty"`

	// Account is the single account of the installation: the row is
	// created at startup from these fields when missing.
	Account Account `json:"account"`

	// Catalog declares products, their policies and named product tokens,
	// reconciled at every start; empty declares nothing.
	Catalog Catalog `json:"catalog,omitempty"`

	// HTTP is the public API's runtime section.
	HTTP HTTP `json:"http"`

	Automation Automation `json:"automation,omitempty"`

	// Brand is the vendor identity customers see: the portal's header,
	// accent color and footer links, and the brand_* data of every message.
	Brand domain.Brand `json:"brand,omitempty"`

	// DocsHost overrides the docs origin of error links (the reference
	// derives it from its configured host); empty derives it from the
	// request host.
	DocsHost string `json:"docsHost,omitempty" schemapb:"description=Docs links origin"` //nolint:tagliatelle,lll // camelCase
}

// HTTP holds the public API's runtime settings.
type HTTP struct {
	// Portal serves the standalone customer SPA on the public HTTP listener.
	Portal portal.Config `json:"portal,omitempty"`
	// TrustedProxies lists the proxy CIDRs (or single IPs) whose
	// X-Forwarded-For is honored when resolving the client address;
	// empty means every request is keyed by its remote address.
	TrustedProxies []string `json:"trustedProxies,omitempty" schemapb:"description=Proxies"` //nolint:tagliatelle,lll // camelCase
	// WebhookAllowPrivate lists the otherwise-private CIDRs webhook
	// endpoint URLs may resolve to (the self-hosted escape hatch for
	// internal receivers; the reference's
	// KEYGEN_ALLOWED_PRIVATE_ADDRESSES).
	WebhookAllowPrivate []string `json:"webhookAllowPrivate,omitempty" schemapb:"description=Private webhook CIDRs"` //nolint:tagliatelle,lll // camelCase
}

// Automation configures links and language, not mail transport.
type Automation struct {
	PortalBase string `json:"portalBase,omitempty" schemapb:"description=Public portal base URL"` //nolint:tagliatelle,lll // config is camelCase
	Locale     string `json:"locale,omitempty"     schemapb:"description=Default message language (en or ru)"`
}

// Account identifies the configured account; both its id and its slug
// address it on the wire. ID is a string so the configuration schema
// validates it as one; New parses it.
type Account struct {
	ID   string `json:"id"   schemapb:"description=The account's UUID (also its API address)"`
	Slug string `json:"slug" schemapb:"description=The account's slug (also its API address)"`
	Name string `json:"name" schemapb:"description=Display name of the account"`
	// Ed25519Seed imports the vendor's signing key when the account row is
	// created; an existing row with another key fails startup.
	Ed25519Seed config.Secret `json:"ed25519_seed,omitempty" schemapb:"description=Hex ed25519 signing seed to import"`
}

// Catalog is the declared catalog. It is read at startup only: token
// values are secrets and never come from Consul.
type Catalog struct {
	Products []CatalogProduct `json:"products,omitempty" schemapb:"description=Declared products"`
}

// CatalogProduct is a declared product with its policies and tokens.
type CatalogProduct struct {
	ID                   string          `json:"id"                              schemapb:"description=UUID"`
	Name                 string          `json:"name"                            schemapb:"description=Name"`
	Code                 string          `json:"code,omitempty"                  schemapb:"description=Code"`
	DistributionStrategy string          `json:"distribution_strategy,omitempty" schemapb:"description=Distribution"`
	URL                  string          `json:"url,omitempty"                   schemapb:"description=URL"`
	Platforms            []string        `json:"platforms,omitempty"             schemapb:"description=Platforms"`
	Metadata             map[string]any  `json:"metadata,omitempty"              schemapb:"description=Metadata"`
	Policies             []CatalogPolicy `json:"policies,omitempty"              schemapb:"description=Policies"`
	Tokens               []CatalogToken  `json:"tokens,omitempty"                schemapb:"description=Tokens"`
}

// CatalogPolicy is a declared policy: its id and its Keygen attributes,
// named as the API names them (maxMachines, expirationStrategy, ...).
type CatalogPolicy struct {
	ID         string         `json:"id"         schemapb:"description=The policy's UUID"`
	Attributes map[string]any `json:"attributes" schemapb:"description=Keygen policy attributes"`
}

// CatalogToken is a named product token of fixed value.
type CatalogToken struct {
	Name        string        `json:"name"                  schemapb:"description=Token name, unique per product"`
	Value       config.Secret `json:"value"                 schemapb:"description=Secret prod-<64 hex>v3"`
	Permissions []string      `json:"permissions,omitempty" schemapb:"description=Product permissions (empty: all)"`
}
