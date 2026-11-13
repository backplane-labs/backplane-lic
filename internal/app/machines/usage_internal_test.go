package machines

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

const (
	usageDomain = "test-usage/v1"
	usageFP     = "installation-fp"
)

type installation struct {
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newInstallation(tb testing.TB) installation {
	tb.Helper()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		tb.Fatal(err)
	}

	return installation{pub: pub, priv: priv}
}

// machineFor seeds the machine of the installation, its key in the metadata.
func (i installation) machineFor(w *world) domain.Machine {
	return w.seedMachine(domain.Machine{
		Fingerprint: usageFP,
		Metadata:    map[string]any{"public_key": base64.StdEncoding.EncodeToString(i.pub)},
	})
}

// report signs the journal's pending report.
func (i installation) report(tb testing.TB, j *usage.Journal) usage.Report {
	tb.Helper()

	r, err := j.Report("test")
	if err != nil {
		tb.Fatal(err)
	}

	r.Sign(i.priv)

	return *r
}

func decodeOf(r usage.Report) func() (usage.Report, error) {
	return func() (usage.Report, error) { return r, nil }
}

func TestReportUsageAcceptsAndContinuesTheChain(t *testing.T) {
	t.Parallel()

	w, inst := newWorld(), newInstallation(t)
	machine := inst.machineFor(w)
	s := w.scenario()
	j := usage.NewJournal(usageDomain, usageFP, 1, usage.Acked{})

	if _, _, err := j.Append([]byte("e1")); err != nil {
		t.Fatal(err)
	}

	first := inst.report(t, j)

	got, ack, err := s.ReportUsage(admin(), w.account, machine.ID.String(), decodeOf(first))
	if err != nil {
		t.Fatal(err)
	}

	if got.ID != machine.ID || ack.Epoch != 1 || ack.Seq != 1 || ack.Hash != first.Head || len(w.appended) != 1 {
		t.Fatalf("ack = %+v appended = %d", ack, len(w.appended))
	}

	// The idempotent re-send of the accepted report answers the same ack and appends nothing.
	_, again, err := s.ReportUsage(admin(), w.account, machine.ID.String(), decodeOf(first))
	if err != nil || again != ack || len(w.appended) != 1 || len(w.acks) != 1 {
		t.Fatalf("re-send = %+v %v appended = %d acks = %d", again, err, len(w.appended), len(w.acks))
	}

	// The second report continues the chain.
	j.Ack(ack)

	for _, e := range []string{"e2", "e3"} {
		if _, _, err := j.Append([]byte(e)); err != nil {
			t.Fatal(err)
		}
	}

	second := inst.report(t, j)

	_, ack2, err := s.ReportUsage(admin(), w.account, machine.ID.String(), decodeOf(second))
	if err != nil || ack2.Seq != 3 || ack2.Hash != second.Head || len(w.appended) != 3 || w.head != ack2 {
		t.Fatalf("second ack = %+v %v appended = %d", ack2, err, len(w.appended))
	}

	for i, e := range w.appended {
		if e.Seq != uint64(i+1) {
			t.Errorf("event %d has seq %d", i, e.Seq)
		}
	}

	// An older report is a fork once the chain has moved on.
	_, _, err = s.ReportUsage(admin(), w.account, machine.ID.String(), decodeOf(first))
	wantCodes(t, err, "USAGE_FORK")
	wantStatus(t, err, 409)
}

