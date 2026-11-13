// Package access is the authorization foundation: the CE permission sets
// per role, the effective-permission intersection with wildcards, the
// principal in the request context and the Require helper scenarios call.
package access

import "github.com/backplane-labs/backplane-lic/internal/domain"

// The permissions the role sets repeat.
const (
	permAccountRead           = "account.read"
	permArchRead              = "arch.read"
	permArtifactRead          = "artifact.read"
	permChannelRead           = "channel.read"
	permComponentCreate       = "component.create"
	permComponentDelete       = "component.delete"
	permComponentRead         = "component.read"
	permComponentUpdate       = "component.update"
	permConstraintRead        = "constraint.read"
	permEngineRead            = "engine.read"
	permEntitlementRead       = "entitlement.read"
	permGroupLicensesRead     = "group.licenses.read"
	permGroupMachinesRead     = "group.machines.read"
	permGroupOwnersRead       = "group.owners.read"
	permGroupRead             = "group.read"
	permGroupUsersRead        = "group.users.read"
	permKeyRead               = "key.read"
	permLicenseCheckIn        = "license.check-in"
	permLicenseCheckOut       = "license.check-out"
	permLicenseCreate         = "license.create"
	permLicenseDelete         = "license.delete"
	permLicensePolicyUpdate   = "license.policy.update"
	permLicenseRead           = "license.read"
	permLicenseRenew          = "license.renew"
	permLicenseRevoke         = "license.revoke"
	permLicenseUsageIncrement = "license.usage.increment"
	permLicenseValidate       = "license.validate"
	permMachineCheckOut       = "machine.check-out"
	permMachineCreate         = "machine.create"
	permMachineDelete         = "machine.delete"
	permMachineHeartbeatPing  = "machine.heartbeat.ping"
	permMachineProofsGenerate = "machine.proofs.generate"
	permMachineRead           = "machine.read"
	permMachineUpdate         = "machine.update"
	permPackageRead           = "package.read"
	permPlatformRead          = "platform.read"
	permPolicyRead            = "policy.read"
	permProcessCreate         = "process.create"
	permProcessDelete         = "process.delete"
	permProcessHeartbeatPing  = "process.heartbeat.ping"
	permProcessRead           = "process.read"
	permProcessUpdate         = "process.update"
	permProductRead           = "product.read"
	permReleaseDownload       = "release.download"
	permReleaseRead           = "release.read"
	permReleaseUpgrade        = "release.upgrade"
	permTokenGenerate         = "token.generate"
	permTokenRead             = "token.read"
	permTokenRegenerate       = "token.regenerate"
	permTokenRevoke           = "token.revoke"
	permUserPasswordReset     = "user.password.reset"
	permUserPasswordUpdate    = "user.password.update"
	permUserRead              = "user.read"
	permUserSecondFactorsRead = "user.second-factors.read"
	permUserUpdate            = "user.update"
	permWebhookEndpointRead   = "webhook-endpoint.read"
	permWebhookEventRead      = "webhook-event.read"
)

