package automation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

var (
	ErrMissingKey  = errors.New("automation: activity idempotency key is required")
	ErrKeyConflict = errors.New("automation: idempotency key reused with different input")
)

// Activities reuse the HTTP API's licensing scenarios, including authorization,
// policy checks and key generation. Only persistence uses consumer-side ports.
type Activities struct {
	store     port.ActivityReceipts
	licenses  licenses.Scenario
	accountID uuid.UUID
	clock     func() time.Time
}

func NewActivities(store port.ActivityReceipts, scenario licenses.Scenario, accountID uuid.UUID,
	clock func() time.Time,
) Activities {
	return Activities{store: store, licenses: scenario, accountID: accountID, clock: clock}
}

func adminContext(ctx context.Context) context.Context {
	return access.With(ctx, access.Principal{
		BearerType: access.BearerUser, Role: domain.RoleAdmin,
		Permissions: access.RolePermissions(domain.RoleAdmin),
	})
}

func (s Activities) CreateLicense(ctx context.Context, in port.CreateLicenseIn, key string) (port.LicenseOut, error) {
	return execute(ctx, s, "CreateLicense", key, in,
		func(ctx context.Context, account domain.Account) (port.LicenseOut, error) {
			input := licenses.Input{
				PolicyID: uuid.NullUUID{UUID: in.PolicyID, Valid: in.PolicyID != uuid.Nil},
				Name:     in.Name, Key: in.Key, Expiry: in.Expiry, ExpirySet: in.Expiry != nil,
				Metadata: in.Metadata, MetadataSet: in.Metadata != nil,
			}
			if in.OwnerID != nil {
				input.Owner = uuid.NullUUID{UUID: *in.OwnerID, Valid: true}
				input.OwnerSet = true
			}

			license, err := s.licenses.Create(ctx, account, input)

			return s.output(license), err
		})
}

func (s Activities) GetLicense(ctx context.Context, in port.LicenseIn, _ string) (port.LicenseOut, error) {
	account, err := s.store.Account(ctx, s.accountID)
	if err != nil {
		return port.LicenseOut{}, fmt.Errorf("automation: account: %w", err)
	}

	license, err := s.licenses.Show(adminContext(ctx), account, in.License)
	if err != nil {
		return port.LicenseOut{}, fmt.Errorf("automation: get license: %w", err)
	}

	return s.output(license), nil
}

func (s Activities) SuspendLicense(ctx context.Context, in port.LicenseIn, key string) (port.LicenseOut, error) {
	return execute(ctx, s, "SuspendLicense", key, in,
		func(ctx context.Context, account domain.Account) (port.LicenseOut, error) {
			license, err := s.licenses.Suspend(ctx, account, in.License)
			return s.output(license), err
		})
}

func (s Activities) RenewLicense(ctx context.Context, in port.LicenseIn, key string) (port.LicenseOut, error) {
	return execute(ctx, s, "RenewLicense", key, in,
		func(ctx context.Context, account domain.Account) (port.LicenseOut, error) {
			license, err := s.licenses.Renew(ctx, account, in.License)
			return s.output(license), err
		})
}

func (s Activities) IncrementUsage(ctx context.Context, in port.IncrementUsageIn, key string) (port.LicenseOut, error) {
	return execute(ctx, s, "IncrementUsage", key, in,
		func(ctx context.Context, account domain.Account) (port.LicenseOut, error) {
			license, err := s.licenses.IncrementUsage(ctx, account, in.License, in.Increment)
			return s.output(license), err
		})
}

func (s Activities) ValidateKey(ctx context.Context, in port.ValidateKeyIn, key string) (port.ValidateKeyOut, error) {
	return execute(ctx, s, "ValidateKey", key, in,
		func(ctx context.Context, account domain.Account) (port.ValidateKeyOut, error) {
			scope := licenses.Scope{
				Present: true, Product: in.Product, Policy: in.Policy,
				Fingerprints: in.Fingerprints, Entitlements: in.Entitlements,
			}
			license, verdict, err := s.licenses.ValidateKey(ctx, account, in.Key, false, scope)
			out := port.ValidateKeyOut{Valid: verdict.Valid, Code: verdict.Code, Detail: verdict.Detail}

			if license != nil {
				result := s.output(*license)
				out.License = &result
			}

			return out, err //nolint:wrapcheck // execute wraps the scenario error with its activity identity
		})
}

func (s Activities) output(license domain.License) port.LicenseOut {
	out := port.LicenseOut{License: LicenseSnapshot(license, "", s.clock()), Key: visibleLicenseKey(license)}

	if license.Name != nil {
		out.Name = *license.Name
	}

	return out
}

func visibleLicenseKey(license domain.License) string {
	if license.RawKey != nil {
		return *license.RawKey
	}

	if license.Policy.Scheme == "LEGACY_ENCRYPT" {
		return "" // the stored bcrypt digest is not a license credential
	}

	return license.Key
}

// execute commits the activity result and every business effect together. The
// xact lock serializes concurrent attempts even before a receipt row exists.
func execute[In, Out any](ctx context.Context, s Activities, name, key string, in In,
	run func(context.Context, domain.Account) (Out, error),
) (Out, error) {
	var out Out
	if key == "" {
		return out, ErrMissingKey
	}

	input, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("automation: activity input: %w", err)
	}

	digest := sha256.Sum256(input)
	hash := hex.EncodeToString(digest[:])
	receiptKey := name + "/" + key

	err = s.store.Do(adminContext(ctx), func(ctx context.Context) error {
		if err := s.store.LockAutomationActivity(ctx, s.accountID, receiptKey); err != nil {
			return fmt.Errorf("lock: %w", err)
		}

		previous, err := s.store.AutomationActivityResult(ctx, s.accountID, receiptKey)
		if err == nil {
			if previous.RequestHash != hash {
				return ErrKeyConflict
			}

			if err := json.Unmarshal(previous.Result, &out); err != nil {
				return fmt.Errorf("decode receipt: %w", err)
			}

			return nil
		}

		if !errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("read receipt: %w", err)
		}

		account, err := s.store.Account(ctx, s.accountID)
		if err != nil {
			return fmt.Errorf("account: %w", err)
		}

		out, err = run(ctx, account)
		if err != nil {
			return err
		}

		result, err := json.Marshal(out)
		if err != nil {
			return fmt.Errorf("encode receipt: %w", err)
		}

		if err := s.store.SaveAutomationActivityResult(ctx, s.accountID, receiptKey,
			port.ActivityReceipt{RequestHash: hash, Result: result}); err != nil {
			return fmt.Errorf("save receipt: %w", err)
		}

		return nil
	})
	if err != nil {
		return out, fmt.Errorf("automation: %s: %w", name, err)
	}

	return out, nil
}
