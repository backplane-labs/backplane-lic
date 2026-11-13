package usage

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
)

// wordSize is the byte length of a big-endian uint64 in the chain.
const wordSize = 8

// lengthPrefixLen is the byte length of an event's length prefix in the
// canonical events encoding.
const lengthPrefixLen = 4

// initHash computes the genesis hash for a chain:
//
//	hash_0 = SHA-256(domain || fingerprint || epoch_be64)
func initHash(domain, fingerprint string, epoch uint64) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte(fingerprint))

	var buf [wordSize]byte
	binary.BigEndian.PutUint64(buf[:], epoch)
	h.Write(buf[:])

	var out [sha256.Size]byte
	h.Sum(out[:0])

	return out
}

// nextHash computes the next hash in the chain:
//
//	hash_n = SHA-256(hash_{n-1} || seq_be64 || event)
func nextHash(prev [sha256.Size]byte, seq uint64, event []byte) [sha256.Size]byte {
	h := sha256.New()
	h.Write(prev[:])

	var buf [wordSize]byte
	binary.BigEndian.PutUint64(buf[:], seq)
	h.Write(buf[:])
	h.Write(event)

	var out [sha256.Size]byte
	h.Sum(out[:0])

	return out
}

// chainHead computes the head hash after applying all events starting
// from prev. startSeq is the sequence number BEFORE the first event
// (often seqFrom-1). Returns the head hash and the last sequence number.
func chainHead(prev [sha256.Size]byte, startSeq uint64, events [][]byte) ([sha256.Size]byte, uint64) {
	h := prev
	seq := startSeq

	for _, event := range events {
		seq++
		h = nextHash(h, seq, event)
	}

	return h, seq
}

// encodeEvents encodes a slice of events in the canonical wire format:
//
//	[4-byte big-endian length][event bytes]...
//
// An event longer than the 4-byte length prefix can hold is refused
// with ErrFormat.
func encodeEvents(events [][]byte) ([]byte, error) {
	total := 0

	for _, e := range events {
		if uint64(len(e)) > math.MaxUint32 {
			return nil, fmt.Errorf("%w: event of %d bytes exceeds the length prefix", ErrFormat, len(e))
		}

		total += lengthPrefixLen + len(e)
	}

	out := make([]byte, 0, total)
	buf := make([]byte, lengthPrefixLen)

	for _, e := range events {
		binary.BigEndian.PutUint32(buf, uint32(min(len(e), math.MaxUint32)))
		out = append(out, buf...)
		out = append(out, e...)
	}

	return out, nil
}

// decodeEvents decodes the canonical wire format back to a slice of events.
func decodeEvents(data []byte) ([][]byte, error) {
	var events [][]byte

	pos := 0

	for pos < len(data) {
		if pos+lengthPrefixLen > len(data) {
			return nil, ErrFormat
		}

		eventLen := binary.BigEndian.Uint32(data[pos:])
		pos += lengthPrefixLen

		if uint64(pos)+uint64(eventLen) > uint64(len(data)) {
			return nil, ErrFormat
		}

		event := make([]byte, eventLen)
		copy(event, data[pos:pos+int(eventLen)])
		events = append(events, event)
		pos += int(eventLen)
	}

	if pos != len(data) {
		return nil, ErrFormat
	}

	return events, nil
}

// hexHash returns the hex encoding of a hash.
func hexHash(h [sha256.Size]byte) string {
	return hex.EncodeToString(h[:])
}

// parseHexHash parses a hex-encoded SHA-256 hash.
func parseHexHash(s string) ([sha256.Size]byte, error) {
	var out [sha256.Size]byte

	decoded, err := hex.DecodeString(s)
	if err != nil {
		return out, fmt.Errorf("%w: hash %q: %w", ErrFormat, s, err)
	}

	if len(decoded) != sha256.Size {
		return out, ErrFormat
	}

	copy(out[:], decoded)

	return out, nil
}
