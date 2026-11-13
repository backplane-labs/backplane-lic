package postgres_test

import (
	"errors"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/app/machines"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
)

const wide = 100000

// openLive connects to LIC_TEST_DATABASE_URL or skips the test.
func openLive(t *testing.T) *postgres.Store {
	t.Helper()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIC_TEST_DATABASE_URL to test the machine repositories")
	}

	store, err := postgres.Open(t.Context(), xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(store.Close)

	return store
}

func ptr[T any](v T) *T { return &v }

// tenant is a product with a policy inside an account; sibling tenants share
// the account.
type tenant struct {
	t       *testing.T
	store   *postgres.Store
	account uuid.UUID
	product uuid.UUID
	policy  domain.Policy
	clock   *time.Time
}

func newTenant(t *testing.T, store *postgres.Store, mut func(*domain.Policy)) tenant {
	t.Helper()

	account := uuid.New()
	if _, err := accounts.Bootstrap(t.Context(), store.Accounts,
		accounts.Spec{ID: account, Slug: account.String(), Name: "machines live test"}); err != nil {
		t.Fatal(err)
	}

	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	return tenant{t: t, store: store, account: account, clock: &start}.sibling(mut)
}

// sibling is another product and policy in the same account.
func (tn tenant) sibling(mut func(*domain.Policy)) tenant {
	tn.t.Helper()

	product := domain.Product{
		ID: uuid.New(), AccountID: tn.account, Name: "product", DistributionStrategy: domain.DistributionLicensed,
		Metadata: map[string]any{},
	}
	if err := tn.store.Products.InsertProduct(tn.t.Context(), product); err != nil {
		tn.t.Fatal(err)
	}

	policy := domain.PolicyCreateDefaults()
	policy.ID, policy.AccountID, policy.ProductID, policy.Name = uuid.New(), tn.account, product.ID, "policy"
	policy.Metadata = map[string]any{}

	if mut != nil {
		mut(&policy)
	}

	if err := tn.store.Policies.InsertPolicy(tn.t.Context(), policy); err != nil {
		tn.t.Fatal(err)
	}

	tn.product, tn.policy = product.ID, policy

	return tn
}

// tick hands out strictly increasing creation times.
func (tn tenant) tick() time.Time {
	*tn.clock = tn.clock.Add(time.Millisecond)

	return *tn.clock
}

func (tn tenant) user() domain.User {
	tn.t.Helper()

	now := time.Now().UTC()
	user := domain.User{
		ID: uuid.New(), AccountID: tn.account, Email: uuid.NewString() + "@example.com", Role: domain.RoleUser,
		Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}

	if err := tn.store.Users.InsertUser(tn.t.Context(), user); err != nil {
		tn.t.Fatal(err)
	}

	return user
}

func (tn tenant) license(mut func(*domain.License)) domain.License {
	tn.t.Helper()

	l := domain.License{
		ID: uuid.New(), AccountID: tn.account, ProductID: tn.product, PolicyID: tn.policy.ID, Key: uuid.NewString(),
		Metadata: map[string]any{},
	}

	if mut != nil {
		mut(&l)
	}

	if err := tn.store.Licenses.InsertLicense(tn.t.Context(), l); err != nil {
		tn.t.Fatal(err)
	}

	return l
}

func (tn tenant) machine(l domain.License, mut func(*domain.Machine)) domain.Machine {
	tn.t.Helper()

	at := tn.tick()
	m := domain.Machine{
		ID: uuid.New(), AccountID: tn.account, LicenseID: l.ID, PolicyID: l.PolicyID, Fingerprint: "fp-" + uuid.NewString(),
		Metadata: map[string]any{}, CreatedAt: at, UpdatedAt: at,
	}

	if mut != nil {
		mut(&m)
	}

	if err := tn.store.Machines.InsertMachine(tn.t.Context(), m); err != nil {
		tn.t.Fatal(err)
	}

	return m
}

func (tn tenant) process(m domain.Machine, lastBeat time.Time, pid string) domain.MachineProcess {
	tn.t.Helper()

	at := tn.tick()
	p := domain.MachineProcess{
		ID: uuid.New(), AccountID: tn.account, MachineID: m.ID, PID: pid, Metadata: map[string]any{},
		LastHeartbeatAt: lastBeat, CreatedAt: at, UpdatedAt: at,
	}

	if err := tn.store.Machines.InsertProcess(tn.t.Context(), p); err != nil {
		tn.t.Fatal(err)
	}

	return p
}

func (tn tenant) component(m domain.Machine, name, fingerprint string) domain.MachineComponent {
	tn.t.Helper()

	at := tn.tick()
	c := domain.MachineComponent{
		ID: uuid.New(), AccountID: tn.account, MachineID: m.ID, Name: name, Fingerprint: fingerprint,
		Metadata: map[string]any{}, CreatedAt: at, UpdatedAt: at,
	}

	if err := tn.store.Machines.InsertComponent(tn.t.Context(), c); err != nil {
		tn.t.Fatal(err)
	}

	return c
}

func machineIDs(ms []domain.Machine) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(ms))
	for _, m := range ms {
		ids = append(ids, m.ID)
	}

	return ids
}

func wantMachines(t *testing.T, got []domain.Machine, want ...domain.Machine) {
	t.Helper()

	wantIDs := make([]uuid.UUID, 0, len(want))
	for _, m := range want {
		wantIDs = append(wantIDs, m.ID)
	}

	if gotIDs := machineIDs(got); !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("machines = %v, want %v", gotIDs, wantIDs)
	}
}

func TestMachinesLive(t *testing.T) {
	t.Parallel()

	store := openLive(t)

	for name, run := range map[string]func(*testing.T, *postgres.Store){
		"scope filtering":        liveScope,
		"index filters":          liveFilters,
		"alias lookup":           liveAlias,
		"status filters":         liveStatus,
		"totals and counters":    liveTotals,
		"uniqueness":             liveUniqueness,
		"relations and counts":   liveRelations,
		"dead machines":          liveDeadMachines,
		"dead processes":         liveDeadProcesses,
		"components":             liveComponents,
		"processes":              liveProcesses,
		"machine update":         liveUpdate,
		"cascade on delete":      liveCascade,
		"activation token locks": liveTokens,
		"usage journal":          liveUsage,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			run(t, store)
		})
	}
}

