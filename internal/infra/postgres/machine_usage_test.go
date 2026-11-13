package postgres_test

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

func liveTokens(t *testing.T, store *postgres.Store) {
	t.Helper()

	tn := newTenant(t, store, nil)
	lic := tn.license(nil)
	token := domain.Token{
		ID: uuid.New(), AccountID: tn.account, Digest: uuid.NewString(), BearerID: lic.ID, BearerType: "License",
		BearerRole: domain.RoleLicense, Kind: "activation-token", Permissions: []string{},
		MaxActivations: ptr(5), MaxDeactivations: ptr(2),
	}

	if err := store.Tokens.InsertToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}

	repo := store.Machines
	// The other session: a context without the transaction below.
	outside := t.Context()

	err := store.Do(t.Context(), func(txCtx context.Context) error {
		locked, ok, err := repo.LockToken(txCtx, tn.account, token.ID, true)
		if err != nil || !ok || locked.MaxActivations == nil || *locked.MaxActivations != 5 || *locked.MaxDeactivations != 2 {
			t.Errorf("lock = %+v %v %v", locked, ok, err)
		}

		// Another session finds the row locked: NOWAIT and SKIP LOCKED both answer "not taken".
		for _, nowait := range []bool{true, false} {
			if _, taken, err := repo.LockToken(outside, tn.account, token.ID, nowait); err != nil || taken {
				t.Errorf("nowait=%v while locked = %v %v", nowait, taken, err)
			}
		}

		return repo.SetTokenCounters(txCtx, tn.account, token.ID, 3, 1, time.Now().UTC())
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, nowait := range []bool{true, false} {
		got, ok, err := repo.LockToken(t.Context(), tn.account, token.ID, nowait)
		if err != nil || !ok || got.Activations != 3 || got.Deactivations != 1 {
			t.Errorf("lock after commit (nowait=%v) = %+v %v %v", nowait, got, ok, err)
		}
	}

	// An unknown token is "not taken", not a failure.
	if _, ok, err := repo.LockToken(t.Context(), tn.account, uuid.New(), true); err != nil || ok {
		t.Errorf("unknown token = %v %v", ok, err)
	}

	// Counters never go negative or past int32.
	if err := repo.SetTokenCounters(t.Context(), tn.account, token.ID, math.MaxInt, -4, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	got, err := repo.TokenByID(t.Context(), tn.account, token.ID)
	if err != nil || got.Activations != math.MaxInt32 || got.Deactivations != 0 {
		t.Errorf("clamped counters = %+v %v", got, err)
	}
}

const usageDomain = "live-usage/v1"

// pending builds the journal's next report and its events and ack.
func pending(t *testing.T, j *usage.Journal, counters map[string]int64) (usage.Report, []usage.Event, usage.Acked) {
	t.Helper()

	r, err := j.Report("build-1")
	if err != nil {
		t.Fatal(err)
	}

	r.Counters = counters

	events, err := r.IterEvents()
	if err != nil {
		t.Fatal(err)
	}

	return *r, events, usage.Acked{Epoch: r.Epoch, Seq: r.SeqTo, Hash: r.Head}
}

func appendEvents(t *testing.T, j *usage.Journal, events ...string) {
	t.Helper()

	for _, e := range events {
		if _, _, err := j.Append([]byte(e)); err != nil {
			t.Fatal(err)
		}
	}
}

func liveUsage(t *testing.T, store *postgres.Store) {
	t.Helper()

	tn := newTenant(t, store, nil)
	m := tn.machine(tn.license(nil), nil)
	repo := store.Machines
	ctx := t.Context()

	for _, forUpdate := range []bool{false, true} {
		if head, err := repo.UsageHead(ctx, m.ID, forUpdate); err != nil || !head.IsZero() {
			t.Fatalf("head before any report (forUpdate=%v) = %+v %v", forUpdate, head, err)
		}
	}

	j := usage.NewJournal(usageDomain, m.Fingerprint, 3, usage.Acked{})
	appendEvents(t, j, "e1", "e2")

	report1, events1, ack1 := pending(t, j, map[string]int64{"runs": 2})
	if err := repo.AppendUsage(ctx, tn.account, m.ID, report1, events1, ack1); err != nil {
		t.Fatal(err)
	}

	if head, err := repo.UsageHead(ctx, m.ID, false); err != nil || head != ack1 {
		t.Fatalf("head = %+v %v, want %+v", head, err, ack1)
	}

	j.Ack(ack1)
	appendEvents(t, j, "e3", "e4", "e5")

	report2, events2, ack2 := pending(t, j, nil)
	if err := repo.AppendUsage(ctx, tn.account, m.ID, report2, events2, ack2); err != nil {
		t.Fatal(err)
	}

	if head, err := repo.UsageHead(ctx, m.ID, true); err != nil || head != ack2 {
		t.Fatalf("head after the second report = %+v %v, want %+v", head, err, ack2)
	}

	t.Run("events", func(t *testing.T) {
		for _, test := range []struct {
			name         string
			epoch, after uint64
			limit        int
			want         []uint64
		}{
			{"all", 3, 0, 100, []uint64{1, 2, 3, 4, 5}},
			{"after 2", 3, 2, 100, []uint64{3, 4, 5}},
			{"limited", 3, 0, 2, []uint64{1, 2}},
			{"limited after", 3, 3, 1, []uint64{4}},
			{"past the end", 3, 5, 100, nil},
			{"another epoch", 4, 0, 100, nil},
		} {
			got, err := repo.UsageEvents(ctx, m.ID, test.epoch, test.after, test.limit)
			if err != nil {
				t.Fatal(err)
			}

			seqs := make([]uint64, 0, len(got))
			for _, e := range got {
				seqs = append(seqs, e.Seq)
			}

			if !slices.Equal(seqs, test.want) {
				t.Errorf("%s: seqs = %v, want %v", test.name, seqs, test.want)
			}
		}

		got, err := repo.UsageEvents(ctx, m.ID, 3, 0, 100)
		if err != nil {
			t.Fatal(err)
		}

		all := slices.Concat(events1, events2)
		for i, e := range got {
			if e.Event != base64.StdEncoding.EncodeToString(all[i].Data) || e.Hash != all[i].Hash {
				t.Errorf("event %d = %+v, want %q %s", i, e, all[i].Data, all[i].Hash)
			}
		}
	})

	t.Run("acks", func(t *testing.T) {
		got, err := repo.UsageAcks(ctx, m.ID, 3, 0, 100)
		if err != nil || !slices.Equal(got, []usage.Acked{ack1, ack2}) {
			t.Fatalf("acks = %+v %v", got, err)
		}

		if got, err = repo.UsageAcks(ctx, m.ID, 3, 2, 100); err != nil || !slices.Equal(got, []usage.Acked{ack2}) {
			t.Errorf("acks after 2 = %+v %v", got, err)
		}

		if got, err = repo.UsageAcks(ctx, m.ID, 3, 0, 1); err != nil || !slices.Equal(got, []usage.Acked{ack1}) {
			t.Errorf("limited acks = %+v %v", got, err)
		}

		if got, err = repo.UsageAcks(ctx, m.ID, 9, 0, 100); err != nil || len(got) != 0 {
			t.Errorf("acks of another epoch = %+v %v", got, err)
		}
	})

	t.Run("a replayed sequence is refused and changes nothing", func(t *testing.T) {
		if err := repo.AppendUsage(ctx, tn.account, m.ID, report2, events2, ack2); err == nil {
			t.Fatal("events of an accepted report were stored twice")
		}

		if head, err := repo.UsageHead(ctx, m.ID, false); err != nil || head != ack2 {
			t.Errorf("head = %+v %v", head, err)
		}
	})

	t.Run("a new epoch restarts the chain beside the old one", func(t *testing.T) {
		j4 := usage.NewJournal(usageDomain, m.Fingerprint, 4, usage.Acked{})
		appendEvents(t, j4, "n1")

		report, events, ack := pending(t, j4, nil)
		if err := repo.AppendUsage(ctx, tn.account, m.ID, report, events, ack); err != nil {
			t.Fatal(err)
		}

		if head, err := repo.UsageHead(ctx, m.ID, false); err != nil || head != ack {
			t.Fatalf("head = %+v %v, want %+v", head, err, ack)
		}

		if old, err := repo.UsageEvents(ctx, m.ID, 3, 0, 100); err != nil || len(old) != 5 {
			t.Errorf("old epoch events = %d %v", len(old), err)
		}

		if current, err := repo.UsageEvents(ctx, m.ID, 4, 0, 100); err != nil || len(current) != 1 {
			t.Errorf("new epoch events = %d %v", len(current), err)
		}
	})

	t.Run("the head lock serializes reports", func(t *testing.T) {
		err := store.Do(ctx, func(txCtx context.Context) error {
			if _, err := repo.UsageHead(txCtx, m.ID, true); err != nil {
				return err
			}

			waiting, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
			defer cancel()

			if _, err := repo.UsageHead(waiting, m.ID, true); !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("a second locker was not held back: %v", err)
			}

			// A plain read is not blocked by the lock.
			if _, err := repo.UsageHead(ctx, m.ID, false); err != nil {
				t.Errorf("plain read blocked: %v", err)
			}

			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		if _, err := repo.UsageHead(ctx, m.ID, true); err != nil {
			t.Errorf("lock not released after commit: %v", err)
		}
	})

	t.Run("sequence numbers past bigint are refused", func(t *testing.T) {
		huge := usage.Report{Epoch: math.MaxUint64}
		if err := repo.AppendUsage(ctx, tn.account, m.ID, huge, nil, usage.Acked{}); err == nil {
			t.Error("epoch past bigint was stored")
		}

		if _, err := repo.UsageEvents(ctx, m.ID, math.MaxUint64, 0, 1); err == nil {
			t.Error("epoch past bigint was read")
		}

		if _, err := repo.UsageAcks(ctx, m.ID, 3, math.MaxUint64, 1); err == nil {
			t.Error("sequence past bigint was read")
		}
	})

	t.Run("a deleted machine takes its journal", func(t *testing.T) {
		if err := repo.DeleteMachine(ctx, tn.account, m.ID); err != nil {
			t.Fatal(err)
		}

		if head, err := repo.UsageHead(ctx, m.ID, false); err != nil || !head.IsZero() {
			t.Errorf("head survived = %+v %v", head, err)
		}

		if got, err := repo.UsageEvents(ctx, m.ID, 3, 0, 100); err != nil || len(got) != 0 {
			t.Errorf("events survived = %d %v", len(got), err)
		}
	})
}
