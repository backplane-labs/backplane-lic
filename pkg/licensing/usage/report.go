//nolint:tagliatelle // the usage wire format mirrors the licensing API's lowerCamelCase, not snake_case
package usage

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
)

// FormatV1 is the current report format version.
const FormatV1 = 1

// sigPrefix is prepended to the signing payload.
const sigPrefix = "backplane-usage-sig/v1"

// payloadWords is the number of 8-byte words in the signing payload:
// epoch, seqFrom and seqTo.
const payloadWords = 3

// Report is a signed usage report in JSON wire format.
type Report struct {
	// Format is the report format version (currently 1).
	Format int `json:"format"`

	// Domain identifies the product and report type, e.g. "komeet-usage/v1".
	Domain string `json:"domain"`

	// Fingerprint is the installation's hex fingerprint.
	Fingerprint string `json:"fingerprint"`

	// Epoch is the chain epoch (uint64).
	Epoch uint64 `json:"epoch"`

	// SeqFrom is the first sequence number in this report.
	SeqFrom uint64 `json:"seqFrom"`

	// SeqTo is the last sequence number in this report.
	SeqTo uint64 `json:"seqTo"`

	// PrevHash is the hex SHA-256 hash before seqFrom, or the init hash.
	PrevHash string `json:"prevHash"`

	// Events is the base64 (standard) of the canonical event encoding.
	Events string `json:"events"`

	// Head is the hex SHA-256 hash after seqTo.
	Head string `json:"head"`

	// Counters are informative per-kind counts set by the product.
	Counters map[string]int64 `json:"counters,omitempty"`

	// Build is the installation's build version.
	Build string `json:"build,omitempty"`

	// Signature is the Ed25519 signature (standard base64).
	Signature string `json:"signature"`
}

// ReportBuilder constructs a Report step by step.
type ReportBuilder struct {
	domain      string
	fingerprint string
	epoch       uint64
	seqFrom     uint64
	seqTo       uint64
	prevHash    string
	head        string
	eventsRaw   []byte
	counters    map[string]int64
	build       string
	err         error
}

// NewReport starts a report builder for the given domain and fingerprint.
func NewReport(domain, fingerprint string) *ReportBuilder {
	return &ReportBuilder{
		domain:      domain,
		fingerprint: fingerprint,
		counters:    map[string]int64{},
	}
}

// Epoch sets the epoch.
func (b *ReportBuilder) Epoch(epoch uint64) *ReportBuilder {
	b.epoch = epoch

	return b
}

// PrevHash sets the hash before the first event in this report.
func (b *ReportBuilder) PrevHash(prevHash string) *ReportBuilder {
	b.prevHash = prevHash

	return b
}

// Head sets the hash after the last event.
func (b *ReportBuilder) Head(head string) *ReportBuilder {
	b.head = head

	return b
}

// Commits sets the sequence range and the embedded events (raw bytes).
// An event too large for the wire format is remembered and reported by
// Build.
func (b *ReportBuilder) Commits(seqFrom, seqTo uint64, events [][]byte) *ReportBuilder {
	b.seqFrom = seqFrom
	b.seqTo = seqTo

	encoded, err := encodeEvents(events)
	if err != nil {
		b.err = err

		return b
	}

	b.eventsRaw = encoded

	return b
}

// Counters sets the informative counters.
func (b *ReportBuilder) Counters(counters map[string]int64) *ReportBuilder {
	b.counters = counters

	return b
}

// SetBuild sets the build version string.
func (b *ReportBuilder) SetBuild(build string) *ReportBuilder {
	b.build = build

	return b
}

// Build returns the assembled Report (unsigned), or the error recorded
// by a builder step.
func (b *ReportBuilder) Build() (*Report, error) {
	if b.err != nil {
		return nil, b.err
	}

	return &Report{
		Format:      FormatV1,
		Domain:      b.domain,
		Fingerprint: b.fingerprint,
		Epoch:       b.epoch,
		SeqFrom:     b.seqFrom,
		SeqTo:       b.seqTo,
		PrevHash:    b.prevHash,
		Events:      base64.StdEncoding.EncodeToString(b.eventsRaw),
		Head:        b.head,
		Counters:    b.counters,
		Build:       b.build,
	}, nil
}

