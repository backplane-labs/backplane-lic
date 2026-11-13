package render

import (
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// typeProducts is the JSON:API type of a product.
const typeProducts = "products"

// typeAccounts is the JSON:API type of an account.
const typeAccounts = "accounts"

// Component renders a machine component document.
func Component(accountID uuid.UUID, c domain.MachineComponent) oas.ComponentResource {
	base := "/v1/accounts/" + accountID.String() + "/components/" + c.ID.String()

	return oas.ComponentResource{
		Type: oas.ComponentResourceTypeComponents,
		ID:   c.ID.String(),
		Attributes: oas.ComponentAttributes{
			Fingerprint: c.Fingerprint, Name: c.Name, Metadata: Metadata(c.Metadata),
			Created: Timestamp(c.CreatedAt), Updated: Timestamp(c.UpdatedAt),
		},
		Relationships: oas.ComponentRelationships{
			Account: accountLink(accountID),
			Product: toOne(base, "/product", typeProducts, c.ProductID),
			License: toOne(base, "/license", "licenses", c.LicenseID),
			Machine: toOne(base, "/machine", "machines", c.MachineID),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}

// Process renders a machine process document.
func Process(accountID uuid.UUID, p domain.MachineProcess) oas.ProcessResource {
	base := "/v1/accounts/" + accountID.String() + "/processes/" + p.ID.String()

	return oas.ProcessResource{
		Type: oas.ProcessResourceTypeProcesses,
		ID:   p.ID.String(),
		Attributes: oas.ProcessAttributes{
			Pid: p.PID, Status: oas.ProcessAttributesStatus(p.HeartbeatStatus),
			Interval:      int(p.Heartbeat.DurationOrDefault() / time.Second),
			LastHeartbeat: Timestamp(p.LastHeartbeatAt), NextHeartbeat: Timestamp(p.NextHeartbeatAt()),
			Metadata: Metadata(p.Metadata), Created: Timestamp(p.CreatedAt), Updated: Timestamp(p.UpdatedAt),
		},
		Relationships: oas.ProcessRelationships{
			Account: accountLink(accountID),
			Product: toOne(base, "/product", typeProducts, p.ProductID),
			License: toOne(base, "/license", "licenses", p.LicenseID),
			Machine: toOne(base, "/machine", "machines", p.MachineID),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}
