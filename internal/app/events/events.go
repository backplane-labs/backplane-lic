// Package events is where scenarios report what happened: a Keygen webhook
// event ("license.created", "machine.heartbeat.ping", …) with the resource
// document it carries. A scenario publishes inside its own transaction, so
// the event is written together with the change it describes (the webhook
// outbox) or not at all.
package events

import "context"

// Publisher records an event for delivery to the subscribed webhook
// endpoints. resource is the domain value the event is about (a
// domain.License, domain.Machine, …); the implementation renders it with
// the same JSON:API renderer the API uses (a type switch over the domain
// types it knows).
type Publisher interface {
	Publish(ctx context.Context, event string, resource any) error
}

// None publishes nothing: an installation without webhook delivery.
type None struct{}

// Publish implements Publisher.
func (None) Publish(context.Context, string, any) error { return nil }