func liveScope(t *testing.T, store *postgres.Store) {
	t.Helper()

	tnA := newTenant(t, store, nil)
	tnB := tnA.sibling(nil)
	owner, member, stranger := tnA.user(), tnA.user(), tnA.user()

	licA := tnA.license(func(l *domain.License) { l.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true} })
	licB := tnB.license(nil)

	if err := store.Licenses.InsertLicenseUsers(t.Context(), uuid.New(), tnA.account, licB.ID,
		[]uuid.UUID{member.ID}); err != nil {
		t.Fatal(err)
	}

	mA, mB := tnA.machine(licA, nil), tnB.machine(licB, nil)

	foreign := newTenant(t, store, nil)
	foreignMachine := foreign.machine(foreign.license(nil), nil)
	now := time.Now().UTC()
	repo := store.Machines

	for _, test := range []struct {
		name  string
		scope machines.Scope
		want  []domain.Machine
	}{
		{"whole account, newest first", machines.Scope{}, []domain.Machine{mB, mA}},
		{"product A", machines.Scope{Kind: "product", ID: tnA.product}, []domain.Machine{mA}},
		{"product B", machines.Scope{Kind: "product", ID: tnB.product}, []domain.Machine{mB}},
		{"unknown product", machines.Scope{Kind: "product", ID: uuid.New()}, nil},
		{"license owner", machines.Scope{Kind: "user", ID: owner.ID}, []domain.Machine{mA}},
		{"license user", machines.Scope{Kind: "user", ID: member.ID}, []domain.Machine{mB}},
		{"unrelated user", machines.Scope{Kind: "user", ID: stranger.ID}, nil},
		{"license A", machines.Scope{Kind: "license", ID: licA.ID}, []domain.Machine{mA}},
		{"license B", machines.Scope{Kind: "license", ID: licB.ID}, []domain.Machine{mB}},
		{"unknown license", machines.Scope{Kind: "license", ID: uuid.New()}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := repo.ListMachines(t.Context(), tnA.account, machines.Filter{}, test.scope, paging.Page{Fetch: wide}, now)
			if err != nil {
				t.Fatal(err)
			}

			wantMachines(t, got, test.want...)

			count, err := repo.CountMachines(t.Context(), tnA.account, machines.Filter{}, test.scope, now)
			if err != nil || count != len(test.want) {
				t.Errorf("count = %d, %v, want %d", count, err, len(test.want))
			}

			// FindMachine applies the same scope.
			_, err = repo.FindMachine(t.Context(), tnA.account, test.scope, mA.ID.String(), now)
			inScope := slices.Contains(machineIDs(test.want), mA.ID)

			if inScope && err != nil || !inScope && !errors.Is(err, domain.ErrNotFound) {
				t.Errorf("find in scope = %v, err %v", inScope, err)
			}
		})
	}

	// Another account's machines are never visible, whatever the scope.
	if got, err := repo.ListMachines(t.Context(), tnA.account, machines.Filter{IDs: []uuid.UUID{foreignMachine.ID}},
		machines.Scope{}, paging.Page{Fetch: wide}, now); err != nil || len(got) != 0 {
		t.Fatalf("foreign machine listed: %v %v", got, err)
	}

	if _, err := repo.FindMachine(t.Context(), tnA.account, machines.Scope{}, foreignMachine.ID.String(), now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign machine found: %v", err)
	}

	// The joined authorization facts.
	got, err := repo.FindMachine(t.Context(), tnA.account, machines.Scope{}, mA.ID.String(), now)
	if err != nil {
		t.Fatal(err)
	}

	if got.ProductID != tnA.product || got.LicenseOwnerID.UUID != owner.ID || got.PolicyID != tnA.policy.ID {
		t.Errorf("joined facts = %+v", got)
	}
}

