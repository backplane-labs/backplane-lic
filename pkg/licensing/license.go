//nolint:tagliatelle // the Keygen wire format is lowerCamelCase, not snake_case
package licensing

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
)

// License is a Keygen license. A License with a bound client (from
// Client.Validate, Client.Me, or a decoded dataset bound via
// Client.Bind) can act: validate, activate machines, check out files.
type License struct {
	client *Client

	// ID is the license UUID.
	ID string `json:"-"`

	// Name is the display name.
	Name string `json:"name"`

	// Key is the license key.
	Key string `json:"key"`

	// Expiry is null for perpetual licenses.
	Expiry *time.Time `json:"expiry"`

	// Status is ACTIVE, INACTIVE, EXPIRING, EXPIRED, SUSPENDED, or BANNED.
	Status string `json:"status"`

	// Uses is the current usage counter.
	Uses int `json:"uses"`

	// Suspended reports manual suspension.
	Suspended bool `json:"suspended"`

	// Scheme is the key signing scheme, empty for plain keys.
	Scheme Scheme `json:"scheme"`

	// Strict reports that the policy requires machine tracking.
	Strict bool `json:"strict"`

	// Floating reports multi-seat policies (node-locked when false).
	Floating bool `json:"floating"`

	// Protected reports protection from non-admin mutation.
	Protected bool `json:"protected"`

	// RequireHeartbeat reports that machines must ping.
	RequireHeartbeat bool `json:"requireHeartbeat"`

	// RequireCheckIn reports periodic license check-ins.
	RequireCheckIn bool `json:"requireCheckIn"`

	// MaxMachines/MaxCores/MaxProcesses are the policy limits (null =
	// unlimited).
	MaxMachines  *int `json:"maxMachines"`
	MaxCores     *int `json:"maxCores"`
	MaxProcesses *int `json:"maxProcesses"`

	// Version is the last validated version.
	Version string `json:"version"`

	// LastValidated/LastCheckIn/LastCheckOut are the activity timestamps.
	LastValidated *time.Time `json:"lastValidated"`
	LastCheckIn   *time.Time `json:"lastCheckIn"`
	LastCheckOut  *time.Time `json:"lastCheckOut"`

	// Created/Updated are the resource timestamps.
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	// Metadata is free-form.
	Metadata Metadata `json:"metadata"`

	// PolicyID/ProductID/OwnerID come from the resource's relationships.
	PolicyID  string `json:"-"`
	ProductID string `json:"-"`
	OwnerID   string `json:"-"`

	// Validation is the result of the last Validate call on this value.
	Validation *ValidationResult `json:"-"`
}

// bind attaches the client and the relationship identifiers of a decoded
// resource.
func (l *License) bind(client *Client, res jsonapi.Resource) {
	l.client = client
	l.ID = res.ID
	l.PolicyID = res.RelID("policy")
	l.ProductID = res.RelID("product")
	l.OwnerID = res.RelID("owner")
}

