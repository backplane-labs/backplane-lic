package usage

import (
	"crypto/sha256"
	"fmt"
	"math"
)

// maxEventSize is the largest single event the canonical wire encoding
// can address with its 4-byte length prefix.
const maxEventSize = math.MaxUint32

// Journal maintains an in-memory usage hash chain for the installation
// side. It appends events and produces reports. Persistence is the
// product's responsibility.
type Journal struct {
	domain      string
	fingerprint string
	epoch       uint64
	nextSeq     uint64
	head        [sha256.Size]byte
	acked       Acked
	recs        []eventRecord
}

type eventRecord struct {
	seq  uint64
	data []byte
	hash [sha256.Size]byte
}

// NewJournal initializes a journal for the given domain, fingerprint
// and epoch. acked is the last state the server acknowledged (zero for
// a fresh journal).
func NewJournal(domain, fingerprint string, epoch uint64, acked Acked) *Journal {
	init := initHash(domain, fingerprint, epoch)

	j := &Journal{
		domain:      domain,
		fingerprint: fingerprint,
		epoch:       epoch,
		nextSeq:     0,
		head:        init,
		acked:       acked,
	}

	// Fast-forward to the acked state.
	if !acked.IsZero() && acked.Epoch == epoch && acked.Seq > 0 {
		ackedHash, err := parseHexHash(acked.Hash)
		if err == nil {
			j.head = ackedHash
			j.nextSeq = acked.Seq
		}
	}

	return j
}

// Append adds an event to the chain and returns its sequence number and
// hash. An event larger than the wire format's length prefix is
// refused without changing the chain.
func (j *Journal) Append(event []byte) (uint64, string, error) {
	if uint64(len(event)) > maxEventSize {
		return 0, "", fmt.Errorf("%w: event of %d bytes exceeds the length prefix", ErrFormat, len(event))
	}

	j.nextSeq++
	j.head = nextHash(j.head, j.nextSeq, event)

	rec := eventRecord{seq: j.nextSeq, data: event, hash: j.head}
	j.recs = append(j.recs, rec)

	return j.nextSeq, hexHash(j.head), nil
}

// Head returns the current chain head (seq, hash).
func (j *Journal) Head() (uint64, string) {
	return j.nextSeq, hexHash(j.head)
}

// Epoch returns the journal's epoch.
func (j *Journal) Epoch() uint64 {
	return j.epoch
}

// Report builds a report covering events from acked+1 through the
// current head.
func (j *Journal) Report(build string) (*Report, error) {
	seqFrom := j.acked.Seq + 1

	// Determine prevHash: init hash if starting from zero, or acked hash.
	prevHash := hexHash(initHash(j.domain, j.fingerprint, j.epoch))

	if !j.acked.IsZero() && j.acked.Epoch == j.epoch {
		prevHash = j.acked.Hash
	}

	// Collect events since seqFrom.
	var rawEvents [][]byte

	for _, rec := range j.recs {
		if rec.seq >= seqFrom {
			rawEvents = append(rawEvents, rec.data)
		}
	}

	// Recompute head from prevHash for correctness.
	ph, _ := parseHexHash(prevHash)
	head, lastSeq := chainHead(ph, seqFrom-1, rawEvents)

	return NewReport(j.domain, j.fingerprint).
		Epoch(j.epoch).
		PrevHash(prevHash).
		Commits(seqFrom, lastSeq, rawEvents).
		Head(hexHash(head)).
		SetBuild(build).
		Build()
}

// AckPending returns a report covering unacknowledged events. pending
// is false, and the report nil, when all events have been acked.
func (j *Journal) AckPending(build string) (*Report, bool, error) {
	if j.nextSeq <= j.acked.Seq {
		return nil, false, nil
	}

	report, err := j.Report(build)
	if err != nil {
		return nil, false, err
	}

	return report, true, nil
}

// Ack records that the server accepted through the given acked state.
func (j *Journal) Ack(acked Acked) {
	j.acked = acked
}