func liveFilters(t *testing.T, store *postgres.Store) {
	t.Helper()

	tnA := newTenant(t, store, nil)
	tnB := tnA.sibling(nil)
	owner, member := tnA.user(), tnA.user()
	group := domain.Group{ID: uuid.New(), AccountID: tnA.account, Name: "g"}

	if err := store.Groups.InsertGroup(t.Context(), group); err != nil {
		t.Fatal(err)
	}

	licA := tnA.license(func(l *domain.License) { l.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true} })
	licB := tnB.license(nil)

	if err := store.Licenses.InsertLicenseUsers(t.Context(), uuid.New(), tnA.account, licB.ID,
		[]uuid.UUID{member.ID}); err != nil {
		t.Fatal(err)
	}

	mA := tnA.machine(licA, func(m *domain.Machine) {
		m.IP, m.Hostname = ptr("10.0.0.1"), ptr("host-a")
		m.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true}
		m.GroupID = uuid.NullUUID{UUID: group.ID, Valid: true}
		m.Metadata = map[string]any{"tier": "gold"}
	})
	mB := tnB.machine(licB, func(m *domain.Machine) { m.IP, m.Hostname = ptr("10.0.0.2"), ptr("host-b") })
	now := time.Now().UTC()

	for _, test := range []struct {
		name   string
		filter machines.Filter
		want   []domain.Machine
	}{
		{"ids", machines.Filter{IDs: []uuid.UUID{mB.ID}}, []domain.Machine{mB}},
		{"fingerprint", machines.Filter{Fingerprint: mA.Fingerprint}, []domain.Machine{mA}},
		{"ip", machines.Filter{IP: "10.0.0.2"}, []domain.Machine{mB}},
		{"hostname", machines.Filter{Hostname: "host-a"}, []domain.Machine{mA}},
		{"product", machines.Filter{ProductID: uuid.NullUUID{UUID: tnB.product, Valid: true}}, []domain.Machine{mB}},
		{"policy", machines.Filter{PolicyID: uuid.NullUUID{UUID: tnA.policy.ID, Valid: true}}, []domain.Machine{mA}},
		{"license", machines.Filter{LicenseID: uuid.NullUUID{UUID: licB.ID, Valid: true}}, []domain.Machine{mB}},
		{"license key", machines.Filter{Key: licB.Key}, []domain.Machine{mB}},
		{"group", machines.Filter{GroupID: uuid.NullUUID{UUID: group.ID, Valid: true}}, []domain.Machine{mA}},
		{"owner by id", machines.Filter{Owner: owner.ID.String()}, []domain.Machine{mA}},
		{"owner by email", machines.Filter{Owner: owner.Email}, []domain.Machine{mA}},
		{"unknown owner", machines.Filter{Owner: "nobody@example.com"}, nil},
		{"unowned", machines.Filter{OwnerNull: true}, []domain.Machine{mB}},
		{"license owner as user", machines.Filter{User: owner.Email}, []domain.Machine{mA}},
		{"licensee as user", machines.Filter{User: member.ID.String()}, []domain.Machine{mB}},
		{"metadata", machines.Filter{Metadata: map[string]any{"tier": "gold"}}, []domain.Machine{mA}},
		{"metadata mismatch", machines.Filter{Metadata: map[string]any{"tier": "silver"}}, nil},
		{"unknown status", machines.Filter{Status: "BOGUS"}, nil},
		{"combined", machines.Filter{IP: "10.0.0.1", Hostname: "host-b"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := store.Machines.ListMachines(t.Context(), tnA.account, test.filter, machines.Scope{},
				paging.Page{Fetch: wide}, now)
			if err != nil {
				t.Fatal(err)
			}

			wantMachines(t, got, test.want...)
		})
	}

	// Ordering and windows: asc, cursor, offset.
	page := func(p paging.Page) []domain.Machine {
		t.Helper()

		got, err := store.Machines.ListMachines(t.Context(), tnA.account, machines.Filter{}, machines.Scope{}, p, now)
		if err != nil {
			t.Fatal(err)
		}

		return got
	}

	wantMachines(t, page(paging.Page{Fetch: wide, Asc: true}), mA, mB)
	wantMachines(t, page(paging.Page{Fetch: 1}), mB)
	wantMachines(t, page(paging.Page{Fetch: 1, Offset: 1}), mA)
	wantMachines(t, page(paging.Page{Fetch: wide, Cursor: &mB.ID}), mA)
	wantMachines(t, page(paging.Page{Fetch: wide, Asc: true, Cursor: &mA.ID}), mB)
	wantMachines(t, page(paging.Page{Fetch: wide, Cursor: &mA.ID}))

	if n, err := store.Machines.CountMachines(t.Context(), tnA.account, machines.Filter{}, machines.Scope{}, now); err != nil || n != 2 {
		t.Errorf("count = %d, %v", n, err)
	}

	if n, err := store.Machines.CountMachines(t.Context(), tnA.account, machines.Filter{IP: "none"}, machines.Scope{}, now); err != nil || n != 0 {
		t.Errorf("empty count = %d, %v", n, err)
	}
}

func liveAlias(t *testing.T, store *postgres.Store) {
	t.Helper()

	tnA := newTenant(t, store, nil)
	tnB := tnA.sibling(nil)
	licA, licB := tnA.license(nil), tnB.license(nil)
	now := time.Now().UTC()

	oldest := tnA.machine(licA, func(m *domain.Machine) { m.Fingerprint = "shared" })
	newer := tnB.machine(licB, func(m *domain.Machine) { m.Fingerprint = "shared" })
	lone := tnA.machine(licA, nil)

	find := func(key string) (domain.Machine, error) {
		return store.Machines.FindMachine(t.Context(), tnA.account, machines.Scope{}, key, now)
	}

	if got, err := find("shared"); err != nil || got.ID != oldest.ID {
		t.Fatalf("alias resolved to %v (%v), want the oldest %v", got.ID, err, oldest.ID)
	}

	for _, m := range []domain.Machine{oldest, newer, lone} {
		if got, err := find(m.ID.String()); err != nil || got.ID != m.ID {
			t.Errorf("by id %v = %v, %v", m.ID, got.ID, err)
		}
	}

	if got, err := find(lone.Fingerprint); err != nil || got.ID != lone.ID {
		t.Errorf("by fingerprint = %v, %v", got.ID, err)
	}

	// A scope narrows the alias to the visible machines.
	scoped, err := store.Machines.FindMachine(t.Context(), tnA.account, machines.Scope{Kind: "product", ID: tnB.product},
		"shared", now)
	if err != nil || scoped.ID != newer.ID {
		t.Errorf("scoped alias = %v, %v, want %v", scoped.ID, err, newer.ID)
	}

	for _, key := range []string{"", "unknown", uuid.NewString()} {
		if _, err := find(key); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%q: err = %v, want not found", key, err)
		}
	}
}

func liveStatus(t *testing.T, store *postgres.Store) {
	t.Helper()

	// A required heartbeat with a 60 second window.
	required := newTenant(t, store, func(p *domain.Policy) {
		p.RequireHeartbeat, p.HeartbeatDuration = true, ptr(int32(60))
	})
	optional := required.sibling(func(p *domain.Policy) { p.HeartbeatDuration = nil })
	now := time.Now().UTC().Truncate(time.Microsecond)
	beat := func(ago time.Duration) func(*domain.Machine) {
		return func(m *domain.Machine) { m.LastHeartbeatAt = ptr(now.Add(-ago)) }
	}
	young := func(m *domain.Machine) { m.CreatedAt = now.Add(-10 * time.Second) }
	old := func(m *domain.Machine) { m.CreatedAt = now.Add(-10 * time.Minute) }

	licR, licO := required.license(nil), optional.license(nil)
	alive := required.machine(licR, beat(30*time.Second))
	dead := required.machine(licR, beat(2*time.Minute))
	youngNever := required.machine(licR, young)
	oldNever := required.machine(licR, old)
	optNever := optional.machine(licO, old)
	optAlive := optional.machine(licO, beat(5*time.Minute))
	optDead := optional.machine(licO, beat(11*time.Minute))

	for status, want := range map[string][]domain.Machine{
		"ALIVE": {optAlive, optNever, youngNever, alive},
		"DEAD":  {optDead, oldNever, dead},
	} {
		got, err := store.Machines.ListMachines(t.Context(), required.account, machines.Filter{Status: status},
			machines.Scope{}, paging.Page{Fetch: wide}, now)
		if err != nil {
			t.Fatal(err)
		}

		ids := machineIDs(got)
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })

		wantIDs := machineIDs(want)
		slices.SortFunc(wantIDs, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })

		if !slices.Equal(ids, wantIDs) {
			t.Errorf("%s: %v, want %v", status, ids, wantIDs)
		}

		// The SQL status and the domain's agree on every row.
		for _, m := range got {
			// A machine that never pinged is NOT_STARTED to the domain and
			// ALIVE to the filter while its window is open.
			notStarted := status == "ALIVE" && m.HeartbeatStatus == domain.MachineNotStarted
			if m.HeartbeatStatus != status && !notStarted {
				t.Errorf("%s listed with domain status %s", status, m.HeartbeatStatus)
			}
		}
	}
}

