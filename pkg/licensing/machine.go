//nolint:tagliatelle // the Keygen wire format is lowerCamelCase, not snake_case
package licensing

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

// defaultHeartbeatDuration is the server's default machine heartbeat
// window when the policy leaves it unset.
const defaultHeartbeatDuration = 600

// Machine is an activated machine of a license.
type Machine struct {
	client *Client

	// ID is the machine UUID.
	ID string `json:"-"`

	// Name is the display name.
	Name string `json:"name"`

	// Fingerprint is the device fingerprint.
	Fingerprint string `json:"fingerprint"`

	// Hostname/Platform/IP describe the device.
	Hostname string `json:"hostname"`
	Platform string `json:"platform"`
	IP       string `json:"ip"`

	// Cores is the reported core count.
	Cores int `json:"cores"`

	// RequireHeartbeat reports that this machine must ping.
	RequireHeartbeat bool `json:"requireHeartbeat"`

	// HeartbeatStatus is NOT_STARTED, ALIVE, DEAD, or RESURRECTED.
	HeartbeatStatus HeartbeatStatus `json:"heartbeatStatus"`

	// HeartbeatDuration is the ping window in seconds.
	HeartbeatDuration int `json:"heartbeatDuration"`

	// LastHeartbeat/NextHeartbeat are the heartbeat timestamps.
	LastHeartbeat *time.Time `json:"lastHeartbeat"`
	NextHeartbeat *time.Time `json:"nextHeartbeat"`

	// LastCheckOut is the last machine-file checkout.
	LastCheckOut *time.Time `json:"lastCheckOut"`

	// Created/Updated are the resource timestamps.
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	// Metadata is free-form.
	Metadata Metadata `json:"metadata"`

	// LicenseID comes from the machine's relationships.
	LicenseID string `json:"-"`
}

// bind attaches the client and relationship identifiers.
func (m *Machine) bind(client *Client, res jsonapi.Resource) {
	m.client = client
	m.ID = res.ID
	m.LicenseID = res.RelID("license")
}

// decodeMachine decodes a machine document into target.
func decodeMachine(client *Client, doc jsonapi.Document, target *Machine) error {
	res, err := doc.One()
	if err != nil {
		return fmt.Errorf("decode machine: %w", err)
	}

	if err := jsonapi.DecodeAttributes(res, target); err != nil {
		return fmt.Errorf("decode machine: %w", err)
	}

	target.bind(client, res)

	return nil
}

// decodeMachines decodes a machines collection into target.
func decodeMachines(client *Client, doc jsonapi.Document, target *[]Machine) error {
	resources, err := doc.Many()
	if err != nil {
		return fmt.Errorf("decode machines: %w", err)
	}

	for _, res := range resources {
		machine := Machine{}
		if err := jsonapi.DecodeAttributes(res, &machine); err != nil {
			return fmt.Errorf("decode machines: %w", err)
		}

		machine.bind(client, res)

		*target = append(*target, machine)
	}

	return nil
}

// Machine fetches a machine by UUID or fingerprint through the client's
// credentials (admin/product token, not a license key). It calls
// GET /v1/accounts/{account}/machines/{id} (cloud) or
// GET /v1/machines/{id} (self-hosted). Keygen allows id or fingerprint
// in the path.
//
// The identifier is validated and the path segment is escaped with
// url.PathEscape so metacharacters in the input cannot change the
// request path.
func (c *Client) Machine(ctx context.Context, idOrFingerprint string) (*Machine, error) {
	path, err := resourcePath("machines", idOrFingerprint)
	if err != nil {
		return nil, err
	}

	machine := &Machine{}

	err = c.do(ctx, "GET", path, nil, jsonapi.Body{}, func(doc jsonapi.Document) error {
		return decodeMachine(c, doc, machine)
	})
	if err != nil {
		return nil, err
	}

	return machine, nil
}

// ErrInvalidIdentifier reports a resource id (UUID) or machine
// fingerprint that is empty, too long or has characters outside
// [A-Za-z0-9_:-]. Such a value is refused before any request is made.
var ErrInvalidIdentifier = errors.New("identifier is invalid")

// maxIdentifierLen is the longest accepted id or fingerprint (Keygen
// bounds fingerprints at 255 characters).
const maxIdentifierLen = 255

// validIdentifier reports whether id is a UUID or a fingerprint: 1 to
// 255 characters from [A-Za-z0-9_:-]. Dots and slashes are excluded so
// the value can never be a path traversal segment.
func validIdentifier(id string) bool {
	if id == "" || len(id) > maxIdentifierLen {
		return false
	}

	for _, c := range id {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-', c == '_', c == ':':
		default:
			return false
		}
	}

	return true
}

// resourcePath builds "kind/id[/tail...]" for the API, refusing an
// invalid id and escaping it as one path segment.
func resourcePath(kind, id string, tail ...string) (string, error) {
	if !validIdentifier(id) {
		return "", fmt.Errorf("%w: %s %q", ErrInvalidIdentifier, kind, id)
	}

	return strings.Join(append([]string{kind, url.PathEscape(id)}, tail...), "/"), nil
}

// Deactivate deactivates (deletes) the machine.
func (m *Machine) Deactivate(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("%w: machine", ErrUnbound)
	}

	path, err := resourcePath("machines", m.ID)
	if err != nil {
		return err
	}

	return m.client.do(ctx, "DELETE", path, nil, jsonapi.Body{}, nil)
}

