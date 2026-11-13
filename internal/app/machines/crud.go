package machines

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// uuidRe is the reference's UUID_RE.
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Attribute limits of the machine model.
const (
	maxFingerprintLength = 4096
	maxTextLength        = 255
	maxNestedComponents  = 20
)

// Input is the decoded create or update body; nil fields are absent.
type Input struct {
	// ID is the client-chosen id of a create.
	ID          string
	Fingerprint string
	Name        Field[string]
	IP          Field[string]
	Hostname    Field[string]
	Platform    Field[string]
	Cores       Field[int64]
	Memory      Field[int64]
	Disk        Field[int64]
	Metadata    Field[map[string]any]
	// LicenseID, Owner and Group are the create relationships.
	LicenseID  uuid.UUID
	Owner      uuid.NullUUID
	Group      uuid.NullUUID
	Components []ComponentInput
}

// Field is an optional attribute: Set distinguishes an explicit null
// (Set with a nil Value) from an absent one.
type Field[T any] struct {
	Set   bool
	Value *T
}

// ComponentInput is a component nested in an activation.
type ComponentInput struct {
	Name        string
	Fingerprint string
	Metadata    map[string]any
}

// Index answers the filtered page within the principal's scope.
func (s Scenario) Index(ctx context.Context, account domain.Account, f Filter, params paging.Params) (List, error) {
	if err := access.Require(ctx, "machine.read"); err != nil {
		return List{}, err
	}

	scope, window, now := ScopeOf(access.PrincipalOf(ctx)), params.Window(), s.now()

	items, err := s.store.ListMachines(ctx, account.ID, f, scope, window, now)
	if err != nil {
		return List{}, fmt.Errorf("machines: index: %w", err)
	}

	out := List{Items: items, Count: len(items)}

	if params.Mode == paging.ModeOffset {
		if out.Count, err = s.store.CountMachines(ctx, account.ID, f, scope, now); err != nil {
			return List{}, fmt.Errorf("machines: index count: %w", err)
		}
	}

	if params.Mode == paging.ModeCursor && len(items) > int(window.Limit) {
		out.Items = items[:window.Limit]
		out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].ID, Valid: true}
	}

	return out, nil
}

// Listed loads machines by id for a relationship route of another
// resource (a user's, a group's), whose scenario already authorized the
// listing; the order is newest first.
func (s Scenario) Listed(ctx context.Context, account domain.Account, ids []uuid.UUID) ([]domain.Machine, error) {
	if len(ids) == 0 {
		return []domain.Machine{}, nil
	}

	items, err := s.store.ListMachines(ctx, account.ID, Filter{IDs: ids}, Scope{},
		paging.Page{Fetch: int64(len(ids))}, s.now())
	if err != nil {
		return nil, fmt.Errorf("machines: listed: %w", err)
	}

	return items, nil
}

// Show answers one machine by id or fingerprint.
func (s Scenario) Show(ctx context.Context, account domain.Account, idOrFingerprint string) (domain.Machine, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorize(ctx, "machine.read", machine, access.MachineShow); err != nil {
		return domain.Machine{}, err
	}

	return machine, nil
}