func liveTotals(t *testing.T, store *postgres.Store) {
	t.Helper()

	tn := newTenant(t, store, nil)
	owner1, owner2 := tn.user(), tn.user()
	n1, n2 := uuid.NullUUID{UUID: owner1.ID, Valid: true}, uuid.NullUUID{UUID: owner2.ID, Valid: true}
	lic, other := tn.license(nil), tn.license(nil)

	sized := func(owner uuid.NullUUID, cores int32, memory, disk int64) func(*domain.Machine) {
		return func(m *domain.Machine) {
			m.OwnerID, m.Cores, m.Memory, m.Disk = owner, &cores, &memory, &disk
		}
	}

	m1 := tn.machine(lic, sized(n1, 2, 100, 10))
	m2 := tn.machine(lic, sized(n1, 4, 200, 20))
	m3 := tn.machine(lic, sized(n2, 1, 1, 1))
	m4 := tn.machine(lic, nil)

	tn.machine(other, sized(n1, 99, 99, 99))

	repo := store.Machines

	for _, test := range []struct {
		name    string
		perUser bool
		owner   machines.Owner
		except  uuid.UUID
		want    machines.Totals
	}{
		{"license", false, machines.Owner{}, uuid.Nil, machines.Totals{Count: 4, Cores: 7, Memory: 301, Disk: 31}},
		{"license without one machine", false, machines.Owner{}, m2.ID, machines.Totals{Count: 3, Cores: 3, Memory: 101, Disk: 11}},
		{"owner 1", true, n1, uuid.Nil, machines.Totals{Count: 2, Cores: 6, Memory: 300, Disk: 30}},
		{"owner 1 without itself", true, n1, m1.ID, machines.Totals{Count: 1, Cores: 4, Memory: 200, Disk: 20}},
		{"owner 2 without itself", true, n2, m3.ID, machines.Totals{}},
		{"the unowned", true, machines.Owner{}, uuid.Nil, machines.Totals{Count: 1}},
		{"the unowned without itself", true, machines.Owner{}, m4.ID, machines.Totals{}},
		{"owner ignored when not per user", false, n2, uuid.Nil, machines.Totals{Count: 4, Cores: 7, Memory: 301, Disk: 31}},
	} {
		got, err := repo.MachineTotals(t.Context(), lic.ID, test.perUser, test.owner, test.except)
		if err != nil || got != test.want {
			t.Errorf("%s: %+v %v, want %+v", test.name, got, err, test.want)
		}
	}

	// The counter cache follows the rows.
	if err := repo.RefreshLicenseCounters(t.Context(), tn.account, lic.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	got, err := repo.LicenseByID(t.Context(), tn.account, lic.ID)
	if err != nil {
		t.Fatal(err)
	}

	if got.MachinesCount != 4 || got.MachinesCoreCount != 7 || got.MachinesMemoryCount != 301 || got.MachinesDiskCount != 31 {
		t.Errorf("counters = %d %d %d %d", got.MachinesCount, got.MachinesCoreCount, got.MachinesMemoryCount, got.MachinesDiskCount)
	}

	if err := repo.DeleteMachine(t.Context(), tn.account, m1.ID); err != nil {
		t.Fatal(err)
	}

	if err := repo.RefreshLicenseCounters(t.Context(), tn.account, lic.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if got, err = repo.LicenseByID(t.Context(), tn.account, lic.ID); err != nil || got.MachinesCount != 3 || got.MachinesCoreCount != 5 {
		t.Errorf("counters after delete = %d %d %v", got.MachinesCount, got.MachinesCoreCount, err)
	}
}

func liveUniqueness(t *testing.T, store *postgres.Store) {
	t.Helper()

	tnA := newTenant(t, store, nil)
	tnB := tnA.sibling(nil)
	licA1, licA2, licB := tnA.license(nil), tnA.license(nil), tnB.license(nil)
	foreign := newTenant(t, store, nil)

	tnB.machine(licB, func(m *domain.Machine) { m.Fingerprint = "shared" })
	repo := store.Machines

	for _, test := range []struct {
		name string
		in   machines.Uniqueness
		want bool
	}{
		{"own license", machines.Uniqueness{Kind: "license", ID: licB.ID}, true},
		{"another license of the account", machines.Uniqueness{Kind: "license", ID: licA1.ID}, false},
		{"another license, same product", machines.Uniqueness{Kind: "license", ID: licA2.ID}, false},
		{"own policy", machines.Uniqueness{Kind: "policy", ID: tnB.policy.ID}, true},
		{"other policy", machines.Uniqueness{Kind: "policy", ID: tnA.policy.ID}, false},
		{"own product", machines.Uniqueness{Kind: "product", ID: tnB.product}, true},
		{"other product", machines.Uniqueness{Kind: "product", ID: tnA.product}, false},
		{"account", machines.Uniqueness{Kind: "account", ID: tnA.account}, true},
	} {
		got, err := repo.FingerprintTaken(t.Context(), tnA.account, test.in, "shared")
		if err != nil || got != test.want {
			t.Errorf("%s: %v %v, want %v", test.name, got, err, test.want)
		}

		if taken, err := repo.FingerprintTaken(t.Context(), tnA.account, test.in, "other"); err != nil || taken {
			t.Errorf("%s: unrelated fingerprint taken: %v %v", test.name, taken, err)
		}
	}

	if taken, err := repo.FingerprintTaken(t.Context(), foreign.account, machines.Uniqueness{Kind: "account"}, "shared"); err != nil || taken {
		t.Errorf("another account sees the fingerprint: %v %v", taken, err)
	}

	// The partial unique index: a license holds a fingerprint once.
	now := time.Now().UTC()

	dup := domain.Machine{
		ID: uuid.New(), AccountID: tnA.account, LicenseID: licB.ID, PolicyID: licB.PolicyID, Fingerprint: "shared",
		Metadata: map[string]any{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.InsertMachine(t.Context(), dup); err == nil {
		t.Error("a duplicate fingerprint in one license was stored")
	}
}

func liveRelations(t *testing.T, store *postgres.Store) {
	t.Helper()

	tn := newTenant(t, store, nil)
	owner, member, stranger := tn.user(), tn.user(), tn.user()
	n := func(u domain.User) uuid.NullUUID { return uuid.NullUUID{UUID: u.ID, Valid: true} }
	group := domain.Group{ID: uuid.New(), AccountID: tn.account, Name: "g"}

	if err := store.Groups.InsertGroup(t.Context(), group); err != nil {
		t.Fatal(err)
	}

	lic := tn.license(func(l *domain.License) { l.OwnerID = n(owner) })
	if err := store.Licenses.InsertLicenseUsers(t.Context(), uuid.New(), tn.account, lic.ID, []uuid.UUID{member.ID}); err != nil {
		t.Fatal(err)
	}

	repo := store.Machines
	m1 := tn.machine(lic, func(m *domain.Machine) {
		m.GroupID, m.OwnerID = uuid.NullUUID{UUID: group.ID, Valid: true}, n(owner)
	})
	m2 := tn.machine(lic, func(m *domain.Machine) { m.GroupID = uuid.NullUUID{UUID: group.ID, Valid: true} })

	for _, test := range []struct {
		user domain.User
		want bool
	}{{owner, true}, {member, true}, {stranger, false}} {
		if got, err := repo.LicenseHasUser(t.Context(), lic.ID, test.user.ID); err != nil || got != test.want {
			t.Errorf("license user %v = %v %v", test.user.ID, got, err)
		}
	}

	if count, err := repo.GroupMachineCount(t.Context(), group.ID); err != nil || count != 2 {
		t.Errorf("group count = %d %v", count, err)
	}

	if count, err := repo.GroupMachineCount(t.Context(), uuid.New()); err != nil || count != 0 {
		t.Errorf("empty group count = %d %v", count, err)
	}

	for _, test := range []struct {
		id      uuid.UUID
		account uuid.UUID
		want    bool
	}{{m1.ID, tn.account, true}, {m2.ID, tn.account, true}, {uuid.New(), tn.account, false}, {m1.ID, uuid.New(), false}} {
		if got, err := repo.MachineExists(t.Context(), test.account, test.id); err != nil || got != test.want {
			t.Errorf("exists %v = %v %v", test.id, got, err)
		}
	}

	// Process counts for the limits: machine, license, owner and the unowned.
	tn.process(m1, time.Now().UTC(), "1")
	tn.process(m1, time.Now().UTC().Add(-48*time.Hour), "2")
	tn.process(m2, time.Now().UTC(), "3")

	for _, test := range []struct {
		name  string
		kind  string
		scope uuid.UUID
		owner machines.Owner
		want  int64
	}{
		{"machine counts dead ones too", "machine", m1.ID, machines.Owner{}, 2},
		{"other machine", "machine", m2.ID, machines.Owner{}, 1},
		{"license", "license", lic.ID, machines.Owner{}, 3},
		{"owner", "user", lic.ID, n(owner), 2},
		{"unowned", "user", lic.ID, machines.Owner{}, 1},
		{"unknown owner", "user", lic.ID, n(stranger), 0},
	} {
		if got, err := repo.ProcessCount(t.Context(), test.kind, test.scope, test.owner); err != nil || got != test.want {
			t.Errorf("%s: %d %v, want %d", test.name, got, err, test.want)
		}
	}
}

func deadRefs(t *testing.T, refs []machines.Ref, err error, account uuid.UUID) []uuid.UUID {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}

	var ids []uuid.UUID

	for _, ref := range refs {
		if ref.AccountID == account {
			ids = append(ids, ref.ID)
		}
	}

	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })

	return ids
}

func sortedIDs(ids ...uuid.UUID) []uuid.UUID {
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })

	return ids
}