func TestReportUsageRefusals(t *testing.T) {
	t.Parallel()

	stranger := newInstallation(t)

	for _, test := range []struct {
		name  string
		build func(t *testing.T, inst installation, w *world) usage.Report
		code  string
		http  int
	}{
		{"signature by another key", func(t *testing.T, _ installation, _ *world) usage.Report {
			t.Helper()

			r := stranger.report(t, journalWith(t, 1, "e1"))

			return r
		}, "USAGE_SIGNATURE_INVALID", 422},
		{"unsigned", func(t *testing.T, inst installation, _ *world) usage.Report {
			t.Helper()

			r := inst.report(t, journalWith(t, 1, "e1"))
			r.Signature = ""

			return r
		}, "USAGE_SIGNATURE_INVALID", 422},
		{"tampered events keep a valid signature but break the chain", func(t *testing.T, inst installation, _ *world) usage.Report {
			t.Helper()

			r := inst.report(t, journalWith(t, 1, "e1"))
			r.Head = hex.EncodeToString(make([]byte, 32))
			r.Sign(inst.priv)

			return r
		}, "USAGE_CHAIN_BROKEN", 422},
		{"unknown format", func(t *testing.T, inst installation, _ *world) usage.Report {
			t.Helper()

			r := inst.report(t, journalWith(t, 1, "e1"))
			r.Format = 2

			return r
		}, "USAGE_FORMAT_INVALID", 422},
		{"first report not starting at 1 is a gap", func(t *testing.T, inst installation, _ *world) usage.Report {
			t.Helper()

			j := usage.NewJournal(usageDomain, usageFP, 1, usage.Acked{})
			if _, _, err := j.Append([]byte("e1")); err != nil {
				t.Fatal(err)
			}

			_, hash := j.Head()
			resumed := usage.NewJournal(usageDomain, usageFP, 1, usage.Acked{Epoch: 1, Seq: 1, Hash: hash})
			if _, _, err := resumed.Append([]byte("e2")); err != nil {
				t.Fatal(err)
			}

			return inst.report(t, resumed)
		}, "USAGE_GAP", 409},
		{"report forking from the accepted head", func(t *testing.T, inst installation, w *world) usage.Report {
			t.Helper()

			w.head = usage.Acked{Epoch: 1, Seq: 1, Hash: hex.EncodeToString(make([]byte, 32))}

			return inst.report(t, journalWith(t, 1, "e1", "e2"))
		}, "USAGE_FORK", 409},
		{"fingerprint of another installation", func(t *testing.T, inst installation, _ *world) usage.Report {
			t.Helper()

			j := usage.NewJournal(usageDomain, "other-fp", 1, usage.Acked{})
			if _, _, err := j.Append([]byte("e1")); err != nil {
				t.Fatal(err)
			}

			return inst.report(t, j)
		}, "USAGE_FINGERPRINT_MISMATCH", 422},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w, inst := newWorld(), newInstallation(t)
			machine := inst.machineFor(w)
			report := test.build(t, inst, w)
			before := len(w.appended)

			_, _, err := w.scenario().ReportUsage(admin(), w.account, machine.ID.String(), decodeOf(report))
			wantCodes(t, err, test.code)
			wantStatus(t, err, test.http)

			if len(w.appended) != before {
				t.Errorf("a refused report appended events")
			}
		})
	}
}

func journalWith(tb testing.TB, epoch uint64, events ...string) *usage.Journal {
	tb.Helper()

	j := usage.NewJournal(usageDomain, usageFP, epoch, usage.Acked{})

	for _, e := range events {
		if _, _, err := j.Append([]byte(e)); err != nil {
			tb.Fatal(err)
		}
	}

	return j
}

func TestReportUsageKeyProblems(t *testing.T) {
	t.Parallel()

	inst := newInstallation(t)
	short := base64.StdEncoding.EncodeToString([]byte("short"))

	for _, test := range []struct {
		name     string
		metadata map[string]any
		code     string
	}{
		{"no metadata", nil, "USAGE_KEY_MISSING"},
		{"no key", map[string]any{"other": "x"}, "USAGE_KEY_MISSING"},
		{"empty key", map[string]any{"public_key": ""}, "USAGE_KEY_MISSING"},
		{"key of the wrong type", map[string]any{"public_key": 5.0}, "USAGE_KEY_MISSING"},
		{"not base64", map[string]any{"public_key": "***"}, "USAGE_KEY_INVALID"},
		{"wrong length", map[string]any{"public_key": short}, "USAGE_KEY_INVALID"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{Fingerprint: usageFP, Metadata: test.metadata})
			report := inst.report(t, journalWith(t, 1, "e1"))

			_, _, err := w.scenario().ReportUsage(admin(), w.account, machine.ID.String(), decodeOf(report))
			wantCodes(t, err, test.code)
			wantStatus(t, err, 422)
		})
	}
}

func TestReportUsageOrder(t *testing.T) {
	t.Parallel()

	w, inst := newWorld(), newInstallation(t)
	machine := inst.machineFor(w)
	s := w.scenario()
	decoded := false

	decode := func() (usage.Report, error) {
		decoded = true

		return usage.Report{}, errBoom
	}

	_, _, err := s.ReportUsage(admin(), w.account, "unknown", decode)
	wantStatus(t, err, 404)

	_, _, err = s.ReportUsage(as(domain.RoleReadOnly, uuid.New()), w.account, machine.ID.String(), decode)
	wantStatus(t, err, 403)

	if decoded {
		t.Fatal("the report was decoded for a forbidden caller")
	}

	if _, _, err = s.ReportUsage(admin(), w.account, machine.ID.String(), decode); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want the decode error", err)
	}

	// Without a key the key error precedes the fingerprint check.
	bare := w.seedMachine(domain.Machine{Fingerprint: "bare"})
	_, _, err = s.ReportUsage(admin(), w.account, bare.ID.String(), decodeOf(inst.report(t, journalWith(t, 1, "e1"))))
	wantCodes(t, err, "USAGE_KEY_MISSING")

	// The machine's own bearers report: its license, its owner; a product too.
	report := inst.report(t, journalWith(t, 1, "e1"))
	if _, _, err = s.ReportUsage(as(domain.RoleLicense, w.license.ID), w.account, machine.ID.String(), decodeOf(report)); err != nil {
		t.Fatalf("license bearer: %v", err)
	}
}