// Create activates a machine on a license.
func (s Scenario) Create(ctx context.Context, account domain.Account, in Input) (domain.Machine, error) {
	principal, now := access.PrincipalOf(ctx), s.now()

	license, licensed, err := s.licenseOf(ctx, account, in.LicenseID)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorizeCreate(ctx, principal, license, licensed, in); err != nil {
		return domain.Machine{}, err
	}

	machine := domain.Machine{
		ID: uuid.New(), AccountID: account.ID, LicenseID: in.LicenseID, OwnerID: in.Owner, GroupID: in.Group,
		Fingerprint: in.Fingerprint, Name: in.Name.Value, IP: in.IP.Value, Hostname: in.Hostname.Value,
		Platform: in.Platform.Value, Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}

	if in.Metadata.Value != nil {
		machine.Metadata = *in.Metadata.Value
	}

	if uuidRe.MatchString(in.ID) {
		machine.ID = uuid.MustParse(in.ID)
	}

	if err := setResources(&machine, in); err != nil {
		return domain.Machine{}, err
	}

	if licensed {
		machine.PolicyID, machine.ProductID = license.PolicyID, license.ProductID
		machine.LicenseOwnerID, machine.LicenseProtected = license.OwnerID, license.ProtectedFlag()
		machine.Heartbeat, machine.LicenseMaxProcesses = license.Policy.HeartbeatOf(), license.MaxProcesses()
	}

	err = s.write(ctx, func(ctx context.Context) error {
		if licensed {
			if err := s.licenses.LockLicense(ctx, account.ID, license.ID); err != nil {
				return fmt.Errorf("machines: lock license: %w", err)
			}

			if license, err = s.licenses.LicenseByID(ctx, account.ID, license.ID); err != nil {
				return fmt.Errorf("machines: license: %w", err)
			}
		}

		if err := s.inherit(ctx, account, license, licensed, &machine); err != nil {
			return err
		}

		if err := s.validateCreate(ctx, account, license, licensed, machine, in); err != nil {
			return err
		}

		if err := s.countActivation(ctx, account, principal, true); err != nil {
			return err
		}

		return s.insert(ctx, account, machine, in.Components, now)
	})
	if err != nil {
		return domain.Machine{}, err
	}

	machine.HeartbeatStatus = machine.Status(now)

	return machine, nil
}

// Update merges the input over the machine. decode reads the body once
// the machine is found and authorized (the reference's order).
func (s Scenario) Update(
	ctx context.Context, account domain.Account, idOrFingerprint string, decode func() (Input, error),
) (domain.Machine, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, err
	}

	if err := s.authorize(ctx, "machine.update", machine, access.MachineUpdate); err != nil {
		return domain.Machine{}, err
	}

	in, err := decode()
	if err != nil {
		return domain.Machine{}, err
	}

	before := machine

	if in.Name.Set {
		machine.Name = in.Name.Value
	}

	if in.IP.Set {
		machine.IP = in.IP.Value
	}

	if in.Hostname.Set {
		machine.Hostname = in.Hostname.Value
	}

	if in.Platform.Set {
		machine.Platform = in.Platform.Value
	}

	if in.Metadata.Set {
		machine.Metadata = map[string]any{}
		if in.Metadata.Value != nil {
			machine.Metadata = *in.Metadata.Value
		}
	}

	if err := setResources(&machine, in); err != nil {
		return domain.Machine{}, err
	}

	machine.UpdatedAt = s.now()

	return s.save(ctx, account, before, machine, "machine.updated")
}

// Destroy deactivates the machine (its components and processes go
// with it).
func (s Scenario) Destroy(ctx context.Context, account domain.Account, idOrFingerprint string) error {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return err
	}

	if err := s.authorize(ctx, "machine.delete", machine, access.MachineDestroy); err != nil {
		return err
	}

	return s.write(ctx, func(ctx context.Context) error {
		if err := s.countActivation(ctx, account, access.PrincipalOf(ctx), false); err != nil {
			return err
		}

		if err := s.publish(ctx, "machine.deleted", machine); err != nil {
			return err
		}

		if err := s.store.DeleteMachine(ctx, account.ID, machine.ID); err != nil {
			return fmt.Errorf("machines: destroy: %w", err)
		}

		if err := s.store.RefreshLicenseCounters(ctx, account.ID, machine.LicenseID, s.now()); err != nil {
			return fmt.Errorf("machines: counters: %w", err)
		}

		return nil
	})
}

// load resolves the machine within the principal's scope: a machine
// outside it is the record 404.
func (s Scenario) load(ctx context.Context, account domain.Account, idOrFingerprint string) (domain.Machine, error) {
	machine, err := s.store.FindMachine(ctx, account.ID, ScopeOf(access.PrincipalOf(ctx)), idOrFingerprint, s.now())
	if errors.Is(err, ErrNotFound) {
		return domain.Machine{}, apperr.NotFound("machine", idOrFingerprint)
	}

	if err != nil {
		return domain.Machine{}, fmt.Errorf("machines: load: %w", err)
	}

	return machine, nil
}

// authorize checks the permission, then the record rule.
func (s Scenario) authorize(
	ctx context.Context, permission string, machine domain.Machine, rule func(access.Principal, domain.Machine) error,
) error {
	if err := access.Require(ctx, permission); err != nil {
		return err
	}

	return rule(access.PrincipalOf(ctx), machine)
}