func liveDeadMachines(t *testing.T, store *postgres.Store) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)
	base := newTenant(t, store, func(p *domain.Policy) {
		p.RequireHeartbeat, p.HeartbeatDuration, p.HeartbeatCullStrategy = true, ptr(int32(60)), "DEACTIVATE_DEAD"
	})
	minute := ptr(int32(60))
	keep := base.sibling(func(p *domain.Policy) { p.HeartbeatCullStrategy, p.HeartbeatDuration = "KEEP_DEAD", minute })
	unset := base.sibling(func(p *domain.Policy) { p.HeartbeatCullStrategy, p.HeartbeatDuration = "", minute })
	optional := base.sibling(func(p *domain.Policy) { p.HeartbeatDuration = minute })

	stale := func(m *domain.Machine) { m.LastHeartbeatAt = ptr(now.Add(-5 * time.Minute)) }
	fresh := func(m *domain.Machine) { m.LastHeartbeatAt = ptr(now.Add(-10 * time.Second)) }
	oldNever := func(m *domain.Machine) { m.CreatedAt = now.Add(-time.Hour) }
	youngNever := func(m *domain.Machine) { m.CreatedAt = now.Add(-5 * time.Second) }

	deadDeactivate := base.machine(base.license(nil), stale)
	deadNever := base.machine(base.license(nil), oldNever)
	deadKeep := keep.machine(keep.license(nil), stale)

	base.machine(base.license(nil), fresh)
	base.machine(base.license(nil), youngNever)
	unset.machine(unset.license(nil), stale)
	optional.machine(optional.license(nil), oldNever)
	// A pinged machine is dead even when the policy does not require it.
	deadOptional := optional.machine(optional.license(nil), stale)

	refs, err := store.Machines.DeadMachines(t.Context(), now, wide)
	got := deadRefs(t, refs, err, base.account)

	want := sortedIDs(deadDeactivate.ID, deadNever.ID, deadKeep.ID, deadOptional.ID)
	if !slices.Equal(got, want) {
		t.Fatalf("dead machines = %v, want %v (a NULL cull strategy is excluded)", got, want)
	}

	// The batch is bounded.
	if limited, err := store.Machines.DeadMachines(t.Context(), now, 1); err != nil || len(limited) != 1 {
		t.Errorf("limited batch = %d %v", len(limited), err)
	}

	// Nothing is dead an hour earlier than the oldest heartbeat.
	refs, err = store.Machines.DeadMachines(t.Context(), now.Add(-2*time.Hour), wide)
	if past := deadRefs(t, refs, err, base.account); len(past) != 0 {
		t.Errorf("dead in the past: %v", past)
	}
}

