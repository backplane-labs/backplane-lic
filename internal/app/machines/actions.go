package machines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// Ping records a heartbeat; a dead machine is resurrected when its
// policy allows it, else the ping is refused.
func (s Scenario) Ping(ctx context.Context, account domain.Account, idOrFingerprint string) (domain.Machine, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorize(ctx, "machine.heartbeat.ping", machine, access.MachinePing); err != nil {
		return domain.Machine{}, err
	}

	now := s.now()
	event := "machine.heartbeat.ping"

	if machine.HeartbeatStatus == domain.MachineDead {
		if !machine.Heartbeat.ResurrectDead() || machine.ResurrectionPeriodPassed(now) {
			return domain.Machine{}, apperr.Unprocessable("MACHINE_HEARTBEAT_DEAD", "is dead", "")
		}

		event = "machine.heartbeat.resurrected"
		machine.LastDeathEventSentAt = nil
	}

	machine.LastHeartbeatAt, machine.UpdatedAt = &now, now

	if err := s.heartbeat(ctx, machine, event); err != nil {
		return domain.Machine{}, err
	}

	machine.HeartbeatStatus = machine.Status(now)
	if event == "machine.heartbeat.resurrected" {
		machine.HeartbeatStatus = domain.HeartbeatResurrected
	}

	return machine, nil
}

// Reset clears the heartbeat.
func (s Scenario) Reset(ctx context.Context, account domain.Account, idOrFingerprint string) (domain.Machine, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorize(ctx, "machine.heartbeat.reset", machine, access.MachineResetHeartbeat); err != nil {
		return domain.Machine{}, err
	}

	now := s.now()
	machine.LastHeartbeatAt, machine.UpdatedAt = nil, now

	if err := s.heartbeat(ctx, machine, "machine.heartbeat.reset"); err != nil {
		return domain.Machine{}, err
	}

	machine.HeartbeatStatus = machine.Status(now)

	return machine, nil
}

// heartbeat writes the heartbeat columns and announces the event.
func (s Scenario) heartbeat(ctx context.Context, machine domain.Machine, event string) error {
	return s.write(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateMachine(ctx, machine); err != nil {
			return fmt.Errorf("machines: heartbeat: %w", err)
		}

		return s.publish(ctx, event, machine)
	})
}

// includeLimit bounds the components a machine file embeds.
const includeLimit = 1000

// checkoutIncluded answers whether include is in
// MachineCheckoutService::ALLOWED_INCLUDES.
func checkoutIncluded(include string) bool {
	switch include {
	case "license.entitlements", "license.product", "license.policy", "license.owner", "license.users",
		kindLicense, topicComponents, "environment", "group", "owner":
		return true
	default:
		return false
	}
}

// CheckoutParams are the merged query and meta parameters of a
// check-out.
type CheckoutParams struct {
	Encrypt   *bool
	Algorithm string
	TTL       *int32
	// TTLSet distinguishes an absent ttl (the default month) from an
	// explicit null (perpetual).
	TTLSet  bool
	Include []string
}

// Checkout is a machine file's content: the machine, the requested
// includes and the resolved parameters.
type Checkout struct {
	Machine  domain.Machine
	License  domain.License
	Includes []string
	// Included is the license's own includes (license.*).
	Included licenses.Checkout
	// Owner, Group and Components are the machine's includes.
	Owner       *domain.User
	OwnerStatus string
	Group       *domain.Group
	Components  []domain.MachineComponent

	Algorithm string
	Encrypted bool
	TTL       *int32
	IssuedAt  time.Time
	Expiry    *time.Time
}

