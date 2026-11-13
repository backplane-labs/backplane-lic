package access_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// actor is one caller kind relative to a fixture machine.
type actor struct {
	name      string
	principal access.Principal
	machine   domain.Machine
}

// machineActors builds every caller kind with the machine seen from its side:
// staff roles, the owning and a foreign product, users (machine owner,
// license owner, other; protected and unprotected license), licenses.
func machineActors() []actor {
	product, license := uuid.New(), uuid.New()
	owner, licenseOwner := uuid.New(), uuid.New()

	base := domain.Machine{
		ID: uuid.New(), LicenseID: license, ProductID: product,
		OwnerID:        uuid.NullUUID{UUID: owner, Valid: true},
		LicenseOwnerID: uuid.NullUUID{UUID: licenseOwner, Valid: true},
	}
	protected := base
	protected.LicenseProtected = true

	staff := func(role domain.Role) actor {
		return actor{string(role), access.Principal{Role: role, BearerType: access.BearerUser, BearerID: uuid.New()}, base}
	}
	user := func(name string, id uuid.UUID, m domain.Machine) actor {
		return actor{name, access.Principal{Role: domain.RoleUser, BearerType: access.BearerUser, BearerID: id}, m}
	}

	return []actor{
		staff(domain.RoleAdmin), staff(domain.RoleDeveloper), staff(domain.RoleSalesAgent),
		staff(domain.RoleSupportRole), staff(domain.RoleReadOnly),
		{"product own", access.Principal{Role: domain.RoleProduct, BearerType: access.BearerProduct, BearerID: product}, base},
		{"product foreign", access.Principal{Role: domain.RoleProduct, BearerType: access.BearerProduct, BearerID: uuid.New()}, base},
		user("user machine owner", owner, base),
		user("user machine owner protected", owner, protected),
		user("user license owner", licenseOwner, base),
		user("user license owner protected", licenseOwner, protected),
		user("user other", uuid.New(), base),
		user("user other protected", uuid.New(), protected),
		{"license own", access.Principal{Role: domain.RoleLicense, BearerType: access.BearerLicense, BearerID: license}, base},
		{"license own protected", access.Principal{Role: domain.RoleLicense, BearerType: access.BearerLicense, BearerID: license}, protected},
		{"license foreign", access.Principal{Role: domain.RoleLicense, BearerType: access.BearerLicense, BearerID: uuid.New()}, base},
	}
}

func TestMachineRules(t *testing.T) {
	t.Parallel()

	// want lists the actor names that pass; all others are denied.
	rules := []struct {
		name string
		rule func(access.Principal, domain.Machine) error
		want []string
	}{
		{"show", access.MachineShow, []string{
			"admin", "developer", "sales_agent", "support_agent", "read_only", "product own",
			"user machine owner", "user machine owner protected", "user license owner", "user license owner protected",
			"user other", "user other protected", "license own", "license own protected",
		}},
		{"proof", access.MachineProof, []string{
			"admin", "developer", "sales_agent", "product own", "user license owner", "license own", "license own protected",
		}},
		{"update", access.MachineUpdate, []string{
			"admin", "developer", "sales_agent", "support_agent", "product own",
			"user machine owner", "user license owner", "license own",
		}},
		{"destroy", access.MachineDestroy, []string{
			"admin", "developer", "sales_agent", "product own", "user machine owner", "user license owner",
			"license own", "license own protected",
		}},
		{"check out", access.MachineCheckOut, []string{
			"admin", "developer", "sales_agent", "support_agent", "product own",
			"user machine owner", "user license owner", "user other", "license own", "license own protected",
		}},
		{"license owner show", access.MachineLicenseOwnerShow, []string{
			"admin", "developer", "sales_agent", "support_agent", "read_only", "product own",
			"user license owner", "user license owner protected", "license own", "license own protected",
		}},
		{"subresource create", access.SubresourceCreate, []string{
			"admin", "developer", "sales_agent", "product own", "user machine owner", "user license owner",
			"license own", "license own protected",
		}},
		{"subresource update", access.SubresourceUpdate, []string{
			"admin", "developer", "sales_agent", "support_agent", "product own", "user machine owner",
			"user license owner", "license own", "license own protected",
		}},
		{"subresource destroy", access.SubresourceDestroy, []string{
			"admin", "developer", "sales_agent", "product own", "user machine owner", "user license owner",
			"license own", "license own protected",
		}},
		{"usage read", access.MachineUsageRead, []string{
			"admin", "developer", "sales_agent", "support_agent", "read_only", "product own",
		}},
		{"relationship update", access.MachineRelationshipUpdate, []string{
			"admin", "developer", "sales_agent", "product own",
		}},
		{"ping", access.MachinePing, []string{
			"admin", "developer", "product own", "user machine owner", "user license owner", "license own", "license own protected",
		}},
		{"reset heartbeat", access.MachineResetHeartbeat, []string{
			"admin", "developer", "sales_agent", "product own",
		}},
	}

	actors := machineActors()

	for _, rule := range rules {
		t.Run(rule.name, func(t *testing.T) {
			t.Parallel()

			allowed := map[string]bool{}
			for _, name := range rule.want {
				allowed[name] = true
			}

			for _, a := range actors {
				err := rule.rule(a.principal, a.machine)
				if (err == nil) != allowed[a.name] {
					t.Errorf("%s: err = %v, want allowed = %v", a.name, err, allowed[a.name])
				}
			}
		})
	}
}