// SigningPayload returns the bytes the report signature covers:
//
//	"backplane-usage-sig/v1" || domain || fingerprint || epoch_be64 ||
//	seqFrom_be64 || seqTo_be64 || prevHash || head
func (r *Report) SigningPayload() []byte {
	size := len(sigPrefix) + len(r.Domain) + len(r.Fingerprint) + len(r.PrevHash) + len(r.Head) +
		payloadWords*wordSize

	buf := make([]byte, 0, size)

	buf = append(buf, []byte(sigPrefix)...)
	buf = append(buf, []byte(r.Domain)...)
	buf = append(buf, []byte(r.Fingerprint)...)

	var word [wordSize]byte

	binary.BigEndian.PutUint64(word[:], r.Epoch)
	buf = append(buf, word[:]...)

	binary.BigEndian.PutUint64(word[:], r.SeqFrom)
	buf = append(buf, word[:]...)

	binary.BigEndian.PutUint64(word[:], r.SeqTo)
	buf = append(buf, word[:]...)

	buf = append(buf, []byte(r.PrevHash)...)
	buf = append(buf, []byte(r.Head)...)

	return buf
}

// Sign signs the report with priv and sets r.Signature.
func (r *Report) Sign(priv ed25519.PrivateKey) {
	payload := r.SigningPayload()
	sig := ed25519.Sign(priv, payload)
	r.Signature = base64.StdEncoding.EncodeToString(sig)
}

// Verify checks the report's signature against pub and its chain
// integrity against acked. A zero acked means verify from genesis.
//
// Returns the new accepted state on success.
func (r *Report) Verify(pub ed25519.PublicKey, acked Acked) (Acked, error) {
	if r.Format != FormatV1 {
		return Acked{}, fmt.Errorf("%w: %d", ErrFormat, r.Format)
	}

	// 1. Check signature.
	payload := r.SigningPayload()

	sig, err := base64.StdEncoding.DecodeString(r.Signature)
	if err != nil {
		return Acked{}, fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}

	if !ed25519.Verify(pub, payload, sig) {
		return Acked{}, ErrInvalidSignature
	}

	// 2. Check chain integrity.
	prevHash, err := parseHexHash(r.PrevHash)
	if err != nil {
		return Acked{}, fmt.Errorf("prevHash: %w", err)
	}

	// 3. Recompute the chain from prevHash through events.
	events, err := DecodeEvents(r)
	if err != nil {
		return Acked{}, err
	}

	computedHead, lastSeq := chainHead(prevHash, r.SeqFrom-1, events)

	expectedHead, err := parseHexHash(r.Head)
	if err != nil {
		return Acked{}, fmt.Errorf("head: %w", err)
	}

	if computedHead != expectedHead {
		return Acked{}, ErrChainBroken
	}

	if lastSeq != r.SeqTo {
		return Acked{}, ErrChainBroken
	}

	// 4. Check against the acked state.
	return r.verifyTransition(acked, prevHash, computedHead)
}

// verifyTransition checks the report against the verifier's acked
// state: the idempotent re-send, the same-epoch continuation and the
// new-epoch restart. A zero acked verifies from genesis. It returns
// the new accepted state.
func (r *Report) verifyTransition(acked Acked, prevHash, computedHead [sha256.Size]byte) (Acked, error) {
	if acked.IsZero() {
		// First report: verify genesis.
		if r.SeqFrom != 1 {
			return Acked{}, ErrGap
		}

		expectedInit := initHash(r.Domain, r.Fingerprint, r.Epoch)

		if prevHash != expectedInit {
			return Acked{}, fmt.Errorf("%w: wrong init hash", ErrFork)
		}

		return r.accepted(computedHead), nil
	}

	// Idempotent re-send: same report already accepted.
	if acked.Epoch == r.Epoch && acked.Seq == r.SeqTo {
		ackedHash, err := parseHexHash(acked.Hash)
		if err != nil {
			return Acked{}, ErrFormat
		}

		if computedHead == ackedHash {
			return acked, nil
		}

		return Acked{}, ErrFork
	}

	if r.Epoch < acked.Epoch {
		return Acked{}, ErrFork
	}

	if r.Epoch == acked.Epoch {
		// Same epoch: prevHash must continue the acked chain.
		ackedHash, err := parseHexHash(acked.Hash)
		if err != nil {
			return Acked{}, ErrFormat
		}

		if prevHash != ackedHash {
			return Acked{}, ErrFork
		}

		if r.SeqFrom != acked.Seq+1 {
			return Acked{}, ErrGap
		}

		return r.accepted(computedHead), nil
	}

	// New epoch: the chain restarts from its init hash.
	if r.SeqFrom != 1 {
		return Acked{}, ErrGap
	}

	expectedInit := initHash(r.Domain, r.Fingerprint, r.Epoch)

	if prevHash != expectedInit {
		return Acked{}, fmt.Errorf("%w: wrong new-epoch init hash", ErrFork)
	}

	return r.accepted(computedHead), nil
}

