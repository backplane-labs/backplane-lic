package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/infra/postgres/db"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

// UsageHead loads the machine's accepted usage state (zero before the
// first report), locking it for the transaction when asked.
func (r MachinesRepo) UsageHead(ctx context.Context, machineID uuid.UUID, forUpdate bool) (usage.Acked, error) {
	rows, err := r.usageHeadRows(ctx, machineID, forUpdate)
	if err != nil {
		return usage.Acked{}, err
	}

	if len(rows) == 0 {
		return usage.Acked{}, nil
	}

	return usage.Acked{Epoch: unsigned(rows[0].Epoch), Seq: unsigned(rows[0].Seq), Hash: rows[0].Hash}, nil
}

// usageHeadRows reads the head row; the locking query returns the same
// columns.
func (r MachinesRepo) usageHeadRows(
	ctx context.Context, machineID uuid.UUID, forUpdate bool,
) ([]db.UsageHeadRow, error) {
	if !forUpdate {
		rows, err := r.q.UsageHead(ctx, machineID)
		if err != nil {
			return nil, fmt.Errorf("postgres: usage head: %w", err)
		}

		return rows, nil
	}

	locked, err := r.q.UsageHeadForUpdate(ctx, machineID)
	if err != nil {
		return nil, fmt.Errorf("postgres: usage head: %w", err)
	}

	rows := make([]db.UsageHeadRow, len(locked))
	for i := range locked {
		rows[i] = db.UsageHeadRow(locked[i])
	}

	return rows, nil
}

// AppendUsage records an accepted report: its events, its ack and the
// new head.
func (r MachinesRepo) AppendUsage(
	ctx context.Context, accountID, machineID uuid.UUID, report usage.Report, events []usage.Event, ack usage.Acked,
) error {
	now := time.Now()

	epoch, err := signed(report.Epoch)
	if err != nil {
		return err
	}

	for _, event := range events {
		seq, err := signed(event.Seq)
		if err != nil {
			return err
		}

		err = r.q.InsertUsageEvent(ctx, db.InsertUsageEventParams{
			MachineID: machineID, Epoch: epoch, Seq: seq, Event: event.Data, Hash: event.Hash, CreatedAt: now,
		})
		if err != nil {
			return fmt.Errorf("postgres: usage event: %w", err)
		}
	}

	seq, err := signed(ack.Seq)
	if err != nil {
		return err
	}

	counters := report.Counters
	if counters == nil {
		counters = map[string]int64{}
	}

	rawCounters, err := json.Marshal(counters)
	if err != nil {
		return fmt.Errorf("postgres: usage counters: %w", err)
	}

	err = r.q.InsertUsageAck(ctx, db.InsertUsageAckParams{
		MachineID: machineID, Epoch: epoch, Seq: seq, Hash: ack.Hash, Counters: rawCounters,
		Build: refOrNil(report.Build), CreatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("postgres: usage ack: %w", err)
	}

	err = r.q.UpsertUsageHead(ctx, db.UpsertUsageHeadParams{
		MachineID: machineID, AccountID: accountID, Epoch: epoch, Seq: seq, Hash: ack.Hash, UpdatedAt: now,
	})
	if err != nil {
		return fmt.Errorf("postgres: usage head: %w", err)
	}

	return nil
}

// UsageEvents reads the epoch's events after a sequence number.
func (r MachinesRepo) UsageEvents(
	ctx context.Context, machineID uuid.UUID, epoch, after uint64, limit int,
) ([]usage.ResponseEvent, error) {
	e, a, err := signedPair(epoch, after)
	if err != nil {
		return nil, err
	}

	rows, err := r.q.UsageEvents(ctx, db.UsageEventsParams{MachineID: machineID, Epoch: e, After: a, Lim: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("postgres: usage events: %w", err)
	}

	out := make([]usage.ResponseEvent, 0, len(rows))
	for i := range rows {
		out = append(out, usage.ResponseEvent{
			Seq: unsigned(rows[i].Seq), Event: base64.StdEncoding.EncodeToString(rows[i].Event), Hash: rows[i].Hash,
		})
	}

	return out, nil
}

// UsageAcks reads the epoch's acks after a sequence number.
func (r MachinesRepo) UsageAcks(
	ctx context.Context, machineID uuid.UUID, epoch, after uint64, limit int,
) ([]usage.Acked, error) {
	e, a, err := signedPair(epoch, after)
	if err != nil {
		return nil, err
	}

	rows, err := r.q.UsageAcks(ctx, db.UsageAcksParams{MachineID: machineID, Epoch: e, After: a, Lim: int64(limit)})
	if err != nil {
		return nil, fmt.Errorf("postgres: usage acks: %w", err)
	}

	out := make([]usage.Acked, 0, len(rows))
	for i := range rows {
		out = append(out, usage.Acked{Epoch: unsigned(rows[i].Epoch), Seq: unsigned(rows[i].Seq), Hash: rows[i].Hash})
	}

	return out, nil
}

// errUsageRange: a sequence number past the bigint column.
var errUsageRange = errors.New("postgres: usage sequence out of range")

// unsigned reads a stored bigint back into the uint64 it was written
// from; the column never holds a negative.
func unsigned(v int64) uint64 {
	if v < 0 {
		return 0
	}

	return uint64(v)
}

func signed(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, errUsageRange
	}

	return int64(v), nil
}

func signedPair(a, b uint64) (int64, int64, error) {
	x, err := signed(a)
	if err != nil {
		return 0, 0, err
	}

	y, err := signed(b)

	return x, y, err
}