func liveDeadProcesses(t *testing.T, store *postgres.Store) {
	t.Helper()

	now := time.Now().UTC().Truncate(time.Microsecond)
	base := newTenant(t, store, func(p *domain.Policy) {
		p.HeartbeatDuration, p.HeartbeatCullStrategy = ptr(int32(60)), "DEACTIVATE_DEAD"
	})
	minute := ptr(int32(60))
	keep := base.sibling(func(p *domain.Policy) { p.HeartbeatCullStrategy, p.HeartbeatDuration = "KEEP_DEAD", minute })
	unset := base.sibling(func(p *domain.Policy) { p.HeartbeatCullStrategy, p.HeartbeatDuration = "", minute })
	defaultWindow := base.sibling(func(p *domain.Policy) { p.HeartbeatDuration = nil })

	machineOn := func(tn tenant) domain.Machine { return tn.machine(tn.license(nil), nil) }
	long := now.Add(-5 * time.Minute)
	short := now.Add(-10 * time.Second)

	mBase, mKeep, mUnset, mDefault := machineOn(base), machineOn(keep), machineOn(unset), machineOn(defaultWindow)

	deadBase := base.process(mBase, long, "1")
	base.process(mBase, short, "2")
	deadKeep := keep.process(mKeep, long, "1")
	unset.process(mUnset, long, "1")
	// The default window is ten minutes.
	defaultWindow.process(mDefault, now.Add(-5*time.Minute), "alive")
	deadDefault := defaultWindow.process(mDefault, now.Add(-11*time.Minute), "dead")

	refs, err := store.Machines.DeadProcesses(t.Context(), now, wide)
	got := deadRefs(t, refs, err, base.account)

	want := sortedIDs(deadBase.ID, deadKeep.ID, deadDefault.ID)
	if !slices.Equal(got, want) {
		t.Fatalf("dead processes = %v, want %v", got, want)
	}

	if limited, err := store.Machines.DeadProcesses(t.Context(), now, 1); err != nil || len(limited) != 1 {
		t.Errorf("limited batch = %d %v", len(limited), err)
	}
}