// authorizeCreate is MachinePolicy#create? over the unsaved record.
func (s Scenario) authorizeCreate(
	ctx context.Context, principal access.Principal, license domain.License, licensed bool, in Input,
) error {
	permissions := []string{"machine.create"}
	if len(in.Components) > 0 {
		permissions = append(permissions, "component.create")
	}

	for _, permission := range permissions {
		if err := access.Require(ctx, permission); err != nil {
			return err
		}
	}

	return access.MachineCreate(principal, license, licensed, in.Owner)
}

// licenseOf loads the license of an activation; licensed is false when
// it does not exist (a validation error, after authorization).
func (s Scenario) licenseOf(ctx context.Context, account domain.Account, id uuid.UUID) (domain.License, bool, error) {
	if id == uuid.Nil {
		return domain.License{}, false, nil
	}

	license, err := s.licenses.LicenseByID(ctx, account.ID, id)
	if errors.Is(err, ErrNotFound) {
		return domain.License{}, false, nil
	}

	if err != nil {
		return domain.License{}, false, fmt.Errorf("machines: license: %w", err)
	}

	return license, true, nil
}

// inherit applies the before_validation callbacks of a create: the
// license's group, else the owner's; the first heartbeat at creation.
func (s Scenario) inherit(
	ctx context.Context, account domain.Account, license domain.License, licensed bool, machine *domain.Machine,
) error {
	if licensed && !machine.GroupID.Valid {
		machine.GroupID = license.GroupID
	}

	if machine.OwnerID.Valid && !machine.GroupID.Valid {
		owner, err := s.directory.UserByID(ctx, account.ID, machine.OwnerID.UUID)

		switch {
		case err == nil:
			machine.GroupID = owner.GroupID
		case !errors.Is(err, ErrNotFound):
			return fmt.Errorf("machines: owner: %w", err)
		}
	}

	if licensed && machine.Heartbeat.FromCreation() && machine.LastHeartbeatAt == nil {
		at := machine.CreatedAt
		machine.LastHeartbeatAt = &at
	}

	return nil
}

// insert writes the machine, its nested components and the license's
// counter cache, and announces it.
func (s Scenario) insert(
	ctx context.Context, account domain.Account, machine domain.Machine, components []ComponentInput, now time.Time,
) error {
	if err := s.store.InsertMachine(ctx, machine); err != nil {
		return fmt.Errorf("machines: create: %w", err)
	}

	for i, c := range components {
		// Each nested record is saved after the previous one.
		at := now.Add(time.Duration(i) * time.Microsecond)
		component := domain.MachineComponent{
			ID: uuid.New(), AccountID: account.ID, MachineID: machine.ID, Name: c.Name,
			Fingerprint: c.Fingerprint, Metadata: c.Metadata, CreatedAt: at, UpdatedAt: at,
		}

		if component.Metadata == nil {
			component.Metadata = map[string]any{}
		}

		if err := s.store.InsertComponent(ctx, component); err != nil {
			return fmt.Errorf("machines: component: %w", err)
		}
	}

	if err := s.store.RefreshLicenseCounters(ctx, account.ID, machine.LicenseID, now); err != nil {
		return fmt.Errorf("machines: counters: %w", err)
	}

	return s.publish(ctx, "machine.created", machine)
}

// save validates and writes an updated machine.
func (s Scenario) save(
	ctx context.Context, account domain.Account, before, machine domain.Machine, event string,
) (domain.Machine, error) {
	err := s.write(ctx, func(ctx context.Context) error {
		if err := s.licenses.LockLicense(ctx, account.ID, machine.LicenseID); err != nil {
			return fmt.Errorf("machines: lock license: %w", err)
		}

		license, err := s.licenses.LicenseByID(ctx, account.ID, machine.LicenseID)
		if err != nil {
			return fmt.Errorf("machines: license: %w", err)
		}

		if err := s.validateUpdate(ctx, account, license, before, machine); err != nil {
			return err
		}

		if err := s.store.UpdateMachine(ctx, machine); err != nil {
			return fmt.Errorf("machines: update: %w", err)
		}

		if err := s.store.RefreshLicenseCounters(ctx, account.ID, machine.LicenseID, machine.UpdatedAt); err != nil {
			return fmt.Errorf("machines: counters: %w", err)
		}

		return s.publish(ctx, event, machine)
	})
	if err != nil {
		return domain.Machine{}, err
	}

	machine.HeartbeatStatus = machine.Status(s.now())

	return machine, nil
}