// AllPermissions is the full catalog of permission names (the
// reference's ALL_PERMISSIONS, transcribed verbatim as data).
var AllPermissions = []string{ //nolint:gochecknoglobals // transcribed reference data []string{
	"account.analytics.read",
	"account.billing.read",
	"account.billing.update",
	"account.plan.read",
	"account.plan.update",
	permAccountRead,
	"account.subscription.read",
	"account.subscription.update",
	"account.update",
	"admin.create",
	"admin.delete",
	"admin.invite",
	"admin.read",
	"admin.update",
	permArchRead,
	"artifact.create",
	"artifact.delete",
	permArtifactRead,
	"artifact.update",
	permComponentCreate,
	permComponentDelete,
	permComponentRead,
	permComponentUpdate,
	permChannelRead,
	permConstraintRead,
	permEngineRead,
	"entitlement.create",
	"entitlement.delete",
	permEntitlementRead,
	"entitlement.update",
	"environment.create",
	"environment.delete",
	"environment.read",
	"environment.tokens.generate",
	"environment.update",
	"event-log.read",
	"group.create",
	"group.delete",
	permGroupLicensesRead,
	permGroupMachinesRead,
	"group.owners.attach",
	"group.owners.detach",
	permGroupOwnersRead,
	permGroupRead,
	"group.update",
	permGroupUsersRead,
	"key.create",
	"key.delete",
	permKeyRead,
	"key.update",
	permLicenseCheckIn,
	permLicenseCheckOut,
	permLicenseCreate,
	permLicenseDelete,
	"license.entitlements.attach",
	"license.entitlements.detach",
	"license.group.update",
	"license.owner.update",
	permLicensePolicyUpdate,
	permLicenseRead,
	"license.reinstate",
	permLicenseRenew,
	permLicenseRevoke,
	"license.suspend",
	"license.tokens.generate",
	"license.update",
	"license.usage.decrement",
	permLicenseUsageIncrement,
	"license.usage.reset",
	"license.user.update",
	"license.users.attach",
	"license.users.detach",
	permLicenseValidate,
	permMachineCheckOut,
	permMachineCreate,
	permMachineDelete,
	"machine.group.update",
	permMachineHeartbeatPing,
	"machine.heartbeat.reset",
	"machine.owner.update",
	permMachineProofsGenerate,
	permMachineRead,
	permMachineUpdate,
	"package.create",
	"package.delete",
	permPackageRead,
	"package.update",
	permPlatformRead,
	"policy.create",
	"policy.delete",
	"policy.entitlements.attach",
	"policy.entitlements.detach",
	"policy.pool.pop",
	permPolicyRead,
	"policy.update",
	permProcessCreate,
	permProcessDelete,
	permProcessHeartbeatPing,
	permProcessRead,
	permProcessUpdate,
	"product.create",
	"product.delete",
	permProductRead,
	"product.tokens.generate",
	"product.update",
	"release.constraints.attach",
	"release.constraints.detach",
	"release.create",
	"release.delete",
	permReleaseDownload,
	"release.package.update",
	"release.publish",
	permReleaseRead,
	"release.update",
	permReleaseUpgrade,
	"release.upload",
	"release.yank",
	"request-log.read",
	permTokenGenerate,
	permTokenRead,
	permTokenRegenerate,
	permTokenRevoke,
	"user.ban",
	"user.create",
	"user.delete",
	"user.group.update",
	"user.invite",
	permUserPasswordReset,
	permUserPasswordUpdate,
	permUserRead,
	"user.second-factors.create",
	"user.second-factors.delete",
	permUserSecondFactorsRead,
	"user.second-factors.update",
	"user.tokens.generate",
	"user.unban",
	permUserUpdate,
	"webhook-endpoint.create",
	"webhook-endpoint.delete",
	permWebhookEndpointRead,
	"webhook-endpoint.update",
	"webhook-event.delete",
	permWebhookEventRead,
	"webhook-event.retry",
}

// allPermissions is the admin set: every permission.
var allPermissions = toSet(AllPermissions) //nolint:gochecknoglobals // derived from the catalog

// readOnlyPermissions is the read-only user role's default set.
var readOnlyPermissions = toSet([]string{ //nolint:gochecknoglobals // transcribed reference data
	"account.analytics.read",
	"account.billing.read",
	"account.plan.read",
	permAccountRead,
	"account.subscription.read",
	"admin.read",
	permArchRead,
	permArtifactRead,
	permComponentRead,
	permChannelRead,
	permConstraintRead,
	permEngineRead,
	permEntitlementRead,
	"environment.read",
	"event-log.read",
	permGroupLicensesRead,
	permGroupMachinesRead,
	permGroupOwnersRead,
	permGroupRead,
	permGroupUsersRead,
	permKeyRead,
	permLicenseRead,
	permLicenseValidate,
	permMachineRead,
	permPackageRead,
	permPlatformRead,
	permPolicyRead,
	permProcessRead,
	permProductRead,
	permReleaseDownload,
	permReleaseRead,
	permReleaseUpgrade,
	"request-log.read",
	permTokenGenerate,
	permTokenRead,
	permUserPasswordReset,
	permUserPasswordUpdate,
	permUserRead,
	permUserSecondFactorsRead,
	permWebhookEndpointRead,
	permWebhookEventRead,
})

