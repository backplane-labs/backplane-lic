//nolint:tagliatelle // the Keygen wire format is lowerCamelCase, not snake_case
package licensing

import (
	"context"
	"fmt"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
)

// Process is a process of a machine, kept alive by heartbeat pings.
type Process struct {
	client *Client

	// ID is the process UUID.
	ID string `json:"-"`

	// Pid is the caller-supplied process identifier.
	Pid string `json:"pid"`

	// Status is ALIVE or DEAD (RESURRECTED in a resurrecting ping's
	// response).
	Status ProcessStatus `json:"status"`

	// Interval is the required ping interval in seconds.
	Interval int `json:"interval"`

	// LastHeartbeat/NextHeartbeat are the heartbeat timestamps.
	LastHeartbeat *time.Time `json:"lastHeartbeat"`
	NextHeartbeat *time.Time `json:"nextHeartbeat"`

	// Created/Updated are the resource timestamps.
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`

	// Metadata is free-form.
	Metadata Metadata `json:"metadata"`

	// MachineID comes from the process's relationships.
	MachineID string `json:"-"`
}

// processAttrs is the attributes object of POST /processes.
type processAttrs struct {
	Pid string `json:"pid"`
}

// bind attaches the client and relationship identifiers.
func (p *Process) bind(client *Client, res jsonapi.Resource) {
	p.client = client
	p.ID = res.ID
	p.MachineID = res.RelID("machine")
}

// decodeProcess decodes a process document into target.
func decodeProcess(client *Client, doc jsonapi.Document, target *Process) error {
	res, err := doc.One()
	if err != nil {
		return fmt.Errorf("decode process: %w", err)
	}

	if err := jsonapi.DecodeAttributes(res, target); err != nil {
		return fmt.Errorf("decode process: %w", err)
	}

	target.bind(client, res)

	return nil
}

// decodeProcesses decodes a processes collection into target.
func decodeProcesses(client *Client, doc jsonapi.Document, target *[]Process) error {
	resources, err := doc.Many()
	if err != nil {
		return fmt.Errorf("decode processes: %w", err)
	}

	for _, res := range resources {
		process := Process{}
		if err := jsonapi.DecodeAttributes(res, &process); err != nil {
			return fmt.Errorf("decode processes: %w", err)
		}

		process.bind(client, res)

		*target = append(*target, process)
	}

	return nil
}

// Kill deletes the process.
func (p *Process) Kill(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("%w: process", ErrUnbound)
	}

	return p.client.do(ctx, "DELETE", "processes/"+p.ID, nil, jsonapi.Body{}, nil)
}

// Ping sends a heartbeat ping and refreshes the process from the
// response.
func (p *Process) Ping(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("%w: process", ErrUnbound)
	}

	return p.client.do(ctx, "POST", "processes/"+p.ID+"/actions/ping", nil, jsonapi.Body{},
		func(doc jsonapi.Document) error {
			return decodeProcess(p.client, doc, p)
		})
}

// Monitor keeps the process's heartbeat alive, like Machine.Monitor but
// at the process's own interval.
func (p *Process) Monitor(ctx context.Context) error {
	if p.client == nil {
		return fmt.Errorf("%w: process", ErrUnbound)
	}

	interval := p.Interval
	if interval <= 0 {
		interval = defaultHeartbeatDuration
	}

	return monitorHeartbeat(ctx, time.Duration(interval)*time.Second, p.client.log, p.Ping)
}