// countActivation counts an activation (or a deactivation) against the
// activation token that authenticated it.
func (s Scenario) countActivation(
	ctx context.Context, account domain.Account, p access.Principal, activation bool,
) error {
	if !p.IsTokenAuthn() || !p.IsLicense() {
		return nil
	}

	token, err := s.tokens.TokenByID(ctx, account.ID, p.TokenID)
	if err != nil {
		return fmt.Errorf("machines: token: %w", err)
	}

	limited := token.MaxDeactivations != nil
	if activation {
		limited = token.MaxActivations != nil
	}

	// A limited token is locked NOWAIT, an unlimited one SKIP LOCKED; a
	// lock not taken skips the count, as in the reference.
	token, locked, err := s.tokens.LockToken(ctx, account.ID, p.TokenID, limited)
	if err != nil {
		return fmt.Errorf("machines: lock token: %w", err)
	}

	if !locked {
		return nil
	}

	activations, deactivations := token.Activations, token.Deactivations

	if activation {
		activations++
		if token.MaxActivations != nil && activations > *token.MaxActivations {
			return tokenLimit("ACTIVATIONS_LIMIT_EXCEEDED", "activations", *token.MaxActivations)
		}
	} else {
		deactivations++
		if token.MaxDeactivations != nil && deactivations > *token.MaxDeactivations {
			return tokenLimit("DEACTIVATIONS_LIMIT_EXCEEDED", "deactivations", *token.MaxDeactivations)
		}
	}

	if err := s.tokens.SetTokenCounters(ctx, account.ID, token.ID, activations, deactivations, s.now()); err != nil {
		return fmt.Errorf("machines: token counters: %w", err)
	}

	return nil
}

func tokenLimit(code, attribute string, limit int) error {
	return apperr.Validation(code, fmt.Sprintf("exceeds maximum allowed (%d)", limit),
		"/data/attributes/"+attribute, "tokens/#tokens-object-attrs-"+attribute)
}

// setResources applies cores, memory and disk, rejecting values outside
// the columns' ranges as the reference's numericality validation does.
func setResources(machine *domain.Machine, in Input) error {
	if in.Cores.Set {
		machine.Cores = nil

		if in.Cores.Value != nil {
			if *in.Cores.Value > math.MaxInt32 {
				return invalid("CORES_INVALID", "must be less than or equal to 2147483647", "/data/attributes/cores")
			}

			cores := int32(max(math.MinInt32, min(*in.Cores.Value, math.MaxInt32)))
			machine.Cores = &cores
		}
	}

	if in.Memory.Set {
		machine.Memory = in.Memory.Value
	}

	if in.Disk.Set {
		machine.Disk = in.Disk.Value
	}

	return nil
}

// invalid is a machine validation error with its docs link.
func invalid(code, detail, pointer string) *apperr.T {
	return apperr.Validation(code, detail, pointer, aboutOf("machines", pointer))
}

// aboutOf is the docs anchor of a pointer (the reference's error link).
func aboutOf(topic, pointer string) string {
	var hash strings.Builder

	hash.WriteString(topic + "-object")

	for _, part := range []struct{ prefix, anchor string }{
		{"/data/relationships/", "-relationships-"},
		{"/data/attributes/", "-attrs-"},
	} {
		if rest, ok := cut(pointer, part.prefix); ok {
			hash.WriteString(part.anchor + rest)
		}
	}

	return topic + "/#" + hash.String()
}

func cut(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return "", false
	}

	rest := s[len(prefix):]
	for i := range len(rest) {
		if rest[i] == '/' {
			return rest[:i], true
		}
	}

	return rest, true
}

func tooLong(value *string, limit int) bool {
	return value != nil && utf8.RuneCountInString(*value) > limit
}