// productPermissions is the product bearer's default set.
var productPermissions = toSet([]string{ //nolint:gochecknoglobals // transcribed reference data
	permAccountRead,
	permArchRead,
	"artifact.create",
	"artifact.delete",
	permArtifactRead,
	"artifact.update",
	permComponentCreate,
	permComponentDelete,
	permComponentRead,
	permComponentUpdate,
	permChannelRead,
	permConstraintRead,
	permEngineRead,
	permEntitlementRead,
	"group.create",
	"group.delete",
	permGroupLicensesRead,
	permGroupMachinesRead,
	"group.owners.attach",
	"group.owners.detach",
	permGroupOwnersRead,
	permGroupRead,
	"group.update",
	permGroupUsersRead,
	"key.create",
	"key.delete",
	permKeyRead,
	"key.update",
	permLicenseCheckIn,
	permLicenseCheckOut,
	permLicenseCreate,
	permLicenseDelete,
	"license.entitlements.attach",
	"license.entitlements.detach",
	"license.group.update",
	"license.owner.update",
	permLicensePolicyUpdate,
	permLicenseRead,
	"license.reinstate",
	permLicenseRenew,
	permLicenseRevoke,
	"license.suspend",
	"license.tokens.generate",
	"license.update",
	"license.usage.decrement",
	permLicenseUsageIncrement,
	"license.usage.reset",
	"license.user.update",
	"license.users.attach",
	"license.users.detach",
	permLicenseValidate,
	permMachineCheckOut,
	permMachineCreate,
	permMachineDelete,
	"machine.group.update",
	permMachineHeartbeatPing,
	"machine.heartbeat.reset",
	"machine.owner.update",
	permMachineProofsGenerate,
	permMachineRead,
	permMachineUpdate,
	"package.create",
	"package.delete",
	permPackageRead,
	"package.update",
	permPlatformRead,
	"policy.create",
	"policy.delete",
	"policy.entitlements.attach",
	"policy.entitlements.detach",
	"policy.pool.pop",
	permPolicyRead,
	"policy.update",
	permProcessCreate,
	permProcessDelete,
	permProcessHeartbeatPing,
	permProcessRead,
	permProcessUpdate,
	permProductRead,
	"product.update",
	"release.constraints.attach",
	"release.constraints.detach",
	"release.create",
	"release.delete",
	permReleaseDownload,
	"release.package.update",
	"release.publish",
	permReleaseRead,
	"release.update",
	permReleaseUpgrade,
	"release.upload",
	"release.yank",
	permTokenGenerate,
	permTokenRead,
	permTokenRegenerate,
	permTokenRevoke,
	"user.ban",
	"user.create",
	"user.delete",
	"user.group.update",
	permUserRead,
	"user.tokens.generate",
	"user.unban",
	permUserUpdate,
	"webhook-endpoint.create",
	"webhook-endpoint.delete",
	permWebhookEndpointRead,
	"webhook-endpoint.update",
	permWebhookEventRead,
})

