package domain

// WebhookEvents is the in-scope webhook event universe: the reference's
// seeded event_types minus the out-of-scope families (releases,
// artifacts, packages, engines, environments, accounts/billing).
// Subscriptions are validated against it (an out-of-scope family name
// is SUBSCRIPTIONS_NOT_ALLOWED here, though the full reference accepts
// it — it never emits those events).
var WebhookEvents = []string{ //nolint:gochecknoglobals // transcribed reference data
	// component.*
	"component.created", "component.updated", "component.deleted",
	// entitlement.*
	"entitlement.created", "entitlement.updated", "entitlement.deleted",
	// group.*
	"group.created", "group.updated", "group.deleted",
	"group.owners.attached", "group.owners.detached",
	// key.*
	"key.created", "key.updated", "key.deleted",
	// license.*
	"license.created", "license.updated", "license.deleted",
	"license.suspended", "license.revoked", "license.reinstated", "license.renewed",
	"license.expired", "license.expiring-soon",
	"license.checked-in", "license.checked-out",
	"license.check-in-required-soon", "license.check-in-overdue",
	"license.validated", "license.validation.succeeded", "license.validation.failed",
	"license.group.updated", "license.owner.updated", "license.policy.updated",
	"license.user.updated", "license.users.attached", "license.users.detached",
	"license.entitlements.attached", "license.entitlements.detached",
	"license.usage.incremented", "license.usage.decremented", "license.usage.reset",
	// machine.*
	"machine.created", "machine.updated", "machine.deleted", "machine.checked-out",
	"machine.heartbeat.ping", "machine.heartbeat.pong", "machine.heartbeat.dead",
	"machine.heartbeat.reset", "machine.heartbeat.resurrected",
	"machine.group.updated", "machine.owner.updated", "machine.proofs.generated",
	// policy.*
	"policy.created", "policy.updated", "policy.deleted",
	"policy.entitlements.attached", "policy.entitlements.detached", "policy.pool.popped",
	// process.*
	"process.created", "process.deleted",
	"process.heartbeat.ping", "process.heartbeat.pong", "process.heartbeat.dead",
	// product.*
	"product.created", "product.updated", "product.deleted",
	// second-factor.*
	"second-factor.created", "second-factor.deleted",
	"second-factor.enabled", "second-factor.disabled",
	// token.*
	"token.generated", "token.regenerated", "token.revoked",
	// user.*
	"user.created", "user.updated", "user.deleted",
	"user.banned", "user.unbanned", "user.password-reset", "user.group.updated",
}

// webhookEventSet indexes the event universe.
var webhookEventSet = func() map[string]struct{} { //nolint:gochecknoglobals // compiled once
	set := make(map[string]struct{}, len(WebhookEvents))
	for _, event := range WebhookEvents {
		set[event] = struct{}{}
	}

	return set
}()

// IsWebhookEvent answers whether the name is a subscribable event.
func IsWebhookEvent(event string) bool {
	_, ok := webhookEventSet[event]

	return ok
}
