//nolint:tagliatelle // the Keygen wire format is lowerCamelCase, not snake_case
package licensing

import (
	"context"
	"fmt"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
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

// Deactivate deactivates (deletes) the machine.
func (m *Machine) Deactivate(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("%w: machine", ErrUnbound)
	}

	return m.client.do(ctx, "DELETE", "machines/"+m.ID, nil, jsonapi.Body{}, nil)
}

// Ping sends a heartbeat ping and refreshes the machine from the
// response. A dead machine is resurrected when the policy allows.
func (m *Machine) Ping(ctx context.Context) error {
	if m.client == nil {
		return fmt.Errorf("%w: machine", ErrUnbound)
	}

	return m.client.do(ctx, "POST", "machines/"+m.ID+"/actions/ping", nil, jsonapi.Body{},
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

	components := []Component{}

	err := m.client.do(ctx, "GET", "machines/"+m.ID+"/components", listQuery(), jsonapi.Body{},
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

	processes := []Process{}

	err := m.client.do(ctx, "GET", "machines/"+m.ID+"/processes", listQuery(), jsonapi.Body{},
		func(doc jsonapi.Document) error {
			return decodeProcesses(m.client, doc, &processes)
		})
	if err != nil {
		return nil, err
	}

	return processes, nil
}