// userPermissions is the plain user role's default set: the reference's
// USER_PERMISSIONS minus account.read, license.users.attach,
// license.users.detach, policy.read and product.read (backwards
// compatibility defaults).
var userPermissions = toSet([]string{ //nolint:gochecknoglobals // transcribed reference data
	permArchRead,
	permArtifactRead,
	permComponentCreate,
	permComponentDelete,
	permComponentRead,
	permComponentUpdate,
	permChannelRead,
	permConstraintRead,
	permEngineRead,
	permEntitlementRead,
	permGroupLicensesRead,
	permGroupMachinesRead,
	permGroupOwnersRead,
	permGroupRead,
	permGroupUsersRead,
	permLicenseCheckIn,
	permLicenseCheckOut,
	permLicenseCreate,
	permLicenseDelete,
	permLicensePolicyUpdate,
	permLicenseRead,
	permLicenseRenew,
	permLicenseRevoke,
	permLicenseUsageIncrement,
	permLicenseValidate,
	permMachineCheckOut,
	permMachineCreate,
	permMachineDelete,
	permMachineHeartbeatPing,
	permMachineProofsGenerate,
	permMachineRead,
	permMachineUpdate,
	permPackageRead,
	permPlatformRead,
	permProcessCreate,
	permProcessDelete,
	permProcessHeartbeatPing,
	permProcessRead,
	permProcessUpdate,
	permReleaseDownload,
	permReleaseRead,
	permReleaseUpgrade,
	permTokenGenerate,
	permTokenRead,
	permTokenRegenerate,
	permTokenRevoke,
	permUserPasswordReset,
	permUserPasswordUpdate,
	permUserRead,
	"user.second-factors.create",
	"user.second-factors.delete",
	permUserSecondFactorsRead,
	"user.second-factors.update",
	permUserUpdate,
})

// licensePermissions is the license bearer's default set:
// LICENSE_PERMISSIONS minus account.read, product.read, policy.read and
// user.read.
var licensePermissions = toSet([]string{ //nolint:gochecknoglobals // transcribed reference data
	permArchRead,
	permArtifactRead,
	permComponentCreate,
	permComponentDelete,
	permComponentRead,
	permComponentUpdate,
	permChannelRead,
	permConstraintRead,
	permEngineRead,
	permEntitlementRead,
	permGroupOwnersRead,
	permGroupRead,
	permLicenseCheckIn,
	permLicenseCheckOut,
	permLicenseRead,
	permLicenseUsageIncrement,
	permLicenseValidate,
	permMachineCheckOut,
	permMachineCreate,
	permMachineDelete,
	permMachineHeartbeatPing,
	permMachineProofsGenerate,
	permMachineRead,
	permMachineUpdate,
	permPackageRead,
	permPlatformRead,
	permProcessCreate,
	permProcessDelete,
	permProcessHeartbeatPing,
	permProcessRead,
	permProcessUpdate,
	permReleaseDownload,
	permReleaseRead,
	permReleaseUpgrade,
	permTokenRead,
	permTokenRegenerate,
	permTokenRevoke,
})

// RolePermissions returns the role's CE default permission set.
func RolePermissions(role domain.Role) map[string]struct{} {
	switch role {
	case domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent, domain.RoleSupportRole:
		// The admin-family roles default to the full set.
		return allPermissions
	case domain.RoleReadOnly:
		return readOnlyPermissions
	case domain.RoleProduct:
		return productPermissions
	case domain.RoleLicense:
		return licensePermissions
	default:
		return userPermissions
	}
}

// Effective computes the effective permission set of a token
// authentication: the intersection of the bearer's role set and the
// token's own permissions, where a wildcard ("*") on either side defers
// to the other side.
func Effective(role domain.Role, tokenPermissions []string) map[string]struct{} {
	roleSet := RolePermissions(role)

	tokenHasWildcard := false

	for _, p := range tokenPermissions {
		if p == "*" {
			tokenHasWildcard = true

			break
		}
	}

	if tokenHasWildcard || len(tokenPermissions) == 0 {
		return roleSet
	}

	// An admin-family bearer holds the wildcard: every token permission
	// passes; otherwise the plain intersection applies.
	bearerHasWildcard := len(roleSet) == len(allPermissions)
	out := make(map[string]struct{}, len(tokenPermissions))

	for _, p := range tokenPermissions {
		if _, ok := roleSet[p]; ok || bearerHasWildcard {
			out[p] = struct{}{}
		}
	}

	return out
}

func toSet(list []string) map[string]struct{} {
	set := make(map[string]struct{}, len(list))
	for _, p := range list {
		set[p] = struct{}{}
	}

	return set
}