func TestMachineCreateRule(t *testing.T) {
	t.Parallel()

	product, licenseID, ownerID := uuid.New(), uuid.New(), uuid.New()
	license := domain.License{
		ID: licenseID, ProductID: product,
		OwnerID: uuid.NullUUID{UUID: ownerID, Valid: true},
	}
	protected := license
	protected.Protected = &[]bool{true}[0]

	user := func(id uuid.UUID) access.Principal {
		return access.Principal{Role: domain.RoleUser, BearerType: access.BearerUser, BearerID: id}
	}
	of := func(role domain.Role, id uuid.UUID) access.Principal {
		return access.Principal{Role: role, BearerID: id}
	}
	self := uuid.NullUUID{UUID: ownerID, Valid: true}

	for _, test := range []struct {
		name     string
		p        access.Principal
		license  domain.License
		licensed bool
		owner    uuid.NullUUID
		want     bool
	}{
		{"admin", of(domain.RoleAdmin, uuid.New()), license, true, uuid.NullUUID{}, true},
		{"admin without license", of(domain.RoleAdmin, uuid.New()), domain.License{}, false, uuid.NullUUID{}, true},
		{"developer", of(domain.RoleDeveloper, uuid.New()), license, true, uuid.NullUUID{}, true},
		{"sales agent", of(domain.RoleSalesAgent, uuid.New()), license, true, uuid.NullUUID{}, true},
		{"support agent", of(domain.RoleSupportRole, uuid.New()), license, true, uuid.NullUUID{}, false},
		{"read only", of(domain.RoleReadOnly, uuid.New()), license, true, uuid.NullUUID{}, false},
		{"product own", of(domain.RoleProduct, product), license, true, uuid.NullUUID{}, true},
		{"product foreign", of(domain.RoleProduct, uuid.New()), license, true, uuid.NullUUID{}, false},
		{"product without license", of(domain.RoleProduct, product), domain.License{}, false, uuid.NullUUID{}, false},
		{"license own", of(domain.RoleLicense, licenseID), license, true, uuid.NullUUID{}, true},
		{"license foreign", of(domain.RoleLicense, uuid.New()), license, true, uuid.NullUUID{}, false},
		{"license without license", of(domain.RoleLicense, licenseID), domain.License{}, false, uuid.NullUUID{}, false},
		{"user license owner", user(ownerID), license, true, uuid.NullUUID{}, true},
		{"user license owner protected", user(ownerID), protected, true, uuid.NullUUID{}, false},
		{"user as machine owner on foreign license", user(ownerID), domain.License{ID: licenseID}, true, self, true},
		{"user as machine owner protected", user(ownerID), protected, true, self, false},
		{"user as machine owner without license", user(ownerID), domain.License{}, false, self, true},
		{"user other", user(uuid.New()), license, true, uuid.NullUUID{}, false},
		{"user other naming another owner", user(uuid.New()), license, true, uuid.NullUUID{UUID: uuid.New(), Valid: true}, false},
		{"user without license or owner", user(ownerID), domain.License{}, false, uuid.NullUUID{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := access.MachineCreate(test.p, test.license, test.licensed, test.owner)
			if (err == nil) != test.want {
				t.Errorf("err = %v, want allowed = %v", err, test.want)
			}
		})
	}
}

func BenchmarkMachineUpdate(b *testing.B) {
	actors := machineActors()

	for b.Loop() {
		for _, a := range actors {
			_ = access.MachineUpdate(a.principal, a.machine)
		}
	}
}
