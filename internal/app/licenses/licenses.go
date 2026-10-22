// Package licenses is the license scenario: the full CRUD surface with
// key generation, every relationship route (machines, tokens, product,
// policy transfers, group, owner, users, entitlements), the actions
// (validate, validate-key, revoke, renew, suspend, reinstate, check-in,
// usage counters) and license-file checkouts.
package licenses

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/tokens"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the scenario's storage not-found sentinel.
var ErrNotFound = domain.ErrNotFound

// ErrKeyTaken: the custom key already exists in the account.
var ErrKeyTaken = errors.New("licenses: key taken")

// Filter is the licenses index filter set (the reference's has_scope
// declarations); the hash filters arrive pre-resolved to timestamp
// bounds.
type Filter struct {
	ProductID uuid.NullUUID
	PolicyID  uuid.NullUUID
	// OwnerID filters by owner id; OwnerEmail (exact) and
	// OwnerEmailLike the non-UUID owner forms.
	OwnerID        uuid.NullUUID
	OwnerEmail     string
	OwnerEmailLike string
	// UserID/UserEmail/UserEmailLike filter by owner ∪ licensees.
	UserID        uuid.NullUUID
	UserEmail     string
	UserEmailLike string
	MachineID     uuid.NullUUID
	GroupID       uuid.NullUUID
	// Status filters by the computed status; unknown values match
	// nothing (StatusNoMatch).
	Status string
	// Suspended/Expiring/Expired/Unexpired are boolean filters
	// (Unexpired is the negated expired flag).
	Suspended *bool
	Expiring  *bool
	Expired   *bool
	Unexpired *bool
	// ExpiryGte1/ExpiryGte2/ExpiryLte narrow the expiry window of the
	// expired/expires hash filters.
	ExpiryGte1 *time.Time
	ExpiryGte2 *time.Time
	ExpiryLte  *time.Time
	// ActivityActiveSince/ActivityInactiveSince resolve the activity
	// hash filter.
	ActivityActiveSince   *time.Time
	ActivityInactiveSince *time.Time
	// Metadata is the metadata[k]=v containment filter.
	Metadata map[string]any
	// Assigned/Unassigned/Activated are boolean flags.
	Assigned   *bool
	Unassigned *bool
	Activated  *bool
	// Activations* compare machines_count.
	ActivationsEq  *int32
	ActivationsGt  *int32
	ActivationsGte *int32
	ActivationsLt  *int32
	ActivationsLte *int32
}

// StatusNoMatch is the status filter sentinel matching nothing.
const StatusNoMatch = "NONE"

// MachineFilter is the machine relationship index filter.
type MachineFilter struct {
	Fingerprint string
	ProductID   uuid.NullUUID
	UserID      uuid.NullUUID
}

// MachineSums are the owner-scoped machine count and resource sums.
type MachineSums struct {
	Count  int
	Cores  int
	Memory int64
	Disk   int64
}

