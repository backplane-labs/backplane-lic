// Machine ownership rules, transcribed from MachinePolicy
// (app/policies/machine_policy.rb) and Machines::HeartbeatPolicy
// (app/policies/machines/heartbeat_policy.rb).
package access

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// The machine rules take a machine already found within the bearer's
// authorized scope (application_policy.rb scope_for: a product's
// machines, a user's through the licenses it owns or uses, a license's
// own), so a user always holds the machine in bearer.machines there.

// MachineShow mirrors MachinePolicy#show? (machine_policy.rb:24-42): the
// admin family; the product bearer its product's; a user the machines of
// its licenses; the license bearer its own.
func MachineShow(p Principal, m domain.Machine) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// MachineProof mirrors Machines::V1x0::ProofPolicy#create?: admin,
// developer and sales agent; the product bearer its product's; the
// license's owner, never a protected license; the license bearer its own.
func MachineProof(p Principal, m domain.Machine) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper || p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser && m.LicenseOwnerID.Valid && m.LicenseOwnerID.UUID == p.BearerID:
		if m.LicenseProtected {
			return deny()
		}

		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// MachineCreate mirrors MachinePolicy#create? (machine_policy.rb:44-60):
// admin, developer and sales agent; the product bearer a license of its
// product; a user only as the machine's owner or on a license she owns,
// never a protected license; the license bearer only itself. licensed
// is false when the license does not exist (record.license is nil).
func MachineCreate(p Principal, license domain.License, licensed bool, owner uuid.NullUUID) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && licensed && license.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if !userMayCreateMachine(p, license, licensed, owner) {
			return deny()
		}

		return nil
	case p.Role == domain.RoleLicense && licensed && license.ID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// userMayCreateMachine is MachineCreate's user clause: the machine's or
// the license's owner, never on a protected license.
func userMayCreateMachine(p Principal, license domain.License, licensed bool, owner uuid.NullUUID) bool {
	ownsMachine := owner.Valid && owner.UUID == p.BearerID
	ownsLicense := licensed && license.OwnerID.Valid && license.OwnerID.UUID == p.BearerID

	return (ownsMachine || ownsLicense) && (!licensed || !license.ProtectedFlag())
}

// MachineUpdate mirrors MachinePolicy#update? (machine_policy.rb:62-78):
// admin, developer, sales agent and support agent (never read only); the
// product bearer its product's; a user as the machine's owner or the
// license's owner, never a protected license; the license bearer only
// itself and never a protected license.
func MachineUpdate(p Principal, m domain.Machine) error {
	switch {
	case isStaffWriter(p):
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if !machineOwnedByUser(p, m) {
			return deny()
		}

		if m.LicenseProtected {
			return deny()
		}

		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		if m.LicenseProtected {
			return deny()
		}

		return nil
	default:
		return deny()
	}
}

// MachineDestroy mirrors MachinePolicy#destroy? (machine_policy.rb:80-96):
// admin, developer and sales agent; the product bearer its product's; a
// user as the machine's owner or the license's owner, never a protected
// license; the license bearer always its own.
func MachineDestroy(p Principal, m domain.Machine) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if !machineOwnedByUser(p, m) {
			return deny()
		}

		if m.LicenseProtected {
			return deny()
		}

		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// MachineCheckOut mirrors MachinePolicy#check_out?
// (machine_policy.rb:98-114): the admin family; the product bearer its
// product's; a user machines she owns, of licenses she owns or is a
// user of, never a protected license; the license bearer its own. Read
// only is not admitted.
func MachineCheckOut(p Principal, m domain.Machine) error {
	switch {
	case isStaffWriter(p):
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if m.LicenseProtected {
			return deny()
		}

		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// MachineLicenseOwnerShow mirrors Machines::V1x5::UserPolicy#show?: the
// admin family; the product bearer its product's; a user only as the
// license's owner; the license bearer its own.
func MachineLicenseOwnerShow(p Principal, m domain.Machine) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser && m.LicenseOwnerID.Valid && m.LicenseOwnerID.UUID == p.BearerID:
		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// The component and process rules (MachineComponentPolicy and
// MachineProcessPolicy are the same table) judge the record by its
// machine: its product, owner, license and the license's protection.

// SubresourceCreate mirrors create?: admin, developer and sales agent;
// the product bearer its product's; the machine's or license's owner,
// never on a protected license; the license bearer its own.
func SubresourceCreate(p Principal, m domain.Machine) error {
	return subresourceWrite(p, m, p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent)
}

// SubresourceUpdate mirrors update?: create?'s table plus support agents.
func SubresourceUpdate(p Principal, m domain.Machine) error {
	return subresourceWrite(p, m, isStaffWriter(p))
}

// SubresourceDestroy mirrors destroy?: create?'s table.
func SubresourceDestroy(p Principal, m domain.Machine) error { return SubresourceCreate(p, m) }

func subresourceWrite(p Principal, m domain.Machine, staff bool) error {
	switch {
	case staff:
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser && machineOwnedByUser(p, m):
		if m.LicenseProtected {
			return deny()
		}

		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// MachineUsageRead guards the usage journal read (a backplane extension):
// the vendor side only — the admin family and the machine's product.
func MachineUsageRead(p Principal, m domain.Machine) error {
	switch {
	case isAdminFamily(p):
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// MachineRelationshipUpdate mirrors Machines::OwnerPolicy#update? and
// Machines::GroupPolicy#update?: admin, developer and sales agent, or
// the product bearer of the machine's product.
func MachineRelationshipUpdate(p Principal, m domain.Machine) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper || p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// isStaffWriter is the admin family without read only (update?,
// check_out?).
func isStaffWriter(p Principal) bool {
	return isAdminFamily(p) && p.Role != domain.RoleReadOnly
}

// machineOwnedByUser is update?/destroy?'s stricter user clause: the
// machine's owner or the license's owner, without the machine_ids set
// (machine_policy.rb:71, 89).
func machineOwnedByUser(p Principal, m domain.Machine) bool {
	return m.OwnerID.Valid && m.OwnerID.UUID == p.BearerID ||
		m.LicenseOwnerID.Valid && m.LicenseOwnerID.UUID == p.BearerID
}

// MachinePing mirrors Machines::HeartbeatPolicy#ping?
// (machines/heartbeat_policy.rb:7-25): admin and developer only on the
// admin side; the product bearer its product's; a user as the machine's
// owner or the license's owner, never a protected license; the license
// bearer its own.
func MachinePing(p Principal, m domain.Machine) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper:
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	case p.Role == domain.RoleUser:
		if !machineOwnedByUser(p, m) {
			return deny()
		}

		if m.LicenseProtected {
			return deny()
		}

		return nil
	case p.Role == domain.RoleLicense && m.LicenseID == p.BearerID:
		return nil
	default:
		return deny()
	}
}

// MachineResetHeartbeat mirrors Machines::HeartbeatPolicy#reset?
// (machines/heartbeat_policy.rb:27-41): admin, developer and sales
// agent, or the product bearer of the machine's product. No user, no
// license.
func MachineResetHeartbeat(p Principal, m domain.Machine) error {
	switch {
	case p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper ||
		p.Role == domain.RoleSalesAgent:
		return nil
	case p.Role == domain.RoleProduct && m.ProductID == p.BearerID:
		return nil
	default:
		return deny()
	}
}
