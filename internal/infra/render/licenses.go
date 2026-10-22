// License, machine, license-user/license-entitlement and license-file
// documents (the reference's LicenseSerializer, MachineSerializer,
// LicenseUserSerializer, LicenseEntitlementSerializer and
// LicenseFileSerializer).
package render

import (
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// License renders a license exactly as the reference's
// LicenseSerializer does. RawKey carries the one-time plaintext of a
// legacy-encrypted key (the create response); later reads omit key.
func License(accountID uuid.UUID, l domain.License, now time.Time, rawKey *string) oas.LicenseResource {
	base := "/v1/accounts/" + accountID.String() + "/licenses/" + l.ID.String()

	attrs := oas.LicenseAttributes{
		Name:             NilString(deref(l.Name)),
		Expiry:           NilString(formatTimePtr(l.Expiry)),
		Status:           l.Status(now),
		Uses:             int(l.Uses),
		Suspended:        l.Suspended,
		Scheme:           nilString(l.Policy.Scheme),
		Encrypted:        l.Policy.Encrypted,
		Strict:           l.Policy.Strict,
		Floating:         l.Policy.Floating,
		Protected:        l.ProtectedFlag(),
		Version:          NilString(deref(l.LastValidatedVersion)),
		MaxMachines:      nilInt(l.MaxMachines()),
		MaxProcesses:     nilInt(l.MaxProcesses()),
		MaxUsers:         nilInt(l.MaxUsers()),
		MaxCores:         nilInt(l.MaxCores()),
		MaxMemory:        nilInt64(l.MaxMemory()),
		MaxDisk:          nilInt64(l.MaxDisk()),
		MaxUses:          nilInt(l.MaxUses()),
		RequireHeartbeat: l.Policy.RequireHeartbeat,
		RequireCheckIn:   l.Policy.RequiresCheckIn(),
		LastValidated:    NilString(formatTimePtr(l.LastValidatedAt)),
		LastCheckIn:      NilString(formatTimePtr(l.LastCheckInAt)),
		NextCheckIn:      NilString(formatTimePtr(l.NextCheckIn())),
		LastCheckOut:     NilString(formatTimePtr(l.LastCheckOutAt)),
		Metadata:         Metadata(l.Metadata),
		Created:          Timestamp(l.CreatedAt),
		Updated:          Timestamp(l.UpdatedAt),
	}

	if l.Policy.Scheme != "LEGACY_ENCRYPT" || !l.Policy.Encrypted || rawKey != nil {
		key := l.Key
		if rawKey != nil {
			key = *rawKey
		}

		attrs.Key = oas.NewOptString(key)
	}

	return oas.LicenseResource{
		Type:       oas.LicenseResourceTypeLicenses,
		ID:         l.ID.String(),
		Attributes: attrs,
		Relationships: oas.LicenseRelationships{
			Account: accountLink(accountID),
			Product: toOne(base, "/product", "products", l.ProductID),
			Policy:  toOne(base, "/policy", "policies", l.PolicyID),
			Group:   nullableToOne(base, "/group", "groups", l.GroupID),
			Owner:   nullableToOne(base, "/owner", "users", l.OwnerID),
			Users: oas.LicenseUsersRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base + "/users")},
				Meta:  oas.LicenseUsersRelationshipMeta{Count: int(l.UsersCount())},
			},
			Machines: oas.LicenseMachinesRelationship{
				Links: oas.RelationshipLinks{Related: oas.NewNilString(base + "/machines")},
				Meta: oas.LicenseMachinesRelationshipMeta{
					Cores: int(l.MachinesCoreCount), Memory: int(l.MachinesMemoryCount),
					Disk: int(l.MachinesDiskCount), Count: int(l.MachinesCount),
				},
			},
			Tokens:       related(base, "/tokens"),
			Entitlements: related(base, "/entitlements"),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}