// accepted is the acked state after accepting the report's head.
func (r *Report) accepted(head [sha256.Size]byte) Acked {
	return Acked{Epoch: r.Epoch, Seq: r.SeqTo, Hash: hexHash(head)}
}

// IdempotencyKey returns the natural deduplication key of the report:
// (fingerprint, epoch, seq_to, head).
func (r *Report) IdempotencyKey() IdempotencyKey {
	return IdempotencyKey{
		Fingerprint: r.Fingerprint,
		Epoch:       r.Epoch,
		SeqTo:       r.SeqTo,
		Head:        r.Head,
	}
}

// DecodeEvents returns the decoded event bytes from the report.
func DecodeEvents(r *Report) ([][]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(r.Events)
	if err != nil {
		return nil, fmt.Errorf("%w: decode events base64: %w", ErrFormat, err)
	}

	return decodeEvents(raw)
}

// IterEvents returns a slice of (seq, event, hash) for each event in
// the report. The hashes are computed from the report's prevHash.
func (r *Report) IterEvents() ([]Event, error) {
	events, err := DecodeEvents(r)
	if err != nil {
		return nil, err
	}

	prev, err := parseHexHash(r.PrevHash)
	if err != nil {
		return nil, fmt.Errorf("prevHash: %w", err)
	}

	result := make([]Event, 0, len(events))
	h := prev
	seq := r.SeqFrom - 1

	for _, event := range events {
		seq++
		h = nextHash(h, seq, event)
		result = append(result, Event{Seq: seq, Data: event, Hash: hexHash(h)})
	}

	return result, nil
}

// Event is a single event from a report.
type Event struct {
	Seq  uint64
	Data []byte
	Hash string
}

// Acked is the verifier's last accepted chain state, and the wire
// shape of the server's ack in a report-usage response.
type Acked struct {
	Epoch uint64 `json:"epoch"`
	Seq   uint64 `json:"seq"`
	Hash  string `json:"hash"`
}

// IsZero reports whether the acked state is the zero value (no events
// accepted yet).
func (a Acked) IsZero() bool {
	return a.Epoch == 0 && a.Seq == 0
}

// IdempotencyKey is the natural deduplication key for a report.
type IdempotencyKey struct {
	Fingerprint string
	Epoch       uint64
	SeqTo       uint64
	Head        string
}

// String returns a compact representation of the idempotency key.
func (k IdempotencyKey) String() string {
	const shortHash = 16

	return fmt.Sprintf("%s/%d/%d/%s", k.Fingerprint, k.Epoch, k.SeqTo, k.Head[:min(len(k.Head), shortHash)])
}

// Response is the server's response to a Usage call.
type Response struct {
	Epoch  uint64          `json:"epoch"`
	Events []ResponseEvent `json:"events"`
	Head   ResponseHead    `json:"head"`
	Acks   []Acked         `json:"acks"`
}

// ResponseEvent is a single event in a usage response.
type ResponseEvent struct {
	Seq   uint64 `json:"seq"`
	Event string `json:"event"`
	Hash  string `json:"hash"`
}

// ResponseHead is the chain head in a usage response.
type ResponseHead struct {
	Seq  uint64 `json:"seq"`
	Hash string `json:"hash"`
}
