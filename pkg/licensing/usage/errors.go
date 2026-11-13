package usage

import "errors"

// Typed sentinels matched with errors.Is.
var (
	// ErrInvalidSignature reports a report whose Ed25519 signature does
	// not verify.
	ErrInvalidSignature = errors.New("report signature is invalid")

	// ErrGap reports a report with seq_from > last_acked_seq + 1.
	ErrGap = errors.New("report has a gap: sequences are not consecutive from the last acked point")

	// ErrFork reports a report whose prev_hash does not match the last
	// acked hash at the same epoch.
	ErrFork = errors.New("report forks from the accepted chain")

	// ErrChainBroken reports prev_hash does not match the computed
	// hash of the acked state (non-zero epoch).
	ErrChainBroken = errors.New("report prev_hash does not match the last accepted hash")

	// ErrFormat reports an unknown or unsupported report format version.
	ErrFormat = errors.New("unsupported report format")
)
