package machines

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

var errBoom = errors.New("test: boom")

// world is one in-memory fake behind every port of the scenario. Each test
// builds its own, so tests stay parallel.
type world struct {
	account domain.Account
	license domain.License
	// licenseMissing makes LicenseByID answer not found.
	licenseMissing bool

	machines   []domain.Machine
	users      map[uuid.UUID]domain.User
	groups     map[uuid.UUID]domain.Group
	members    map[uuid.UUID]bool
	taken      map[string]bool
	takenScope []Uniqueness
	existing   map[uuid.UUID]bool

	totals      Totals
	totalsCalls []totalsCall
	groupCount  int

	components []domain.MachineComponent
	compTaken  map[string]bool
	compScopes []Uniqueness

	processes     []domain.MachineProcess
	pidTaken      bool
	processExists bool
	procCount     int64
	procCountCall procCountCall

	token       domain.Token
	tokenLocked bool
	lockNowait  bool
	tokenSet    [2]int
	tokenSetOK  bool

	deadMachines  []Ref
	deadProcesses []Ref

	head        usage.Acked
	appended    []usage.Event
	acks        []usage.Acked
	usageLimits []int

	events      []string
	failPublish error

	inserted          []domain.Machine
	insertedComp      []domain.MachineComponent
	updated           []domain.Machine
	updatedProcesses  []domain.MachineProcess
	deletedMachines   []uuid.UUID
	deletedProcesses  []uuid.UUID
	deletedComponents []uuid.UUID
	insertedProcesses []domain.MachineProcess
	refreshed         int
	locked            int
	includeRequests   [][]string
	listedScopes      []Scope
}

type totalsCall struct {
	perUser bool
	owner   Owner
	except  uuid.UUID
}

type procCountCall struct {
	kind  string
	id    uuid.UUID
	owner Owner
}

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

func newWorld() *world {
	account := domain.Account{ID: uuid.New()}
	policy := domain.Policy{ID: uuid.New(), AccountID: account.ID, ProductID: uuid.New()}

	return &world{
		account: account,
		license: domain.License{
			ID: uuid.New(), AccountID: account.ID, ProductID: policy.ProductID, PolicyID: policy.ID, Policy: policy,
		},
		users:     map[uuid.UUID]domain.User{},
		groups:    map[uuid.UUID]domain.Group{},
		members:   map[uuid.UUID]bool{},
		taken:     map[string]bool{},
		existing:  map[uuid.UUID]bool{},
		compTaken: map[string]bool{},
	}
}

func (w *world) scenario() Scenario {
	return New(w, w, w, w, w, w, w, func() time.Time { return testNow },
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
}

func pagingDefault() paging.Params {
	return paging.Params{Mode: paging.ModeLimit, Limit: 10, Order: paging.OrderDesc}
}

func accessPrincipal(role domain.Role, id uuid.UUID) access.Principal {
	bearer := access.BearerUser

	switch role {
	case domain.RoleProduct:
		bearer = access.BearerProduct
	case domain.RoleLicense:
		bearer = access.BearerLicense
	}

	return access.Principal{BearerType: bearer, BearerID: id, Role: role, Permissions: access.RolePermissions(role)}
}

// as stores a principal of the role (with its default permissions).
func as(role domain.Role, id uuid.UUID) context.Context {
	return access.With(context.Background(), accessPrincipal(role, id))
}

func admin() context.Context { return as(domain.RoleAdmin, uuid.New()) }

// codesOf lists the codes of a (multi-)error document in document order.
func codesOf(t *testing.T, err error) []string {
	t.Helper()

	if err == nil {
		return nil
	}

	var doc *apperr.T
	if !errors.As(err, &doc) {
		t.Fatalf("not an application error: %v", err)
	}

	codes := []string{doc.Code}
	for _, sibling := range doc.Siblings() {
		codes = append(codes, sibling.Code)
	}

	return codes
}

func wantCodes(t *testing.T, err error, want ...string) {
	t.Helper()

	if got := codesOf(t, err); !slices.Equal(got, want) {
		t.Fatalf("codes = %v, want %v (err %v)", got, want, err)
	}
}

func wantStatus(t *testing.T, err error, status int) {
	t.Helper()

	var doc *apperr.T
	if !errors.As(err, &doc) || doc.Status != status {
		t.Fatalf("err = %v, want status %d", err, status)
	}
}

func (w *world) seedMachine(m domain.Machine) domain.Machine {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}

	m.AccountID = w.account.ID

	if m.LicenseID == uuid.Nil {
		m.LicenseID = w.license.ID
		m.PolicyID, m.ProductID = w.license.PolicyID, w.license.ProductID
	}

	if m.Fingerprint == "" {
		m.Fingerprint = "fp-" + m.ID.String()
	}

	w.machines = append(w.machines, m)

	return m
}

// Store.

