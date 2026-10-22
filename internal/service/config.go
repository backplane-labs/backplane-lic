package service

import (
	"github.com/backplane-labs/backplane/pkg/backplane/config"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/backplane-labs/backplane/pkg/backplane/infra/valkey"
)

// Config is the whole configuration: file (BACKPLANE_CONFIG_FILE) < env
// LIC_* (BACKPLANE_* for the SDK block) < Consul KV for Live fields.
type Config struct {
	config.Backplane `json:"backplane"`

	Store postgres.Config `json:"store"`
	Cache valkey.Config   `json:"cache"`

	// Account is the single account of the installation: the row is
	// created at startup from these fields when missing.
	Account Account `json:"account"`

	// HTTP is the public API's runtime section.
	HTTP HTTP `json:"http"`

	// DocsHost overrides the docs origin of error links (the reference
	// derives it from its configured host); empty derives it from the
	// request host.
	DocsHost string `json:"docsHost" schemapb:"description=Docs links origin"` //nolint:tagliatelle // config is camelCase
}

// HTTP holds the public API's runtime settings.
type HTTP struct {
	// TrustedProxies lists the proxy CIDRs (or single IPs) whose
	// X-Forwarded-For is honored when resolving the client address;
	// empty means every request is keyed by its remote address.
	TrustedProxies []string `json:"trustedProxies" schemapb:"description=Proxies"` //nolint:tagliatelle // camelCase
	// WebhookAllowPrivate lists the otherwise-private CIDRs webhook
	// endpoint URLs may resolve to (the self-hosted escape hatch for
	// internal receivers; the reference's
	// KEYGEN_ALLOWED_PRIVATE_ADDRESSES).
	WebhookAllowPrivate []string `json:"webhookAllowPrivate" schemapb:"description=Private webhook CIDRs"` //nolint:tagliatelle,lll // camelCase
}

// Account identifies the configured account; both its id and its slug
// address it on the wire. ID is a string so the configuration schema
// validates it as one; New parses it.
type Account struct {
	ID   string `json:"id"   schemapb:"description=The account's UUID (also its API address)"`
	Slug string `json:"slug" schemapb:"description=The account's slug (also its API address)"`
	Name string `json:"name" schemapb:"description=Display name of the account"`
}