// Checkout builds the machine file's content and stamps the check-out;
// decode reads the parameters before the authorization.
func (s Scenario) Checkout(
	ctx context.Context, account domain.Account, idOrFingerprint string, decode func() (CheckoutParams, error),
) (Checkout, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return Checkout{}, err
	}

	params, err := decode()
	if err != nil {
		return Checkout{}, err
	}

	if err := s.authorize(ctx, "machine.check-out", machine, access.MachineCheckOut); err != nil {
		return Checkout{}, err
	}

	license, err := s.licenses.LicenseByID(ctx, account.ID, machine.LicenseID)
	if err != nil {
		return Checkout{}, fmt.Errorf("machines: checkout license: %w", err)
	}

	out, err := checkoutOf(machine, license, params, s.now())
	if err != nil {
		return Checkout{}, err
	}

	if err := s.loadIncludes(ctx, account, &out); err != nil {
		return Checkout{}, err
	}

	err = s.write(ctx, func(ctx context.Context) error {
		machine.LastCheckOutAt = &out.IssuedAt

		if err := s.store.UpdateMachine(ctx, machine); err != nil {
			return fmt.Errorf("machines: checkout: %w", err)
		}

		return s.publish(ctx, "machine.checked-out", machine)
	})
	if err != nil {
		return Checkout{}, err
	}

	out.Machine = machine

	return out, nil
}

// checkoutOf resolves the parameters of a check-out.
func checkoutOf(
	machine domain.Machine, license domain.License, params CheckoutParams, now time.Time,
) (Checkout, error) {
	includes := make([]string, 0, len(params.Include))

	for _, include := range params.Include {
		if include == "license.user" {
			include = "license.owner"
		}

		if !checkoutIncluded(include) {
			return Checkout{}, checkoutError("CHECKOUT_INCLUDE_INVALID", "invalid includes", "include")
		}

		includes = append(includes, include)
	}

	ttl := params.TTL
	if !params.TTLSet {
		month := crypto.DefaultTTL()
		ttl = &month
	}

	encrypt := params.Encrypt != nil && *params.Encrypt

	algorithm, err := crypto.ResolveFileAlgorithm(
		params.Algorithm, encrypt, crypto.SchemeSigningAlgorithm(license.Policy.Scheme), ttl)
	if err != nil {
		var (
			algErr *crypto.FileAlgorithmError
			ttlErr *crypto.FileTTLError
		)

		switch {
		case errors.As(err, &algErr):
			return Checkout{}, checkoutError("CHECKOUT_ALGORITHM_INVALID", algErr.Detail, "algorithm")
		case errors.As(err, &ttlErr):
			return Checkout{}, checkoutError("CHECKOUT_TTL_INVALID", "must be greater than or equal to 3600 (1 hour)", "ttl")
		default:
			return Checkout{}, fmt.Errorf("machines: checkout: %w", err)
		}
	}

	out := Checkout{
		Machine: machine, License: license, Includes: includes, Algorithm: algorithm,
		Encrypted: crypto.FileAlgorithmEncrypted(algorithm), TTL: ttl, IssuedAt: now,
	}

	if ttl != nil {
		expiry := now.Add(time.Duration(*ttl) * time.Second)
		out.Expiry = &expiry
	}

	return out, nil
}

// loadIncludes resolves the requested includes.
func (s Scenario) loadIncludes(ctx context.Context, account domain.Account, out *Checkout) error {
	var licenseIncludes []string

	for _, include := range out.Includes {
		if rest, ok := cut(include, "license."); ok {
			licenseIncludes = append(licenseIncludes, rest)
		}
	}

	if len(licenseIncludes) > 0 {
		included, err := s.included.Included(ctx, account, out.License, licenseIncludes)
		if err != nil {
			return fmt.Errorf("machines: checkout license includes: %w", err)
		}

		out.Included = included
	}

	for _, include := range out.Includes {
		if err := s.loadInclude(ctx, account, out, include); err != nil {
			return err
		}
	}

	return nil
}