func (w *world) ListMachines(
	_ context.Context, _ uuid.UUID, _ Filter, scope Scope, page paging.Page, _ time.Time,
) ([]domain.Machine, error) {
	w.listedScopes = append(w.listedScopes, scope)

	out := slices.Clone(w.machines)
	if page.Fetch > 0 && int(page.Fetch) < len(out) {
		out = out[:page.Fetch]
	}

	return out, nil
}

func (w *world) CountMachines(context.Context, uuid.UUID, Filter, Scope, time.Time) (int, error) {
	return len(w.machines), nil
}

func (w *world) FindMachine(
	_ context.Context, _ uuid.UUID, _ Scope, idOrFingerprint string, _ time.Time,
) (domain.Machine, error) {
	for _, m := range w.machines {
		if m.ID.String() == idOrFingerprint || m.Fingerprint == idOrFingerprint {
			return m, nil
		}
	}

	return domain.Machine{}, ErrNotFound
}

func (w *world) InsertMachine(_ context.Context, m domain.Machine) error {
	w.inserted = append(w.inserted, m)
	w.machines = append(w.machines, m)

	return nil
}

func (w *world) UpdateMachine(_ context.Context, m domain.Machine) error {
	w.updated = append(w.updated, m)

	for i := range w.machines {
		if w.machines[i].ID == m.ID {
			w.machines[i] = m
		}
	}

	return nil
}

func (w *world) DeleteMachine(_ context.Context, _, id uuid.UUID) error {
	w.deletedMachines = append(w.deletedMachines, id)

	return nil
}

func (w *world) MachineExists(_ context.Context, _, id uuid.UUID) (bool, error) {
	return w.existing[id], nil
}

func (w *world) FingerprintTaken(_ context.Context, _ uuid.UUID, in Uniqueness, fingerprint string) (bool, error) {
	w.takenScope = append(w.takenScope, in)

	return w.taken[in.Kind+":"+fingerprint], nil
}

func (w *world) MachineTotals(
	_ context.Context, _ uuid.UUID, perUser bool, owner Owner, except uuid.UUID,
) (Totals, error) {
	w.totalsCalls = append(w.totalsCalls, totalsCall{perUser, owner, except})

	return w.totals, nil
}

func (w *world) RefreshLicenseCounters(context.Context, uuid.UUID, uuid.UUID, time.Time) error {
	w.refreshed++

	return nil
}

func (w *world) LicenseHasUser(_ context.Context, _, userID uuid.UUID) (bool, error) {
	return w.members[userID], nil
}

func (w *world) GroupMachineCount(context.Context, uuid.UUID) (int, error) { return w.groupCount, nil }

func (w *world) ListComponents(
	_ context.Context, _ uuid.UUID, f SubFilter, _ Scope, _ paging.Page,
) ([]domain.MachineComponent, int, error) {
	var out []domain.MachineComponent

	for _, c := range w.components {
		if !f.MachineID.Valid || f.MachineID.UUID == c.MachineID {
			out = append(out, c)
		}
	}

	return out, len(out), nil
}

func (w *world) FindComponent(_ context.Context, _ uuid.UUID, _ Scope, id uuid.UUID) (domain.MachineComponent, error) {
	for _, c := range w.components {
		if c.ID == id {
			return c, nil
		}
	}

	return domain.MachineComponent{}, ErrNotFound
}

func (w *world) InsertComponent(_ context.Context, c domain.MachineComponent) error {
	w.insertedComp = append(w.insertedComp, c)
	w.components = append(w.components, c)

	return nil
}

func (w *world) UpdateComponent(_ context.Context, c domain.MachineComponent) error {
	for i := range w.components {
		if w.components[i].ID == c.ID {
			w.components[i] = c
		}
	}

	return nil
}

func (w *world) DeleteComponent(_ context.Context, _, id uuid.UUID) error {
	w.deletedComponents = append(w.deletedComponents, id)

	return nil
}

func (w *world) ComponentFingerprintTaken(
	_ context.Context, _ uuid.UUID, in Uniqueness, fingerprint string,
) (bool, error) {
	w.compScopes = append(w.compScopes, in)

	return w.compTaken[in.Kind+":"+fingerprint], nil
}

func (w *world) ListProcesses(
	_ context.Context, _ uuid.UUID, f SubFilter, _ Scope, _ paging.Page, _ time.Time,
) ([]domain.MachineProcess, int, error) {
	var out []domain.MachineProcess

	for _, p := range w.processes {
		if !f.MachineID.Valid || f.MachineID.UUID == p.MachineID {
			out = append(out, p)
		}
	}

	return out, len(out), nil
}

func (w *world) FindProcess(
	_ context.Context, _ uuid.UUID, _ Scope, id uuid.UUID, _ time.Time,
) (domain.MachineProcess, error) {
	for _, p := range w.processes {
		if p.ID == id {
			return p, nil
		}
	}

	return domain.MachineProcess{}, ErrNotFound
}