func liveComponents(t *testing.T, store *postgres.Store) {
	t.Helper()

	tnA := newTenant(t, store, nil)
	tnB := tnA.sibling(nil)
	owner := tnA.user()
	licA := tnA.license(func(l *domain.License) { l.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true} })
	licB := tnB.license(nil)
	mA := tnA.machine(licA, func(m *domain.Machine) { m.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true} })
	mB := tnB.machine(licB, nil)

	cA1, cA2 := tnA.component(mA, "cpu", "c-cpu"), tnA.component(mA, "gpu", "c-gpu")
	cB := tnB.component(mB, "cpu", "c-cpu")
	repo := store.Machines

	ids := func(cs []domain.MachineComponent) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(cs))
		for _, c := range cs {
			out = append(out, c.ID)
		}

		return out
	}

	list := func(f machines.SubFilter, scope machines.Scope) ([]uuid.UUID, int) {
		t.Helper()

		got, total, err := repo.ListComponents(t.Context(), tnA.account, f, scope, paging.Page{Fetch: wide})
		if err != nil {
			t.Fatal(err)
		}

		return ids(got), total
	}

	if got, total := list(machines.SubFilter{}, machines.Scope{}); !slices.Equal(got, []uuid.UUID{cB.ID, cA2.ID, cA1.ID}) || total != 3 {
		t.Errorf("all = %v (%d)", got, total)
	}

	for _, test := range []struct {
		name   string
		filter machines.SubFilter
		scope  machines.Scope
		want   []uuid.UUID
	}{
		{"machine", machines.SubFilter{MachineID: uuid.NullUUID{UUID: mA.ID, Valid: true}}, machines.Scope{}, []uuid.UUID{cA2.ID, cA1.ID}},
		{"product", machines.SubFilter{ProductID: uuid.NullUUID{UUID: tnB.product, Valid: true}}, machines.Scope{}, []uuid.UUID{cB.ID}},
		{"license", machines.SubFilter{LicenseID: uuid.NullUUID{UUID: licA.ID, Valid: true}}, machines.Scope{}, []uuid.UUID{cA2.ID, cA1.ID}},
		{"machine owner", machines.SubFilter{OwnerID: uuid.NullUUID{UUID: owner.ID, Valid: true}}, machines.Scope{}, []uuid.UUID{cA2.ID, cA1.ID}},
		{"license user by email", machines.SubFilter{User: owner.Email}, machines.Scope{}, []uuid.UUID{cA2.ID, cA1.ID}},
		{"scope product", machines.SubFilter{}, machines.Scope{Kind: "product", ID: tnA.product}, []uuid.UUID{cA2.ID, cA1.ID}},
		{"scope license", machines.SubFilter{}, machines.Scope{Kind: "license", ID: licB.ID}, []uuid.UUID{cB.ID}},
		{"scope user", machines.SubFilter{}, machines.Scope{Kind: "user", ID: owner.ID}, []uuid.UUID{cA2.ID, cA1.ID}},
		{"scope stranger", machines.SubFilter{}, machines.Scope{Kind: "user", ID: uuid.New()}, nil},
	} {
		got, total := list(test.filter, test.scope)
		if !slices.Equal(got, test.want) || total != len(test.want) {
			t.Errorf("%s: %v (%d), want %v", test.name, got, total, test.want)
		}
	}

	// Find within and outside the scope; update; delete.
	if got, err := repo.FindComponent(t.Context(), tnA.account, machines.Scope{Kind: "product", ID: tnA.product}, cA1.ID); err != nil || got.MachineID != mA.ID || got.ProductID != tnA.product || got.LicenseID != licA.ID {
		t.Fatalf("find = %+v %v", got, err)
	}

	if _, err := repo.FindComponent(t.Context(), tnA.account, machines.Scope{Kind: "product", ID: tnB.product}, cA1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("out of scope: %v", err)
	}

	cA1.Name, cA1.Metadata, cA1.UpdatedAt = "renamed", map[string]any{"k": "v"}, time.Now().UTC().Truncate(time.Microsecond)
	if err := repo.UpdateComponent(t.Context(), cA1); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindComponent(t.Context(), tnA.account, machines.Scope{}, cA1.ID)
	if err != nil || got.Name != "renamed" || got.Metadata["k"] != "v" || got.Fingerprint != "c-cpu" {
		t.Fatalf("after update = %+v %v", got, err)
	}

	// Uniqueness spaces.
	for _, test := range []struct {
		name string
		in   machines.Uniqueness
		want bool
	}{
		{"machine A", machines.Uniqueness{Kind: "machine", ID: mA.ID}, true},
		{"machine B does not hold it", machines.Uniqueness{Kind: "machine", ID: mB.ID}, false},
		{"unrelated machine", machines.Uniqueness{Kind: "machine", ID: uuid.New()}, false},
		{"license A", machines.Uniqueness{Kind: "license", ID: licA.ID}, true},
		{"policy A", machines.Uniqueness{Kind: "policy", ID: tnA.policy.ID}, true},
		{"product A", machines.Uniqueness{Kind: "product", ID: tnA.product}, true},
		{"account", machines.Uniqueness{Kind: "account", ID: tnA.account}, true},
		{"unrelated license", machines.Uniqueness{Kind: "license", ID: uuid.New()}, false},
	} {
		if taken, err := repo.ComponentFingerprintTaken(t.Context(), tnA.account, test.in, "c-gpu"); err != nil || taken != test.want {
			t.Errorf("%s: %v %v, want %v", test.name, taken, err, test.want)
		}
	}

	if err := repo.DeleteComponent(t.Context(), tnA.account, cA2.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.FindComponent(t.Context(), tnA.account, machines.Scope{}, cA2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted component found: %v", err)
	}

	// A component fingerprint is unique within its machine.
	dup := domain.MachineComponent{
		ID: uuid.New(), AccountID: tnA.account, MachineID: mA.ID, Name: "dup", Fingerprint: "c-cpu",
		Metadata: map[string]any{}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.InsertComponent(t.Context(), dup); err == nil {
		t.Error("a duplicate component fingerprint in one machine was stored")
	}
}

func liveProcesses(t *testing.T, store *postgres.Store) {
	t.Helper()

	tnA := newTenant(t, store, func(p *domain.Policy) { p.HeartbeatDuration = ptr(int32(60)) })
	tnB := tnA.sibling(nil)
	owner := tnA.user()
	licA := tnA.license(func(l *domain.License) { l.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true} })
	licB := tnB.license(nil)
	mA := tnA.machine(licA, func(m *domain.Machine) { m.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true} })
	mB := tnB.machine(licB, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)

	alive := tnA.process(mA, now.Add(-10*time.Second), "alive")
	dead := tnA.process(mA, now.Add(-5*time.Minute), "dead")
	foreign := tnB.process(mB, now, "other")
	repo := store.Machines

	list := func(f machines.SubFilter, scope machines.Scope) ([]domain.MachineProcess, int) {
		t.Helper()

		got, total, err := repo.ListProcesses(t.Context(), tnA.account, f, scope, paging.Page{Fetch: wide}, now)
		if err != nil {
			t.Fatal(err)
		}

		return got, total
	}

	ids := func(ps []domain.MachineProcess) []uuid.UUID {
		out := make([]uuid.UUID, 0, len(ps))
		for _, p := range ps {
			out = append(out, p.ID)
		}

		return out
	}

	for _, test := range []struct {
		name   string
		filter machines.SubFilter
		scope  machines.Scope
		want   []uuid.UUID
	}{
		{"all", machines.SubFilter{}, machines.Scope{}, []uuid.UUID{foreign.ID, dead.ID, alive.ID}},
		{"machine", machines.SubFilter{MachineID: uuid.NullUUID{UUID: mA.ID, Valid: true}}, machines.Scope{}, []uuid.UUID{dead.ID, alive.ID}},
		{"alive", machines.SubFilter{Status: "ALIVE"}, machines.Scope{}, []uuid.UUID{foreign.ID, alive.ID}},
		{"dead", machines.SubFilter{Status: "DEAD"}, machines.Scope{}, []uuid.UUID{dead.ID}},
		{"unknown status", machines.SubFilter{Status: "x"}, machines.Scope{}, nil},
		{"product", machines.SubFilter{ProductID: uuid.NullUUID{UUID: tnB.product, Valid: true}}, machines.Scope{}, []uuid.UUID{foreign.ID}},
		{"license", machines.SubFilter{LicenseID: uuid.NullUUID{UUID: licA.ID, Valid: true}}, machines.Scope{}, []uuid.UUID{dead.ID, alive.ID}},
		{"machine owner", machines.SubFilter{OwnerID: uuid.NullUUID{UUID: owner.ID, Valid: true}}, machines.Scope{}, []uuid.UUID{dead.ID, alive.ID}},
		{"user", machines.SubFilter{User: owner.Email}, machines.Scope{}, []uuid.UUID{dead.ID, alive.ID}},
		{"scope license", machines.SubFilter{}, machines.Scope{Kind: "license", ID: licB.ID}, []uuid.UUID{foreign.ID}},
		{"scope user", machines.SubFilter{}, machines.Scope{Kind: "user", ID: owner.ID}, []uuid.UUID{dead.ID, alive.ID}},
		{"scope product and status", machines.SubFilter{Status: "DEAD"}, machines.Scope{Kind: "product", ID: tnA.product}, []uuid.UUID{dead.ID}},
	} {
		got, total := list(test.filter, test.scope)
		if !slices.Equal(ids(got), test.want) || total != len(test.want) {
			t.Errorf("%s: %v (%d), want %v", test.name, ids(got), total, test.want)
		}
	}

	// The domain status computed from the policy's window.
	statuses := map[uuid.UUID]string{}

	all, _ := list(machines.SubFilter{}, machines.Scope{})
	for _, p := range all {
		statuses[p.ID] = p.HeartbeatStatus
	}

	if statuses[alive.ID] != domain.MachineAlive || statuses[dead.ID] != domain.MachineDead {
		t.Errorf("statuses = %v", statuses)
	}

	found, err := repo.FindProcess(t.Context(), tnA.account, machines.Scope{Kind: "product", ID: tnA.product}, alive.ID, now)
	if err != nil || found.PID != "alive" || found.ProductID != tnA.product || found.LicenseID != licA.ID {
		t.Fatalf("find = %+v %v", found, err)
	}

	if _, err := repo.FindProcess(t.Context(), tnA.account, machines.Scope{Kind: "product", ID: tnB.product}, alive.ID, now); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("out of scope: %v", err)
	}

	// Update, pid and id checks, delete.
	dead.LastHeartbeatAt, dead.LastDeathEventSentAt, dead.Metadata = now, ptr(now), map[string]any{"k": "v"}
	dead.UpdatedAt = now

	if err := repo.UpdateProcess(t.Context(), dead); err != nil {
		t.Fatal(err)
	}

	got, err := repo.FindProcess(t.Context(), tnA.account, machines.Scope{}, dead.ID, now)
	if err != nil || got.HeartbeatStatus != domain.MachineAlive || got.LastDeathEventSentAt == nil || got.Metadata["k"] != "v" {
		t.Fatalf("after update = %+v %v", got, err)
	}

	for _, test := range []struct {
		name string
		got  func() (bool, error)
		want bool
	}{
		{"pid taken", func() (bool, error) { return repo.ProcessPidTaken(t.Context(), mA.ID, "alive") }, true},
		{"pid free", func() (bool, error) { return repo.ProcessPidTaken(t.Context(), mA.ID, "other") }, false},
		{"pid of another machine", func() (bool, error) { return repo.ProcessPidTaken(t.Context(), mB.ID, "alive") }, false},
		{"process exists", func() (bool, error) { return repo.ProcessExists(t.Context(), alive.ID) }, true},
		{"process unknown", func() (bool, error) { return repo.ProcessExists(t.Context(), uuid.New()) }, false},
	} {
		if got, err := test.got(); err != nil || got != test.want {
			t.Errorf("%s: %v %v", test.name, got, err)
		}
	}

	if err := repo.DeleteProcess(t.Context(), tnA.account, alive.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.FindProcess(t.Context(), tnA.account, machines.Scope{}, alive.ID, now); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted process found: %v", err)
	}

	if err := repo.InsertProcess(t.Context(), domain.MachineProcess{
		ID: uuid.New(), AccountID: tnA.account, MachineID: mA.ID, PID: "dead", Metadata: map[string]any{},
		LastHeartbeatAt: now, CreatedAt: now, UpdatedAt: now,
	}); err == nil {
		t.Error("a duplicate pid in one machine was stored")
	}
}

