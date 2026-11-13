// Package machines is the machine scenario: index, show, activation,
// update, deactivation, the heartbeat actions, machine-file check-outs
// and the relationship routes (product, license, owner, user, group).
package machines

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the scenario's storage not-found sentinel.
var ErrNotFound = domain.ErrNotFound

// Scope is the bearer's authorized scope (the reference's
// authorized_scope): the zero value is the whole account.
type Scope struct {
	// Kind is "", "product", "user" or "license".
	Kind string
	ID   uuid.UUID
}

// ScopeOf is the principal's machine scope: a product its product's
// machines, a user the machines of the licenses it owns or uses, a
// license its own.
func ScopeOf(p access.Principal) Scope {
	switch p.Role {
	case domain.RoleProduct:
		return Scope{Kind: kindProduct, ID: p.BearerID}
	case domain.RoleUser:
		return Scope{Kind: kindUser, ID: p.BearerID}
	case domain.RoleLicense:
		return Scope{Kind: kindLicense, ID: p.BearerID}
	default:
		return Scope{}
	}
}

// Filter is the index filter set (the reference's has_scope list). The
// owner and user filters take an id or an email.
type Filter struct {
	IDs         []uuid.UUID
	Fingerprint string
	IP          string
	Hostname    string
	// Status is ALIVE or DEAD; any other value matches nothing.
	Status    string
	ProductID uuid.NullUUID
	PolicyID  uuid.NullUUID
	LicenseID uuid.NullUUID
	Key       string
	// Owner is an id or an email; OwnerNull filters unowned machines.
	Owner     string
	OwnerNull bool
	// User is an id or an email of a license user.
	User     string
	GroupID  uuid.NullUUID
	Metadata map[string]any
}

// Owner selects the machines of one owner for the per-user limits; an
// invalid id is the unowned machines (the reference's where(owner: nil)).
type Owner = uuid.NullUUID

// Totals are a license's machine count and resource sums.
type Totals struct {
	Count  int64
	Cores  int64
	Memory int64
	Disk   int64
}

// Uniqueness is the space a fingerprint must be unique in.
type Uniqueness struct {
	// Kind is "account", "product", "policy" or "license".
	Kind string
	ID   uuid.UUID
}

// Store is the scenario's storage port: the machines, the rules their
// limits read, the components, the processes and the cull's batches.
type Store interface {
	MachineStore
	MachineLimitStore
	ComponentStore
	ProcessStore
	CullStore
}

// MachineStore reads and writes machine rows.
type MachineStore interface {
	ListMachines(ctx context.Context, accountID uuid.UUID, f Filter, scope Scope, page paging.Page,
		now time.Time) ([]domain.Machine, error)
	CountMachines(ctx context.Context, accountID uuid.UUID, f Filter, scope Scope, now time.Time) (int, error)
	// FindMachine resolves an id or a fingerprint within the scope, the
	// oldest machine first.
	FindMachine(ctx context.Context, accountID uuid.UUID, scope Scope, idOrFingerprint string,
		now time.Time) (domain.Machine, error)
	InsertMachine(ctx context.Context, m domain.Machine) error
	UpdateMachine(ctx context.Context, m domain.Machine) error
	DeleteMachine(ctx context.Context, accountID, id uuid.UUID) error
	MachineExists(ctx context.Context, accountID, id uuid.UUID) (bool, error)
}

// MachineLimitStore answers the uniqueness and limit questions of an
// activation.
type MachineLimitStore interface {
	FingerprintTaken(ctx context.Context, accountID uuid.UUID, in Uniqueness, fingerprint string) (bool, error)
	// MachineTotals sums a license's machines other than except, of one
	// owner when owner is set (per-user leasing).
	MachineTotals(ctx context.Context, licenseID uuid.UUID, perUser bool, owner Owner,
		except uuid.UUID) (Totals, error)
	// RefreshLicenseCounters rewrites the license's machine counter cache.
	RefreshLicenseCounters(ctx context.Context, accountID, licenseID uuid.UUID, now time.Time) error
	LicenseHasUser(ctx context.Context, licenseID, userID uuid.UUID) (bool, error)
	GroupMachineCount(ctx context.Context, groupID uuid.UUID) (int, error)
}

// ComponentStore reads and writes component rows.
type ComponentStore interface {
	// ListComponents reads a page of the scoped components and the total.
	ListComponents(ctx context.Context, accountID uuid.UUID, f SubFilter, scope Scope,
		page paging.Page) ([]domain.MachineComponent, int, error)
	FindComponent(ctx context.Context, accountID uuid.UUID, scope Scope, id uuid.UUID) (domain.MachineComponent, error)
	InsertComponent(ctx context.Context, c domain.MachineComponent) error
	UpdateComponent(ctx context.Context, c domain.MachineComponent) error
	DeleteComponent(ctx context.Context, accountID, id uuid.UUID) error
	ComponentFingerprintTaken(
		ctx context.Context, accountID uuid.UUID, in Uniqueness, fingerprint string,
	) (bool, error)
}

