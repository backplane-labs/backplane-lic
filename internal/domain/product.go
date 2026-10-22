package domain

import (
	"time"

	"github.com/google/uuid"
)

// The product's distribution strategies (Product#distribution_strategy).
// LICENSED is the default and the null equivalent on the wire.
const (
	DistributionLicensed = "LICENSED"
	DistributionOpen     = "OPEN"
	DistributionClosed   = "CLOSED"
)

// ProductDistributionStrategies is the allowed set.
func ProductDistributionStrategies() []string {
	return []string{DistributionLicensed, DistributionOpen, DistributionClosed}
}

// ReservedProductCodes: the two route-segment-colliding aliases.
var ReservedProductCodes = []string{"actions", "action"} //nolint:gochecknoglobals // transcribed reference data

// Product is an account's product: the top of the licensing tree.
type Product struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	Name                 string
	Code                 string // empty is null
	DistributionStrategy string // LICENSED when unset
	URL                  string
	Platforms            []string // nil is null
	Metadata             map[string]any
	CreatedAt, UpdatedAt time.Time
}

// Open reports the OPEN distribution strategy (visible to every
// authenticated bearer).
func (p Product) Open() bool { return p.DistributionStrategy == DistributionOpen }

// Licensed reports the LICENSED strategy (the default).
func (p Product) Licensed() bool {
	return p.DistributionStrategy == "" || p.DistributionStrategy == DistributionLicensed
}

// Closed reports the CLOSED strategy.
func (p Product) Closed() bool { return p.DistributionStrategy == DistributionClosed }