func (s Scenario) loadInclude(ctx context.Context, account domain.Account, out *Checkout, include string) error {
	machine := out.Machine

	switch include {
	case "owner":
		if !machine.OwnerID.Valid {
			return nil
		}

		owner, err := s.directory.UserByID(ctx, account.ID, machine.OwnerID.UUID)
		if err != nil {
			return fmt.Errorf("machines: checkout owner: %w", err)
		}

		active, err := s.directory.UserHasActiveLicense(ctx, account.ID, owner.ID)
		if err != nil {
			return fmt.Errorf("machines: checkout owner status: %w", err)
		}

		out.Owner, out.OwnerStatus = &owner, owner.Status(active, out.IssuedAt)
	case "group":
		if !machine.GroupID.Valid {
			return nil
		}

		group, err := s.directory.GroupByID(ctx, account.ID, machine.GroupID.UUID)
		if err != nil {
			return fmt.Errorf("machines: checkout group: %w", err)
		}

		out.Group = &group
	case topicComponents:
		components, _, err := s.store.ListComponents(ctx, account.ID,
			SubFilter{MachineID: uuid.NullUUID{UUID: machine.ID, Valid: true}}, Scope{},
			paging.Page{Fetch: includeLimit, Asc: true})
		if err != nil {
			return fmt.Errorf("machines: checkout components: %w", err)
		}

		out.Components = components
	}

	return nil
}

func checkoutError(code, detail, parameter string) error {
	return &apperr.T{
		Status: http.StatusBadRequest, Title: apperr.TitleBadRequest, Detail: detail, Code: code,
		Source: apperr.Source{Parameter: parameter},
	}
}

// Proof signs the machine's offline proof: the default dataset, or the
// caller's when its role may supply one.
func (s Scenario) Proof(
	ctx context.Context, account domain.Account, idOrFingerprint string, decode func() (map[string]any, error),
) (domain.Machine, string, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, "", err
	}

	if err := s.authorize(ctx, "machine.proofs.generate", machine, access.MachineProof); err != nil {
		return domain.Machine{}, "", err
	}

	dataset, err := decode()
	if err != nil {
		return domain.Machine{}, "", err
	}

	data := any(dataset)
	if dataset == nil {
		if data, err = s.defaultProofDataset(ctx, account, machine); err != nil {
			return domain.Machine{}, "", err
		}
	}

	raw, err := json.Marshal(data)
	if err != nil {
		return domain.Machine{}, "", fmt.Errorf("machines: proof dataset: %w", err)
	}

	proof, err := crypto.MachineProof(account.Keys, raw)
	if err != nil {
		return domain.Machine{}, "", fmt.Errorf("machines: proof: %w", err)
	}

	if err := s.publish(ctx, "machine.proofs.generated", machine); err != nil {
		return domain.Machine{}, "", err
	}

	return machine, proof, nil
}

// proofDataset is Machine#default_proof_dataset.
type proofDataset struct {
	Account struct {
		ID string `json:"id"`
	} `json:"account"`
	Product struct {
		ID string `json:"id"`
	} `json:"product"`
	Policy struct {
		ID       string `json:"id"`
		Duration *int32 `json:"duration"`
	} `json:"policy"`
	License struct {
		ID     string  `json:"id"`
		Key    string  `json:"key"`
		Expiry *string `json:"expiry"`
	} `json:"license"`
	Machine struct {
		ID          string `json:"id"`
		Fingerprint string `json:"fingerprint"`
		Created     string `json:"created"`
	} `json:"machine"`
	TS string `json:"ts"`
}

func (s Scenario) defaultProofDataset(
	ctx context.Context, account domain.Account, machine domain.Machine,
) (proofDataset, error) {
	license, err := s.licenses.LicenseByID(ctx, account.ID, machine.LicenseID)
	if err != nil {
		return proofDataset{}, fmt.Errorf("machines: proof license: %w", err)
	}

	const millis = "2006-01-02T15:04:05.000Z07:00"

	var d proofDataset

	d.Account.ID = account.ID.String()
	d.Product.ID = license.ProductID.String()
	d.Policy.ID, d.Policy.Duration = license.PolicyID.String(), license.Policy.Duration
	d.License.ID, d.License.Key = license.ID.String(), license.Key

	if license.Expiry != nil {
		expiry := license.Expiry.UTC().Format(millis)
		d.License.Expiry = &expiry
	}

	d.Machine.ID, d.Machine.Fingerprint = machine.ID.String(), machine.Fingerprint
	d.Machine.Created = machine.CreatedAt.UTC().Format(millis)
	// ts is a Time serialized by JSON.generate: Ruby's Time#to_s.
	d.TS = s.now().UTC().Format("2006-01-02 15:04:05 UTC")

	return d, nil
}