// ProcessStore reads and writes process rows.
type ProcessStore interface {
	// ListProcesses reads a page of the scoped processes and the total.
	ListProcesses(ctx context.Context, accountID uuid.UUID, f SubFilter, scope Scope, page paging.Page,
		now time.Time) ([]domain.MachineProcess, int, error)
	FindProcess(ctx context.Context, accountID uuid.UUID, scope Scope, id uuid.UUID,
		now time.Time) (domain.MachineProcess, error)
	InsertProcess(ctx context.Context, p domain.MachineProcess) error
	UpdateProcess(ctx context.Context, p domain.MachineProcess) error
	DeleteProcess(ctx context.Context, accountID, id uuid.UUID) error
	ProcessExists(ctx context.Context, id uuid.UUID) (bool, error)
	ProcessPidTaken(ctx context.Context, machineID uuid.UUID, pid string) (bool, error)
	// ProcessCount counts every process (alive or dead) of a machine, of a
	// license, or of a license's owner (kind "user", owner nil: unowned).
	ProcessCount(ctx context.Context, kind string, scopeID uuid.UUID, owner Owner) (int64, error)
}

// CullStore reads the cull's batches.
type CullStore interface {
	// DeadMachines and DeadProcesses are the cull's batches, every account.
	DeadMachines(ctx context.Context, now time.Time, limit int) ([]Ref, error)
	DeadProcesses(ctx context.Context, now time.Time, limit int) ([]Ref, error)
}

// LicenseStore reads and locks licenses.
type LicenseStore interface {
	LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.License, error)
	// LockLicense takes the license row lock for the transaction: the
	// limit checks and the counter cache of activations serialize on it.
	LockLicense(ctx context.Context, accountID, id uuid.UUID) error
}

// TokenStore counts an activation token's activations.
type TokenStore interface {
	TokenByID(ctx context.Context, accountID, id uuid.UUID) (domain.Token, error)
	// LockToken locks the token row for the transaction: NOWAIT when it
	// has a limit, else SKIP LOCKED; ok is false when the lock was not
	// taken (the reference then skips the count).
	LockToken(ctx context.Context, accountID, id uuid.UUID, nowait bool) (domain.Token, bool, error)
	SetTokenCounters(ctx context.Context, accountID, id uuid.UUID, activations, deactivations int,
		now time.Time) error
}

// Directory reads the related records of a machine.
type Directory interface {
	UserByID(ctx context.Context, accountID, id uuid.UUID) (domain.User, error)
	UserHasActiveLicense(ctx context.Context, accountID, userID uuid.UUID) (bool, error)
	GroupByID(ctx context.Context, accountID, id uuid.UUID) (domain.Group, error)
	ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error)
}

// LicenseIncludes resolves the license.* includes of a machine file
// (the license scenario's own check-out includes).
type LicenseIncludes interface {
	Included(ctx context.Context, account domain.Account, license domain.License,
		includes []string) (licenses.Checkout, error)
}

// Scenario runs the machine reads and writes.
type Scenario struct {
	store     Store
	licenses  LicenseStore
	tokens    TokenStore
	usage     UsageStore
	directory Directory
	included  LicenseIncludes
	events    events.Publisher
	clock     func() time.Time
	doTx      func(ctx context.Context, fn func(context.Context) error) error
}

// New builds the scenario over the repositories.
func New(
	store Store, licenseStore LicenseStore, tokens TokenStore, usage UsageStore, directory Directory,
	included LicenseIncludes, publisher events.Publisher, clock func() time.Time,
	doTx func(ctx context.Context, fn func(context.Context) error) error,
) Scenario {
	return Scenario{
		store: store, licenses: licenseStore, tokens: tokens, usage: usage, directory: directory, included: included,
		events: publisher, clock: clock, doTx: doTx,
	}
}

// The scope and uniqueness kinds the store switches on.
const (
	kindMachine = "machine"
	kindProduct = "product"
	kindUser    = "user"
	kindLicense = "license"
)

// topicComponents names the components violation document.
const topicComponents = "components"

// isReservedWord answers whether a fingerprint or pid collides with the
// actions routes.
func isReservedWord(value string) bool { return value == "actions" || value == "action" }

// List is one index page (with its total and next cursor).
type List struct {
	Items []domain.Machine
	Count int
	Next  uuid.NullUUID
}

func (s Scenario) now() time.Time { return s.clock() }

func (s Scenario) write(ctx context.Context, fn func(context.Context) error) error {
	return s.doTx(ctx, fn)
}

func (s Scenario) publish(ctx context.Context, event string, resource any) error {
	if err := s.events.Publish(ctx, event, resource); err != nil {
		return fmt.Errorf("machines: publish %s: %w", event, err)
	}

	return nil
}