func TestUsageRead(t *testing.T) {
	t.Parallel()

	w, inst := newWorld(), newInstallation(t)
	machine := inst.machineFor(w)
	s := w.scenario()

	empty, err := s.Usage(admin(), w.account, machine.ID.String(), 0, 0)
	if err != nil || empty.Events == nil || empty.Acks == nil || len(empty.Events) != 0 || empty.Head.Seq != 0 || empty.Epoch != 0 {
		t.Fatalf("empty = %+v %v", empty, err)
	}

	j := usage.NewJournal(usageDomain, usageFP, 7, usage.Acked{})

	var last usage.Acked

	for batch := range 3 {
		for range 2 {
			if _, _, err := j.Append([]byte{byte(batch)}); err != nil {
				t.Fatal(err)
			}
		}

		if _, last, err = s.ReportUsage(admin(), w.account, machine.ID.String(), decodeOf(inst.report(t, j))); err != nil {
			t.Fatal(err)
		}

		j.Ack(last)
	}

	got, err := s.Usage(as(domain.RoleProduct, w.license.ProductID), w.account, machine.Fingerprint, 0, 0)
	if err != nil {
		t.Fatal(err)
	}

	if got.Epoch != 7 || len(got.Events) != 6 || len(got.Acks) != 3 || got.Head.Seq != 6 || got.Head.Hash != last.Hash {
		t.Fatalf("usage = %+v", got)
	}

	after, err := s.Usage(admin(), w.account, machine.ID.String(), 4, 100)
	if err != nil || len(after.Events) != 2 || after.Events[0].Seq != 5 || len(after.Acks) != 1 || after.Acks[0].Seq != 6 {
		t.Fatalf("after = %+v %v", after, err)
	}

	for _, test := range []struct{ limit, want int }{{0, 100}, {-3, 100}, {5, 5}, {1000, 1000}, {5000, 1000}} {
		if _, err := s.Usage(admin(), w.account, machine.ID.String(), 0, test.limit); err != nil {
			t.Fatal(err)
		}

		if got := w.usageLimits[len(w.usageLimits)-1]; got != test.want {
			t.Errorf("limit %d read %d, want %d", test.limit, got, test.want)
		}
	}
}

func TestUsageReadAuthorization(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		role   domain.Role
		bearer func(w *world) uuid.UUID
		ok     bool
	}{
		{domain.RoleAdmin, func(*world) uuid.UUID { return uuid.New() }, true},
		{domain.RoleReadOnly, func(*world) uuid.UUID { return uuid.New() }, true},
		{domain.RoleProduct, func(w *world) uuid.UUID { return w.license.ProductID }, true},
		{domain.RoleProduct, func(*world) uuid.UUID { return uuid.New() }, false},
		{domain.RoleLicense, func(w *world) uuid.UUID { return w.license.ID }, false},
		{domain.RoleUser, func(*world) uuid.UUID { return uuid.New() }, false},
	} {
		t.Run(string(test.role), func(t *testing.T) {
			t.Parallel()

			w := newWorld()
			machine := w.seedMachine(domain.Machine{})

			_, err := w.scenario().Usage(as(test.role, test.bearer(w)), w.account, machine.ID.String(), 0, 10)
			if (err == nil) != test.ok {
				t.Fatalf("err = %v, want ok = %v", err, test.ok)
			}
		})
	}
}

func TestInstallationKey(t *testing.T) {
	t.Parallel()

	inst := newInstallation(t)

	key, err := installationKey(domain.Machine{Metadata: map[string]any{
		"public_key": base64.StdEncoding.EncodeToString(inst.pub),
	}})
	if err != nil || !key.Equal(inst.pub) {
		t.Fatalf("key = %x %v", key, err)
	}

	if _, err := installationKey(domain.Machine{}); err == nil || !strings.Contains(err.Error(), "publicKey") {
		t.Fatalf("err = %v", err)
	}
}

func BenchmarkReportUsageVerify(b *testing.B) {
	inst := newInstallation(b)
	j := usage.NewJournal(usageDomain, usageFP, 1, usage.Acked{})

	for i := range 50 {
		if _, _, err := j.Append([]byte{byte(i), 1, 2, 3, 4, 5, 6, 7}); err != nil {
			b.Fatal(err)
		}
	}

	r := inst.report(b, j)

	for b.Loop() {
		if _, err := r.Verify(inst.pub, usage.Acked{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReportUsage(b *testing.B) {
	w, inst := newWorld(), newInstallation(b)
	machine := inst.machineFor(w)
	s := w.scenario()
	ctx := admin()
	r := inst.report(b, journalWith(b, 1, "e1", "e2", "e3"))

	for b.Loop() {
		w.head, w.appended, w.acks = usage.Acked{}, nil, nil

		if _, _, err := s.ReportUsage(ctx, w.account, machine.ID.String(), decodeOf(r)); err != nil {
			b.Fatal(err)
		}
	}
}
