package licensing

import (
	"fmt"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
)

// Component is a hardware component of a machine.
type Component struct {
	// ID is the component UUID.
	ID string `json:"-"`

	// Fingerprint identifies the component.
	Fingerprint string `json:"fingerprint"`

	// Name is the display name.
	Name string `json:"name"`

	// Created/Updated are the resource timestamps.
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	// Metadata is free-form.
	Metadata map[string]string `json:"metadata"`

	// MachineID comes from the component's relationships.
	MachineID string `json:"-"`
}

// decodeComponent decodes a component resource.
func decodeComponent(res jsonapi.Resource, target *Component) error {
	if err := jsonapi.DecodeAttributes(res, target); err != nil {
		return fmt.Errorf("decode component: %w", err)
	}

	target.ID = res.ID
	target.MachineID = res.RelID("machine")

	return nil
}

// decodeComponents decodes a components collection into target.
func decodeComponents(doc jsonapi.Document, target *[]Component) error {
	resources, err := doc.Many()
	if err != nil {
		return fmt.Errorf("decode components: %w", err)
	}

	for _, res := range resources {
		component := Component{}
		if err := decodeComponent(res, &component); err != nil {
			return err
		}

		*target = append(*target, component)
	}

	return nil
}