// Store is the scenario's consumer-side storage port.
type Store interface { //nolint:interfacebloat // one repository, one port
	LicenseByID(ctx context.Context, accountID, id uuid.UUID) (domain.License, error)
	LicenseByKey(ctx context.Context, accountID uuid.UUID, key string) (domain.License, error)
	InsertLicense(ctx context.Context, license domain.License) error
	UpdateLicense(ctx context.Context, license domain.License) error
	DeleteLicense(ctx context.Context, accountID, id uuid.UUID) error
	ListLicenses(ctx context.Context, accountID uuid.UUID, f Filter, scopeKind string,
		scopeID uuid.UUID, page paging.Page, now time.Time) ([]domain.License, error)
	CountLicenses(ctx context.Context, accountID uuid.UUID, f Filter, scopeKind string,
		scopeID uuid.UUID, now time.Time) (int, error)
	LicenseKeyTaken(ctx context.Context, accountID uuid.UUID, key string) (bool, error)
	LicenseIDExists(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	SetLicenseExpiry(ctx context.Context, accountID, id uuid.UUID, expiry *time.Time, now time.Time) error
	SetLicenseUses(ctx context.Context, accountID, id uuid.UUID, uses int32, now time.Time) error
	SetLicenseSuspended(ctx context.Context, accountID, id uuid.UUID, suspended bool, now time.Time) error
	TouchLicenseValidated(ctx context.Context, accountID, id uuid.UUID, at time.Time, checksum, version *string) error
	SetLicenseLastCheckIn(ctx context.Context, accountID, id uuid.UUID, at, now time.Time) error
	SetLicenseLastCheckOut(ctx context.Context, accountID, id uuid.UUID, at, now time.Time) error
	TransferLicense(ctx context.Context, accountID, id, policyID, productID uuid.UUID,
		expiry *time.Time, now time.Time) error
	SetLicenseOwner(ctx context.Context, accountID, id uuid.UUID, owner, group uuid.NullUUID, now time.Time) error
	SetLicenseGroup(ctx context.Context, accountID, id uuid.UUID, group uuid.NullUUID, now time.Time) error
	CountLicensesOfGroup(ctx context.Context, accountID, groupID uuid.UUID) (int, error)
	InsertLicenseUsers(ctx context.Context, joinID, accountID, licenseID uuid.UUID, userIDs []uuid.UUID) error
	DeleteLicenseUsers(ctx context.Context, accountID, licenseID uuid.UUID, userIDs []uuid.UUID) (int, error)
	CountLicenseUsersOf(ctx context.Context, accountID, licenseID uuid.UUID, userIDs []uuid.UUID) (int, error)
	SetLicenseUsersCount(ctx context.Context, accountID, licenseID uuid.UUID, count int32) error
	ListLicenseUserRows(ctx context.Context, accountID, licenseID uuid.UUID,
		userIDs []uuid.UUID) ([]domain.LicenseUser, error)
	LicenseeIsUser(ctx context.Context, accountID, licenseID, userID uuid.UUID) (bool, error)
	InsertLicenseEntitlements(ctx context.Context, joinID, accountID, licenseID uuid.UUID,
		entitlementIDs []uuid.UUID) error
	DeleteLicenseEntitlements(ctx context.Context, accountID, licenseID uuid.UUID,
		entitlementIDs []uuid.UUID) (int, error)
	CountLicenseEntitlementsOf(ctx context.Context, accountID, licenseID uuid.UUID,
		entitlementIDs []uuid.UUID) (int, error)
	ListLicenseEntitlementRows(ctx context.Context, accountID, licenseID uuid.UUID,
		entitlementIDs []uuid.UUID) ([]domain.LicenseEntitlement, error)
	CountPolicyEntitlementsOf(ctx context.Context, accountID, policyID uuid.UUID,
		entitlementIDs []uuid.UUID) (int, error)
	ListLicenseEntitlementCodes(ctx context.Context, accountID, policyID,
		licenseID uuid.UUID) ([]string, error)
	CountLicenseEntitlementCodes(ctx context.Context, accountID, policyID,
		licenseID uuid.UUID, codes []string) (int, error)
	LicenseUserMatches(ctx context.Context, accountID, licenseID, userID uuid.UUID, email *string) (bool, error)
	LicenseMachines(ctx context.Context, accountID, licenseID uuid.UUID, f MachineFilter,
		now time.Time) ([]domain.Machine, error)
	LicenseMachineByID(ctx context.Context, accountID, licenseID, id uuid.UUID, now time.Time) (domain.Machine, error)
	LicenseMachineByFingerprint(ctx context.Context, accountID, licenseID uuid.UUID,
		fingerprint string, now time.Time) (domain.Machine, error)
	SumLicenseMachinesForOwner(ctx context.Context, accountID, licenseID uuid.UUID, owner *uuid.UUID) (MachineSums, error)
	CountMachineComponentsOf(ctx context.Context, machineID uuid.UUID, fingerprints []string) (int, error)
	CountLicenseProcesses(ctx context.Context, accountID, licenseID uuid.UUID, owner *uuid.UUID) (int, error)
	CountMachineProcesses(ctx context.Context, accountID, machineID uuid.UUID) (int, error)
}

// EntitlementListStore pages the union entitlement documents.
type EntitlementListStore interface {
	ListLicenseEntitlementsOf(ctx context.Context, accountID, policyID, licenseID uuid.UUID,
		page paging.Page) ([]domain.Entitlement, error)
	CountLicenseEntitlementsOfUnion(ctx context.Context, accountID, policyID, licenseID uuid.UUID) (int, error)
	LicenseEntitlementOf(ctx context.Context, accountID, policyID, licenseID, id uuid.UUID) (domain.Entitlement, error)
}

// PolicyStore is the policy seam (the create relationship and the
// transfer target).
type PolicyStore interface {
	PolicyByID(ctx context.Context, accountID, id uuid.UUID) (domain.Policy, error)
}

// ProductStore is the product seam (the product relationship).
type ProductStore interface {
	ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error)
}

