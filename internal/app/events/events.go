// Package events is where scenarios report what happened: a Keygen webhook
// event ("license.created", "machine.heartbeat.ping", …) with the resource
// document it carries. A scenario publishes inside its own transaction, so
// the event is written together with the change it describes (the webhook
// outbox) or not at all.
package events

import (
	"context"
	"encoding/json"
)

// Publisher records an event for delivery to the subscribed webhook
// endpoints. payload is the JSON:API document of the resource, rendered as
// the API renders it.
type Publisher interface {
	Publish(ctx context.Context, event string, payload json.RawMessage) error
}

// None publishes nothing: an installation without webhook delivery.
type None struct{}

// Publish implements Publisher.
func (None) Publish(context.Context, string, json.RawMessage) error { return nil }