// Me fetches the current license (GET /v1/me). It requires license-key
// authentication.
func (c *Client) Me(ctx context.Context) (*License, error) {
	license := &License{}

	err := c.do(ctx, "GET", "me", nil, jsonapi.Body{}, func(doc jsonapi.Document) error {
		res, err := doc.One()
		if err != nil {
			return fmt.Errorf("decode license (me): %w", err)
		}

		if err := jsonapi.DecodeAttributes(res, license); err != nil {
			return fmt.Errorf("decode license (me): %w", err)
		}

		license.bind(c, res)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return license, nil
}

// Validate performs a license validation, optionally scoped to a machine
// fingerprint and component fingerprints. It fetches the license first,
// then runs the scoped validation. The returned License carries the full
// Validation result; when the code is not VALID the error wraps one of
// the validation sentinels (ErrNotActivated, ErrExpired, ...).
func (c *Client) Validate(ctx context.Context, options ...ValidationOption) (*License, error) {
	license, err := c.Me(ctx)
	if err != nil {
		return nil, err
	}

	if err := license.Validate(ctx, options...); err != nil {
		return license, err
	}

	return license, nil
}

// Validate re-validates the license. See Client.Validate.
func (l *License) Validate(ctx context.Context, options ...ValidationOption) error {
	if l.client == nil {
		return fmt.Errorf("%w: license", ErrUnbound)
	}

	scope := ValidationOptions{}
	scope.apply(options...)

	body := jsonapi.Body{Meta: validateMeta{Scope: scope.wire(l.client.cfg.Product)}}

	decode := func(doc jsonapi.Document) error {
		res, err := doc.One()
		if err != nil {
			return fmt.Errorf("decode validation: %w", err)
		}

		if err := jsonapi.DecodeAttributes(res, l); err != nil {
			return fmt.Errorf("decode validation: %w", err)
		}

		l.bind(l.client, res)

		result := &ValidationResult{}
		if err := doc.DecodeMeta(result); err != nil {
			return fmt.Errorf("decode validation meta: %w", err)
		}

		l.Validation = result

		return nil
	}

	if err := l.client.do(ctx, "POST", "licenses/"+l.ID+"/actions/validate", nil, body, decode); err != nil {
		return err
	}

	return l.Validation.Err()
}

// validateMeta is the validate request/response meta object.
type validateMeta struct {
	Scope *validateScope `json:"scope,omitempty"`
}

// validateScope is the scope the client sends; the server echoes it back
// (with extra keys that are ignored here).
type validateScope struct {
	Fingerprint string   `json:"fingerprint,omitempty"`
	Components  []string `json:"components,omitempty"`
	Product     string   `json:"product,omitempty"`
}

// ActivationOptions collect the optional parts of a machine activation.
type ActivationOptions struct {
	Name       string
	Hostname   string
	Platform   string
	Cores      int
	Metadata   map[string]string
	Components []Component
}

// ValidationOption configures a license validation scope.
type ValidationOption func(*ValidationOptions)

// ValidationOptions is the validation scope under construction.
type ValidationOptions struct {
	Fingerprint string
	Components  []string
}

func (o *ValidationOptions) apply(options ...ValidationOption) {
	for _, option := range options {
		option(o)
	}
}

// wire renders the scope, adding the configured product when set. The
// scope object is always sent, like the reference clients do.
func (o ValidationOptions) wire(product string) *validateScope {
	return &validateScope{
		Fingerprint: o.Fingerprint,
		Components:  o.Components,
		Product:     product,
	}
}

// WithFingerprint scopes the validation to a machine fingerprint.
func WithFingerprint(fingerprint string) ValidationOption {
	return func(o *ValidationOptions) { o.Fingerprint = fingerprint }
}

// WithComponents scopes the validation to component fingerprints; it
// requires WithFingerprint.
func WithComponents(fingerprints ...string) ValidationOption {
	return func(o *ValidationOptions) { o.Components = append(o.Components, fingerprints...) }
}

// ActivationOption configures a machine activation.
type ActivationOption func(*ActivationOptions)

// apply folds the options into o.
func (o *ActivationOptions) apply(options ...ActivationOption) {
	for _, option := range options {
		option(o)
	}
}

// WithMachineName names the machine.
func WithMachineName(name string) ActivationOption {
	return func(o *ActivationOptions) { o.Name = name }
}

// WithMachineHostname overrides the hostname (default: os.Hostname).
func WithMachineHostname(hostname string) ActivationOption {
	return func(o *ActivationOptions) { o.Hostname = hostname }
}

// WithMachinePlatform overrides the platform (default: os/arch).
func WithMachinePlatform(platform string) ActivationOption {
	return func(o *ActivationOptions) { o.Platform = platform }
}

// WithMachineCores overrides the core count (default: NumCPU).
func WithMachineCores(cores int) ActivationOption {
	return func(o *ActivationOptions) { o.Cores = cores }
}

// WithMachineMetadata attaches metadata to the machine.
func WithMachineMetadata(metadata map[string]string) ActivationOption {
	return func(o *ActivationOptions) { o.Metadata = metadata }
}

// WithActivationComponents activates hardware components together with
// the machine.
func WithActivationComponents(components ...Component) ActivationOption {
	return func(o *ActivationOptions) { o.Components = append(o.Components, components...) }
}

// machineActivationAttrs is the attributes object of POST /machines.
type machineActivationAttrs struct {
	Fingerprint string            `json:"fingerprint"`
	Name        string            `json:"name,omitempty"`
	Hostname    string            `json:"hostname,omitempty"`
	Platform    string            `json:"platform,omitempty"`
	Cores       int               `json:"cores,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Activate activates a machine for the license. The machine's hostname,
// platform, and core count default to the local system's.
func (l *License) Activate(
	ctx context.Context,
	fingerprint string,
	options ...ActivationOption,
) (*Machine, error) {
	if l.client == nil {
		return nil, fmt.Errorf("%w: license", ErrUnbound)
	}

	opts := ActivationOptions{}
	opts.apply(options...)

	if opts.Hostname == "" {
		opts.Hostname, _ = os.Hostname()
	}

	if opts.Platform == "" {
		opts.Platform = runtime.GOOS + "/" + runtime.GOARCH
	}

	if opts.Cores == 0 {
		opts.Cores = runtime.NumCPU()
	}

	body := jsonapi.Body{
		Type: "machines",
		Attributes: machineActivationAttrs{
			Fingerprint: fingerprint,
			Name:        opts.Name,
			Hostname:    opts.Hostname,
			Platform:    opts.Platform,
			Cores:       opts.Cores,
			Metadata:    opts.Metadata,
		},
		Relationships: map[string]jsonapi.Relationship{
			"license": {Data: &jsonapi.Identifier{Type: "licenses", ID: l.ID}},
		},
	}

	if len(opts.Components) > 0 {
		body.Relationships["components"] = componentsRelationship(opts.Components)
	}

	machine := &Machine{}

	err := l.client.do(ctx, "POST", "machines", nil, body, func(doc jsonapi.Document) error {
		return decodeMachine(l.client, doc, machine)
	})
	if err != nil {
		return nil, err
	}

	return machine, nil
}

// componentResource is a component in an activation's embedded
// relationship data.
type componentResource struct {
	Type       string                 `json:"type"`
	Attributes componentResourceAttrs `json:"attributes"`
}

// componentResourceAttrs is the attribute subset sent on activation.
type componentResourceAttrs struct {
	Fingerprint string            `json:"fingerprint"`
	Name        string            `json:"name,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// componentsRelationship renders activation components as embedded
// relationship data.
func componentsRelationship(components []Component) jsonapi.Relationship {
	data := make([]componentResource, 0, len(components))

	for _, component := range components {
		data = append(data, componentResource{
			Type: "components",
			Attributes: componentResourceAttrs{
				Fingerprint: component.Fingerprint,
				Name:        component.Name,
				Metadata:    component.Metadata,
			},
		})
	}

	return jsonapi.Relationship{Data: data}
}

// Deactivate deactivates a machine by UUID or fingerprint.
func (l *License) Deactivate(ctx context.Context, machineID string) error {
	if l.client == nil {
		return fmt.Errorf("%w: license", ErrUnbound)
	}

	return l.client.do(ctx, "DELETE", "machines/"+machineID, nil, jsonapi.Body{}, nil)
}

// Machine fetches a machine of the license by UUID or fingerprint.
func (l *License) Machine(ctx context.Context, machineID string) (*Machine, error) {
	if l.client == nil {
		return nil, fmt.Errorf("%w: license", ErrUnbound)
	}

	machine := &Machine{}

	err := l.client.do(ctx, "GET", "machines/"+machineID, nil, jsonapi.Body{}, func(doc jsonapi.Document) error {
		return decodeMachine(l.client, doc, machine)
	})
	if err != nil {
		return nil, err
	}

	return machine, nil
}

// Machines lists up to 100 machines of the license.
func (l *License) Machines(ctx context.Context) ([]Machine, error) {
	if l.client == nil {
		return nil, fmt.Errorf("%w: license", ErrUnbound)
	}

	machines := []Machine{}

	err := l.client.do(ctx, "GET", "licenses/"+l.ID+"/machines", listQuery(), jsonapi.Body{},
		func(doc jsonapi.Document) error {
			return decodeMachines(l.client, doc, &machines)
		})
	if err != nil {
		return nil, err
	}

	return machines, nil
}

// Entitlements lists up to 100 entitlements of the license.
func (l *License) Entitlements(ctx context.Context) ([]Entitlement, error) {
	if l.client == nil {
		return nil, fmt.Errorf("%w: license", ErrUnbound)
	}

	entitlements := []Entitlement{}

	err := l.client.do(ctx, "GET", "licenses/"+l.ID+"/entitlements", listQuery(), jsonapi.Body{},
		func(doc jsonapi.Document) error {
			return decodeEntitlements(doc, &entitlements)
		})
	if err != nil {
		return nil, err
	}

	return entitlements, nil
}

// VerifyKey cryptographically verifies the license's signed key offline
// and returns the embedded dataset (JSON). It requires Config.PublicKey.
func (l *License) VerifyKey() ([]byte, error) {
	if l.client == nil {
		return nil, fmt.Errorf("%w: license", ErrUnbound)
	}

	if l.Scheme == "" {
		return nil, fmt.Errorf("%w: license key is not signed", ErrSchemeNotSupported)
	}

	if l.Key == "" {
		return nil, ErrLicenseKeyMissing
	}

	return l.client.verifier.VerifyLicenseKey(l.Scheme, l.Key)
}

// listQuery is the limit=100 query used by list endpoints.
func listQuery() url.Values {
	query := url.Values{}
	query.Set("limit", "100")

	return query
}