// GroupStore is the group seam.
type GroupStore interface {
	GroupByID(ctx context.Context, accountID, id uuid.UUID) (domain.Group, error)
}

// EntitlementStore loads entitlements for attach.
type EntitlementStore interface {
	EntitlementByID(ctx context.Context, accountID, id uuid.UUID) (domain.Entitlement, error)
}

// PoolStore pops pooled keys.
type PoolStore interface {
	PopPolicyKey(ctx context.Context, accountID, policyID uuid.UUID) (domain.Key, error)
}

// TokenManager mints and reads the license's activation tokens.
type TokenManager interface {
	List(ctx context.Context, account domain.Account, principal access.Principal,
		f tokens.Filter, p paging.Params) (tokens.Page, error)
	Show(ctx context.Context, account domain.Account, principal access.Principal, id uuid.UUID) (domain.Token, error)
	CreateForLicense(ctx context.Context, account domain.Account, principal access.Principal,
		licenseID uuid.UUID, attrs tokens.CreateAttrs) (domain.Token, string, error)
}

// Scenario runs the license reads and writes.
type Scenario struct {
	store        Store
	entlist      EntitlementListStore
	policies     PolicyStore
	products     ProductStore
	users        users.Store
	groups       GroupStore
	entitlements EntitlementStore
	pool         PoolStore
	tokens       TokenManager
	events       events.Publisher
	rng          io.Reader
	clock        func() time.Time
	// doTx runs fn inside one database transaction (pgtx); nil runs
	// it bare (tests without a pool).
	doTx func(ctx context.Context, fn func(context.Context) error) error
}

// New builds the scenario over the repositories and seams it reuses.
func New(
	store Store, entlist EntitlementListStore, policyStore PolicyStore, productStore ProductStore,
	userStore users.Store, groupStore GroupStore, entitlementStore EntitlementStore, poolStore PoolStore,
	tokenManager TokenManager, publisher events.Publisher, rng io.Reader, clock func() time.Time,
	doTx func(ctx context.Context, fn func(context.Context) error) error,
) Scenario {
	return Scenario{
		store: store, entlist: entlist, policies: policyStore, products: productStore,
		users: userStore, groups: groupStore, entitlements: entitlementStore, pool: poolStore,
		tokens: tokenManager, events: publisher, rng: rng, clock: clock, doTx: doTx,
	}
}

// write runs fn inside one transaction when the seam is wired (the
// multi-statement writes span their ports atomically).
func (s Scenario) write(ctx context.Context, fn func(context.Context) error) error {
	if s.doTx == nil {
		return fn(ctx)
	}

	return s.doTx(ctx, fn)
}

// List is one index page (with its total and next cursor).
type List struct {
	Items []domain.License
	Count int
	Next  uuid.NullUUID
}

func (s Scenario) publish(ctx context.Context, event string, resource any) {
	_ = s.events.Publish(ctx, event, resource)
}

func (s Scenario) now() time.Time { return s.clock() }
