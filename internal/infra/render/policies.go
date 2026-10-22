package render

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// PolicyResource renders a policy exactly as the reference's
// PolicySerializer does.
func PolicyResource(accountID uuid.UUID, p domain.Policy) oas.PolicyResource {
	base := "/v1/accounts/" + accountID.String() + "/policies/" + p.ID.String()

	return oas.PolicyResource{
		Type: oas.PolicyResourceTypePolicies,
		ID:   p.ID.String(),
		Attributes: oas.PolicyAttributes{
			Name:                          p.Name,
			Duration:                      nilInt(p.Duration),
			Strict:                        p.Strict,
			Floating:                      p.Floating,
			UsePool:                       p.UsePool,
			MaxMachines:                   nilInt(p.MaxMachines),
			MaxProcesses:                  nilInt(p.MaxProcesses),
			MaxUsers:                      nilInt(p.MaxUsers),
			MaxCores:                      nilInt(p.MaxCores),
			MaxMemory:                     nilInt64(p.MaxMemory),
			MaxDisk:                       nilInt64(p.MaxDisk),
			MaxUses:                       nilInt(p.MaxUses),
			MachineUniquenessStrategy:     nilString(p.MachineUniquenessStrategy),
			MachineMatchingStrategy:       nilString(p.MachineMatchingStrategy),
			ComponentUniquenessStrategy:   nilString(p.ComponentUniquenessStrategy),
			ComponentMatchingStrategy:     nilString(p.ComponentMatchingStrategy),
			ExpirationStrategy:            nilString(p.ExpirationStrategy),
			ExpirationBasis:               nilString(p.ExpirationBasis),
			RenewalBasis:                  nilString(p.RenewalBasis),
			TransferStrategy:              nilString(p.TransferStrategy),
			AuthenticationStrategy:        nilString(p.AuthenticationStrategy),
			MachineLeasingStrategy:        nilString(p.MachineLeasingStrategy),
			ProcessLeasingStrategy:        nilString(p.ProcessLeasingStrategy),
			OverageStrategy:               nilString(p.OverageStrategy),
			Scheme:                        nilString(p.Scheme),
			Encrypted:                     p.Encrypted,
			Protected:                     nilBool(p.Protected),
			RequireProductScope:           p.RequireProductScope,
			RequirePolicyScope:            p.RequirePolicyScope,
			RequireMachineScope:           p.RequireMachineScope,
			RequireFingerprintScope:       p.RequireFingerprintScope,
			RequireComponentsScope:        p.RequireComponentsScope,
			RequireUserScope:              p.RequireUserScope,
			RequireChecksumScope:          p.RequireChecksumScope,
			RequireVersionScope:           p.RequireVersionScope,
			RequireCheckIn:                p.RequireCheckIn,
			CheckInInterval:               nilString(p.CheckInInterval),
			CheckInIntervalCount:          nilInt(p.CheckInIntervalCount),
			HeartbeatDuration:             nilInt(p.HeartbeatDuration),
			HeartbeatCullStrategy:         nilString(p.HeartbeatCullStrategy),
			HeartbeatResurrectionStrategy: nilString(p.HeartbeatResurrectionStrategy),
			HeartbeatBasis:                nilString(p.HeartbeatBasis),
			RequireHeartbeat:              p.RequireHeartbeat,
			Metadata:                      metadata(p.Metadata),
			Created:                       timestamp(p.CreatedAt),
			Updated:                       timestamp(p.UpdatedAt),
		},
		Relationships: oas.PolicyRelationships{
			Account:      accountLink(accountID),
			Product:      toOne(base, "/product", "products", p.ProductID),
			Pool:         related(base, "/pool"),
			Licenses:     related(base, "/licenses"),
			Entitlements: related(base, "/entitlements"),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}