// Machine renders a machine document (the machines reached through a
// license's relationship).
func Machine(
	accountID uuid.UUID, m domain.Machine, licenseProductID uuid.UUID, licenseMaxProcesses *int32,
) oas.MachineResource {
	base := "/v1/accounts/" + accountID.String() + "/machines/" + m.ID.String()

	requireHeartbeat := m.RequireHeartbeat || m.LastHeartbeatAt != nil

	duration := m.HeartbeatDurationOrDefault()

	nextHeartbeat := (*string)(nil)

	if m.LastHeartbeatAt != nil {
		next := m.LastHeartbeatAt.Add(time.Duration(duration) * time.Second)
		formatted := formatTime(next)
		nextHeartbeat = &formatted
	}

	return oas.MachineResource{
		Type: oas.MachineResourceTypeMachines,
		ID:   m.ID.String(),
		Attributes: oas.MachineAttributes{
			Fingerprint:       m.Fingerprint,
			Cores:             nilInt(m.Cores),
			Memory:            nilInt64(m.Memory),
			Disk:              nilInt64(m.Disk),
			IP:                NilString(deref(m.IP)),
			Hostname:          NilString(deref(m.Hostname)),
			Platform:          NilString(deref(m.Platform)),
			Name:              NilString(deref(m.Name)),
			RequireHeartbeat:  requireHeartbeat,
			HeartbeatStatus:   m.HeartbeatStatus,
			HeartbeatDuration: oas.NewNilInt(int(duration)),
			MaxProcesses:      nilInt(m.MaxProcesses(licenseMaxProcesses)),
			LastCheckOut:      NilString(formatTimePtr(m.LastCheckOutAt)),
			LastHeartbeat:     NilString(formatTimePtr(m.LastHeartbeatAt)),
			NextHeartbeat:     NilString(deref(nextHeartbeat)),
			Metadata:          Metadata(m.Metadata),
			Created:           Timestamp(m.CreatedAt),
			Updated:           Timestamp(m.UpdatedAt),
		},
		Relationships: oas.MachineRelationships{
			Account:    accountLink(accountID),
			Product:    toOne(base, "/product", "products", licenseProductID),
			Group:      nullableToOne(base, "/group", "groups", m.GroupID),
			License:    toOne(base, "/license", "licenses", m.LicenseID),
			Owner:      nullableToOne(base, "/owner", "users", m.OwnerID),
			Components: related(base, "/components"),
			Processes:  related(base, "/processes"),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}

// LicenseUser renders a license-user join document.
func LicenseUser(accountID uuid.UUID, join domain.LicenseUser) oas.LicenseUserResource {
	return oas.LicenseUserResource{
		Type: oas.LicenseUserResourceTypeLicenseUsers,
		ID:   join.ID.String(),
		Attributes: oas.LicenseUserResourceAttributes{
			Created: Timestamp(join.CreatedAt), Updated: Timestamp(join.UpdatedAt),
		},
		Relationships: oas.LicenseUserResourceRelationships{
			Account: accountLink(accountID),
			License: toOne("/v1/accounts/"+accountID.String()+"/licenses/"+join.LicenseID.String(), "",
				"licenses", join.LicenseID),
			User: toOne("/v1/accounts/"+accountID.String()+"/users/"+join.UserID.String(), "", "users", join.UserID),
		},
		Links: oas.JoinLinks{
			Related: "/v1/accounts/" + accountID.String() + "/licenses/" +
				join.LicenseID.String() + "/users/" + join.UserID.String(),
		},
	}
}

// LicenseEntitlement renders a license-entitlement join document.
func LicenseEntitlement(accountID uuid.UUID, join domain.LicenseEntitlement) oas.LicenseEntitlementResource {
	licenseBase := "/v1/accounts/" + accountID.String() + "/licenses/" + join.LicenseID.String()

	return oas.LicenseEntitlementResource{
		Type: oas.LicenseEntitlementResourceTypeLicenseEntitlements,
		ID:   join.ID.String(),
		Attributes: oas.LicenseEntitlementResourceAttributes{
			Created: Timestamp(join.CreatedAt), Updated: Timestamp(join.UpdatedAt),
		},
		Relationships: oas.LicenseEntitlementResourceRelationships{
			Account: accountLink(accountID),
			Entitlement: toOne("/v1/accounts/"+accountID.String()+"/entitlements/"+join.EntitlementID.String(),
				"", "entitlements", join.EntitlementID),
			License: toOne(licenseBase, "", "licenses", join.LicenseID),
		},
		Links: oas.JoinLinks{
			Related: licenseBase + "/entitlements/" + join.EntitlementID.String(),
		},
	}
}

// LicenseFile renders the license-file document of a checkout.
func LicenseFile(accountID uuid.UUID, fileID uuid.UUID, certificate, algorithm string,
	includes []string, ttl *int32, issued time.Time, expiry *time.Time, licenseID uuid.UUID,
) oas.LicenseFileResource {
	var (
		ttlAttr      oas.NilInt
		includesAttr []string
	)

	if ttl != nil {
		ttlAttr = oas.NewNilInt(int(*ttl))
	} else {
		ttlAttr.SetToNull()
	}

	if includes == nil {
		includesAttr = []string{}
	} else {
		includesAttr = includes
	}

	return oas.LicenseFileResource{
		Type: oas.LicenseFileResourceTypeLicenseFiles,
		ID:   fileID.String(),
		Attributes: oas.LicenseFileAttributes{
			Certificate: certificate,
			Algorithm:   algorithm,
			Includes:    includesAttr,
			TTL:         ttlAttr,
			Expiry:      NilString(formatTimePtr(expiry)),
			Issued:      Timestamp(issued),
		},
		Relationships: oas.LicenseFileResourceRelationships{
			Account: accountLink(accountID),
			License: toOne("/v1/accounts/"+accountID.String()+"/licenses/"+licenseID.String(),
				"", "licenses", licenseID),
		},
	}
}

// nullableToOne renders a to-one relationship whose linkage may be
// null (path empty skips the related link duplication when the caller
// already built it).
func nullableToOne(base, path, typ string, id uuid.NullUUID) oas.ToOneRelationship {
	if !id.Valid {
		return oas.ToOneRelationship{
			Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)},
			Data:  oas.NilResourceIdentifier{Null: true},
		}
	}

	return toOne(base, path, typ, id.UUID)
}

func deref[T any](p *T) T {
	if p == nil {
		var zero T

		return zero
	}

	return *p
}

func formatTime(t time.Time) string { return jsonapi.Timestamp(t) }

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}

	return formatTime(*t)
}
