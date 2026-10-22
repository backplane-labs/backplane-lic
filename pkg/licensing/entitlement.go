package licensing

import (
	"fmt"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
)

// Entitlement is a license's entitlement (a feature flag with an
// expiration date).
type Entitlement struct {
	// ID is the entitlement UUID.
	ID string `json:"-"`

	// Code identifies the entitlement, e.g. "premium-features".
	Code string `json:"code"`

	// Name is the display name.
	Name string `json:"name"`

	// Expiry is the entitlement's expiration, null = perpetual.
	Expiry *time.Time `json:"expiry"`

	// Created/Updated are the resource timestamps.
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	// Metadata is free-form.
	Metadata Metadata `json:"metadata"`
}

// decodeEntitlement decodes an entitlement resource.
func decodeEntitlement(res jsonapi.Resource, target *Entitlement) error {
	if err := jsonapi.DecodeAttributes(res, target); err != nil {
		return fmt.Errorf("decode entitlement: %w", err)
	}

	target.ID = res.ID

	return nil
}

// decodeEntitlements decodes an entitlements collection into target.
func decodeEntitlements(doc jsonapi.Document, target *[]Entitlement) error {
	resources, err := doc.Many()
	if err != nil {
		return fmt.Errorf("decode entitlements: %w", err)
	}

	for _, res := range resources {
		entitlement := Entitlement{}
		if err := decodeEntitlement(res, &entitlement); err != nil {
			return err
		}

		*target = append(*target, entitlement)
	}

	return nil
}