func (w *world) InsertProcess(_ context.Context, p domain.MachineProcess) error {
	w.insertedProcesses = append(w.insertedProcesses, p)
	w.processes = append(w.processes, p)

	return nil
}

func (w *world) UpdateProcess(_ context.Context, p domain.MachineProcess) error {
	w.updatedProcesses = append(w.updatedProcesses, p)

	for i := range w.processes {
		if w.processes[i].ID == p.ID {
			w.processes[i] = p
		}
	}

	return nil
}

func (w *world) DeleteProcess(_ context.Context, _, id uuid.UUID) error {
	w.deletedProcesses = append(w.deletedProcesses, id)

	return nil
}

func (w *world) ProcessExists(context.Context, uuid.UUID) (bool, error) { return w.processExists, nil }

func (w *world) ProcessPidTaken(context.Context, uuid.UUID, string) (bool, error) {
	return w.pidTaken, nil
}

func (w *world) ProcessCount(_ context.Context, kind string, scopeID uuid.UUID, owner Owner) (int64, error) {
	w.procCountCall = procCountCall{kind, scopeID, owner}

	return w.procCount, nil
}

func (w *world) DeadMachines(context.Context, time.Time, int) ([]Ref, error) {
	return w.deadMachines, nil
}

func (w *world) DeadProcesses(context.Context, time.Time, int) ([]Ref, error) {
	return w.deadProcesses, nil
}

// LicenseStore.

func (w *world) LicenseByID(_ context.Context, _, id uuid.UUID) (domain.License, error) {
	if w.licenseMissing || id != w.license.ID {
		return domain.License{}, ErrNotFound
	}

	return w.license, nil
}

func (w *world) LockLicense(context.Context, uuid.UUID, uuid.UUID) error {
	w.locked++

	return nil
}

// TokenStore.

func (w *world) TokenByID(context.Context, uuid.UUID, uuid.UUID) (domain.Token, error) {
	return w.token, nil
}

func (w *world) LockToken(_ context.Context, _, _ uuid.UUID, nowait bool) (domain.Token, bool, error) {
	w.lockNowait = nowait

	return w.token, w.tokenLocked, nil
}

func (w *world) SetTokenCounters(_ context.Context, _, _ uuid.UUID, activations, deactivations int, _ time.Time) error {
	w.tokenSet, w.tokenSetOK = [2]int{activations, deactivations}, true

	return nil
}

// UsageStore.

func (w *world) UsageHead(context.Context, uuid.UUID, bool) (usage.Acked, error) { return w.head, nil }

func (w *world) AppendUsage(
	_ context.Context, _, _ uuid.UUID, _ usage.Report, events []usage.Event, ack usage.Acked,
) error {
	w.appended = append(w.appended, events...)
	w.acks = append(w.acks, ack)
	w.head = ack

	return nil
}

func (w *world) UsageEvents(
	_ context.Context, _ uuid.UUID, _, after uint64, limit int,
) ([]usage.ResponseEvent, error) {
	w.usageLimits = append(w.usageLimits, limit)

	var out []usage.ResponseEvent

	for _, e := range w.appended {
		if e.Seq > after && len(out) < limit {
			out = append(out, usage.ResponseEvent{Seq: e.Seq, Hash: e.Hash})
		}
	}

	return out, nil
}

func (w *world) UsageAcks(_ context.Context, _ uuid.UUID, _, after uint64, limit int) ([]usage.Acked, error) {
	var out []usage.Acked

	for _, a := range w.acks {
		if a.Seq > after && len(out) < limit {
			out = append(out, a)
		}
	}

	return out, nil
}

// Directory.

func (w *world) UserByID(_ context.Context, _, id uuid.UUID) (domain.User, error) {
	if u, ok := w.users[id]; ok {
		return u, nil
	}

	return domain.User{}, ErrNotFound
}

func (w *world) UserHasActiveLicense(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return true, nil
}

func (w *world) GroupByID(_ context.Context, _, id uuid.UUID) (domain.Group, error) {
	if g, ok := w.groups[id]; ok {
		return g, nil
	}

	return domain.Group{}, ErrNotFound
}

func (w *world) ProductByID(_ context.Context, _, id uuid.UUID) (domain.Product, error) {
	return domain.Product{ID: id}, nil
}

// LicenseIncludes.

func (w *world) Included(_ context.Context, _ domain.Account, _ domain.License, includes []string) (licenses.Checkout, error) {
	w.includeRequests = append(w.includeRequests, includes)

	return licenses.Checkout{}, nil
}

// Publisher.

func (w *world) Publish(_ context.Context, event string, _ any) error {
	if w.failPublish != nil {
		return w.failPublish
	}

	w.events = append(w.events, event)

	return nil
}

func (w *world) seedUser(group uuid.NullUUID, member bool) domain.User {
	u := domain.User{ID: uuid.New(), AccountID: w.account.ID, GroupID: group, CreatedAt: testNow}
	w.users[u.ID] = u
	w.members[u.ID] = member

	return u
}