// Ping sends a heartbeat ping and refreshes the machine from the
// response. A dead machine is resurrected when the policy allows.
func (m *Machine) Ping(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("%w: machine", ErrUnbound)
	}

	path, err := resourcePath("machines", m.ID, "actions", "ping")
	if err != nil {
		return err
	}

	return m.client.do(ctx, "POST", path, nil, jsonapi.Body{},
		func(doc jsonapi.Document) error {
			return decodeMachine(m.client, doc, m)
		})
}

// Monitor keeps the machine's heartbeat alive: it pings immediately,
// then again roughly every heartbeat window (minus a safety margin,
// with jitter and retries) until ctx is done, at which point it returns
// nil. It returns the first unrecoverable ping error otherwise.
func (m *Machine) Monitor(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("%w: machine", ErrUnbound)
	}

	interval := m.HeartbeatDuration
	if interval <= 0 {
		interval = defaultHeartbeatDuration
	}

	return monitorHeartbeat(ctx, time.Duration(interval)*time.Second, m.client.log, m.Ping)
}

// Components lists up to 100 components of the machine.
func (m *Machine) Components(ctx context.Context) ([]Component, error) {
	if m.client == nil {
		return nil, fmt.Errorf("%w: machine", ErrUnbound)
	}

	path, err := resourcePath("machines", m.ID, "components")
	if err != nil {
		return nil, err
	}

	components := []Component{}

	err = m.client.do(ctx, "GET", path, listQuery(), jsonapi.Body{},
		func(doc jsonapi.Document) error {
			return decodeComponents(doc, &components)
		})
	if err != nil {
		return nil, err
	}

	return components, nil
}

// Spawn creates a process for the machine with the given pid. Use the
// returned Process's Monitor to keep it alive.
func (m *Machine) Spawn(ctx context.Context, pid string) (*Process, error) {
	if m.client == nil {
		return nil, fmt.Errorf("%w: machine", ErrUnbound)
	}

	body := jsonapi.Body{
		Type:       "processes",
		Attributes: processAttrs{Pid: pid},
		Relationships: map[string]jsonapi.Relationship{
			"machine": {Data: jsonapi.Identifier{Type: "machines", ID: m.ID}},
		},
	}

	process := &Process{}

	err := m.client.do(ctx, "POST", "processes", nil, body, func(doc jsonapi.Document) error {
		return decodeProcess(m.client, doc, process)
	})
	if err != nil {
		return nil, err
	}

	return process, nil
}

// Processes lists up to 100 processes of the machine.
func (m *Machine) Processes(ctx context.Context) ([]Process, error) {
	if m.client == nil {
		return nil, fmt.Errorf("%w: machine", ErrUnbound)
	}

	path, err := resourcePath("machines", m.ID, "processes")
	if err != nil {
		return nil, err
	}

	processes := []Process{}

	err = m.client.do(ctx, "GET", path, listQuery(), jsonapi.Body{},
		func(doc jsonapi.Document) error {
			return decodeProcesses(m.client, doc, &processes)
		})
	if err != nil {
		return nil, err
	}

	return processes, nil
}

// ReportUsage sends a usage report for the machine.
// POST /v1/accounts/{account}/machines/{id}/actions/report-usage
// Body: {"meta": {"report": <Report>}}.
// Answer: {"meta": {"ack": {"epoch":..., "seq":..., "hash":"..."}}}.
func (m *Machine) ReportUsage(ctx context.Context, report *usage.Report) (*usage.Acked, error) {
	if m.client == nil {
		return nil, fmt.Errorf("%w: machine", ErrUnbound)
	}

	path, err := resourcePath("machines", m.ID, "actions", "report-usage")
	if err != nil {
		return nil, err
	}

	body := jsonapi.Body{
		Meta: map[string]any{"report": report},
	}

	var ack usage.Acked

	err = m.client.do(ctx, "POST", path, nil, body,
		func(doc jsonapi.Document) error {
			type ackMeta struct {
				Ack usage.Acked `json:"ack"`
			}

			var meta ackMeta
			if err := doc.DecodeMeta(&meta); err != nil {
				return fmt.Errorf("decode ack: %w", err)
			}

			ack = meta.Ack

			return nil
		})
	if err != nil {
		return nil, err
	}

	return &ack, nil
}

// Usage fetches usage events for the machine.
// GET /v1/accounts/{account}/machines/{id}/usage?after=<seq>&limit=<n>.
func (m *Machine) Usage(ctx context.Context, afterSeq uint64, limit int) (*usage.Response, error) {
	if m.client == nil {
		return nil, fmt.Errorf("%w: machine", ErrUnbound)
	}

	path, err := resourcePath("machines", m.ID, "usage")
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("after", strconv.FormatUint(afterSeq, 10))
	query.Set("limit", strconv.Itoa(limit))

	var resp usage.Response

	err = m.client.do(ctx, "GET", path, query, jsonapi.Body{},
		func(doc jsonapi.Document) error {
			if err := doc.DecodeMeta(&resp); err != nil {
				return fmt.Errorf("decode usage response: %w", err)
			}

			return nil
		})
	if err != nil {
		return nil, err
	}

	return &resp, nil
}