func liveUpdate(t *testing.T, store *postgres.Store) {
	t.Helper()

	tn := newTenant(t, store, nil)
	lic := tn.license(nil)
	owner := tn.user()
	m := tn.machine(lic, nil)
	now := time.Now().UTC().Truncate(time.Microsecond)

	m.Name, m.IP, m.Hostname, m.Platform = ptr("n"), ptr("1.1.1.1"), ptr("h"), ptr("linux")
	m.Cores, m.Memory, m.Disk, m.MaxProcessesOverride = ptr(int32(8)), ptr(int64(16)), ptr(int64(32)), ptr(int32(3))
	m.OwnerID = uuid.NullUUID{UUID: owner.ID, Valid: true}
	m.Metadata = map[string]any{"a": "b"}
	m.LastHeartbeatAt, m.LastCheckOutAt, m.LastDeathEventSentAt, m.UpdatedAt = &now, &now, &now, now

	if err := store.Machines.UpdateMachine(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	got, err := store.Machines.FindMachine(t.Context(), tn.account, machines.Scope{}, m.ID.String(), now)
	if err != nil {
		t.Fatal(err)
	}

	if *got.Name != "n" || *got.IP != "1.1.1.1" || *got.Cores != 8 || *got.Memory != 16 || *got.Disk != 32 ||
		*got.MaxProcessesOverride != 3 || got.OwnerID != m.OwnerID || got.Metadata["a"] != "b" ||
		got.LastHeartbeatAt == nil || got.LastCheckOutAt == nil || got.LastDeathEventSentAt == nil {
		t.Fatalf("after update = %+v", got)
	}

	// Fingerprint, license and creation time never change.
	if got.Fingerprint != m.Fingerprint || got.LicenseID != lic.ID || !got.CreatedAt.Equal(m.CreatedAt) {
		t.Errorf("immutable columns changed: %+v", got)
	}

	// Clearing a column writes NULL.
	m.Name, m.OwnerID, m.LastHeartbeatAt = nil, uuid.NullUUID{}, nil
	if err := store.Machines.UpdateMachine(t.Context(), m); err != nil {
		t.Fatal(err)
	}

	if got, err = store.Machines.FindMachine(t.Context(), tn.account, machines.Scope{}, m.ID.String(), now); err != nil ||
		got.Name != nil || got.OwnerID.Valid || got.LastHeartbeatAt != nil {
		t.Errorf("after clearing = %+v %v", got, err)
	}
}

func liveCascade(t *testing.T, store *postgres.Store) {
	t.Helper()

	tn := newTenant(t, store, nil)
	m := tn.machine(tn.license(nil), nil)
	c := tn.component(m, "cpu", "c")
	p := tn.process(m, time.Now().UTC(), "1")
	now := time.Now().UTC()

	if err := store.Machines.DeleteMachine(t.Context(), tn.account, m.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Machines.FindComponent(t.Context(), tn.account, machines.Scope{}, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("component survived its machine: %v", err)
	}

	if _, err := store.Machines.FindProcess(t.Context(), tn.account, machines.Scope{}, p.ID, now); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("process survived its machine: %v", err)
	}
}
