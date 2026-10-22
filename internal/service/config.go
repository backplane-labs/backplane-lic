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
}

// Account identifies the configured account; both its id and its slug
// address it on the wire. ID is a string so the configuration schema
// validates it as one; New parses it.
type Account struct {
	ID   string `json:"id"   schemapb:"description=The account's UUID (also its API address)"`
	Slug string `json:"slug" schemapb:"description=The account's slug (also its API address)"`
	Name string `json:"name" schemapb:"description=Display name of the account"`
}
