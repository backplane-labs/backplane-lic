package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func (p Publisher) recordLicense(ctx context.Context, name string, license domain.License, code string) error {
	var eventName string

	switch name {
	case "license.created":
		eventName = "LicenseCreated"
	case "license.expiring-soon":
		eventName = "LicenseExpiringSoon"
	case "license.expired":
		eventName = "LicenseExpired"
	case "license.renewed":
		eventName = "LicenseRenewed"
	case "license.suspended":
		eventName = "LicenseSuspended"
	case "license.revoked":
		eventName = "LicenseRevoked"
	case "license.validation.failed":
		eventName = "LicenseValidationFailed"
	case "license.usage.incremented":
		eventName = "UsageIncremented"
	default:
		return nil
	}

	if err := p.enqueue(ctx, EventTopic, eventName, LicenseSnapshot(license, code, p.clock())); err != nil {
		return err
	}

	if name != "license.expiring-soon" || !license.OwnerID.Valid || !p.delivery.Bound() {
		return nil
	}

	return p.expiringSoon(ctx, license)
}

// LicenseSnapshot excludes license credentials and arbitrary metadata from events.
func LicenseSnapshot(license domain.License, code string, now time.Time) port.LicenseEvent {
	return port.LicenseEvent{
		AccountID: license.AccountID, LicenseID: license.ID, ProductID: license.ProductID,
		PolicyID: license.PolicyID, UserID: license.OwnerID.UUID, Expiry: license.Expiry, Uses: license.Uses,
		Suspended: license.Suspended, ValidationCode: code, OccurredAt: now,
	}
}

func (p Publisher) expiringSoon(ctx context.Context, license domain.License) error {
	user, err := p.store.UserByID(ctx, license.AccountID, license.OwnerID.UUID)
	if err != nil {
		return fmt.Errorf("automation: expiring license owner: %w", err)
	}

	product, err := p.store.ProductByID(ctx, license.AccountID, license.ProductID)
	if err != nil {
		return fmt.Errorf("automation: expiring license product: %w", err)
	}

	expiry := ""
	if license.Expiry != nil {
		expiry = license.Expiry.UTC().Format(time.RFC3339)
	}

	return p.enqueue(ctx, DeliveryTopic, "Deliver", port.DeliverIn{
		AccountID: license.AccountID, Template: "lic.license-expiring-soon", Recipient: user.Email, Locale: p.locale,
		Data: port.DeliveryData{
			UserID: user.ID, UserName: strings.TrimSpace(user.FirstName + " " + user.LastName),
			UserEmail: user.Email, LicenseID: license.ID, LicenseKey: visibleLicenseKey(license), LicenseExpiry: expiry,
			ProductName: product.Name,
		},
	})
}
