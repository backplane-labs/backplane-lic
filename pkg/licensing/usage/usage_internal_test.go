package usage

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

func TestChain_InitHash(t *testing.T) {
	t.Parallel()

	h := initHash("test/v1", "abcd1234", 42)

	if h == [sha256.Size]byte{} {
		t.Error("init hash is zero")
	}
}

func TestChain_InitHash_Deterministic(t *testing.T) {
	t.Parallel()

	h1 := initHash("test/v1", "fp", 1)
	h2 := initHash("test/v1", "fp", 1)

	if h1 != h2 {
		t.Error("init hash is not deterministic")
	}
}

func TestChain_InitHash_DifferentInputs(t *testing.T) {
	t.Parallel()

	h1 := initHash("test/v1", "fp1", 1)
	h2 := initHash("test/v1", "fp2", 1)
	h3 := initHash("test/v2", "fp1", 1)
	h4 := initHash("test/v1", "fp1", 2)

	if h1 == h2 {
		t.Error("different fingerprints produce same hash")
	}

	if h1 == h3 {
		t.Error("different domains produce same hash")
	}

	if h1 == h4 {
		t.Error("different epochs produce same hash")
	}
}

func TestChain_NextHash(t *testing.T) {
	t.Parallel()

	h0 := initHash("test/v1", "fp", 0)

	h1 := nextHash(h0, 1, []byte("event-a"))
	h2 := nextHash(h1, 2, []byte("event-b"))

	// Verify chain: recompute from h0.
	h1r := nextHash(h0, 1, []byte("event-a"))
	if h1 != h1r {
		t.Error("nextHash is not deterministic")
	}

	h2r := nextHash(h1r, 2, []byte("event-b"))
	if h2 != h2r {
		t.Error("chain verification failed")
	}
}

func TestChain_NextHash_SequenceMatters(t *testing.T) {
	t.Parallel()

	h0 := initHash("test/v1", "fp", 0)

	h1 := nextHash(h0, 1, []byte("a"))
	h2 := nextHash(h0, 2, []byte("a"))

	if h1 == h2 {
		t.Error("different sequences produce same hash")
	}
}

func TestEncodeDecodeEvents(t *testing.T) {
	t.Parallel()

	events := [][]byte{
		[]byte("hello"),
		[]byte("world"),
		{},
		[]byte("longer-event-with-more-data"),
	}

	encoded, err := encodeEvents(events)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := decodeEvents(encoded)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != len(events) {
		t.Fatalf("got %d events, want %d", len(decoded), len(events))
	}

	for i, event := range events {
		if string(decoded[i]) != string(event) {
			t.Errorf("event %d: got %q, want %q", i, decoded[i], event)
		}
	}
}

func TestDecodeEvents_Truncated(t *testing.T) {
	t.Parallel()

	// 4 bytes claiming 100-byte event, but only 10 bytes follow.
	data := make([]byte, 14)
	data[0] = 0
	data[1] = 0
	data[2] = 0
	data[3] = 100

	_, err := decodeEvents(data)
	if err == nil {
		t.Fatal("expected error for truncated events")
	}
}

func TestReport_SignAndVerify(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	fp := "test-fingerprint-0000000000000000000000000000000000000000000000000000000000000000"
	domain := "test-usage/v1"
	epoch := uint64(12345)

	// Build a simple report.
	events := [][]byte{[]byte("event-1"), []byte("event-2")}
	init := initHash(domain, fp, epoch)

	prev := init

	for i, event := range events {
		prev = nextHash(prev, uint64(i+1), event)
	}

	report, err := NewReport(domain, fp).
		Epoch(epoch).
		PrevHash(hexHash(init)).
		Commits(1, 2, events).
		Head(hexHash(prev)).
		SetBuild("1.0.0").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	report.Sign(priv)

	// Verify.
	acked, err := report.Verify(pub, Acked{})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if acked.Epoch != epoch {
		t.Errorf("epoch = %d", acked.Epoch)
	}

	if acked.Seq != 2 {
		t.Errorf("seq = %d", acked.Seq)
	}

	if acked.Hash != hexHash(prev) {
		t.Errorf("hash mismatch")
	}
}

func TestReport_Verify_BadSignature(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	_, wrongPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	pub := ed25519.PublicKey(wrongPriv[32:])

	report, err := NewReport("test/v1", "fp-0000000000000000000000000000000000000000000000000000000000000000").
		Epoch(1).
		PrevHash(hexHash(initHash("test/v1", "fp-0000000000000000000000000000000000000000000000000000000000000000", 1))).
		Commits(1, 1, [][]byte{[]byte("ev")}).
		Head(hexHash(nextHash(initHash("test/v1", "fp-0000000000000000000000000000000000000000000000000000000000000000", 1), 1, []byte("ev")))).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	report.Sign(priv) // Signed with priv, verifying with wrongPub.

	_, err = report.Verify(pub, Acked{})
	if !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestReport_Verify_Fork(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	fp := "fp-000000000000000000000000000000000000000000000000000000000000000000"
	domain := "test/v1"

	// Accept report 1-2.
	r1 := makeReport(t, domain, fp, 1, 1, 2, [][]byte{[]byte("a"), []byte("b")}, Acked{}, pub, priv)
	acked, err := r1.Verify(pub, Acked{})
	if err != nil {
		t.Fatal(err)
	}

	if acked.Seq != 2 {
		t.Fatalf("acked seq = %d", acked.Seq)
	}

	// Now submit a report that claims seq 3 but with a different prevHash (fork).
	r2 := makeReport(t, domain, fp, 1, 3, 3, [][]byte{[]byte("c")}, Acked{}, pub, priv)
	// Tamper prevHash to fork.
	r2.PrevHash = "0000000000000000000000000000000000000000000000000000000000000000"
	r2.Sign(priv)

	_, err = r2.Verify(pub, acked)
	if !errors.Is(err, ErrChainBroken) {
		t.Fatalf("expected ErrChainBroken, got: %v", err)
	}
}

func TestReport_Verify_Gap(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	fp := "fp-00000000000000000000000000000000000000000000000000000000000000000"
	domain := "test/v1"

	// Accept report 1-1.
	r1 := makeReport(t, domain, fp, 1, 1, 1, [][]byte{[]byte("a")}, Acked{}, pub, priv)
	acked, err := r1.Verify(pub, Acked{})
	if err != nil {
		t.Fatal(err)
	}

	if acked.Seq != 1 {
		t.Fatalf("acked seq = %d", acked.Seq)
	}

	// Submit report 3-3 (gap: missing seq 2). Build a self-consistent
	// report that starts from the correct prevHash but skips seq 2.
	correctPrev := nextHash(initHash(domain, fp, 1), 1, []byte("a"))
	r2, err := NewReport(domain, fp).
		Epoch(1).
		PrevHash(hexHash(correctPrev)).
		Commits(3, 3, [][]byte{[]byte("c")}).
		Head(hexHash(nextHash(correctPrev, 3, []byte("c")))).
		SetBuild("test").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	r2.Sign(priv)

	_, err = r2.Verify(pub, acked)
	if !errors.Is(err, ErrGap) {
		t.Fatalf("expected ErrGap, got: %v", err)
	}
}

func TestReport_IdempotentResend(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	fp := "fp-000000000000000000000000000000000000000000000000000000000000000000"
	domain := "test/v1"

	r1 := makeReport(t, domain, fp, 1, 1, 2, [][]byte{[]byte("a"), []byte("b")}, Acked{}, pub, priv)
	acked, err := r1.Verify(pub, Acked{})
	if err != nil {
		t.Fatal(err)
	}

	// Resend the same report.
	acked2, err := r1.Verify(pub, acked)
	if err != nil {
		t.Fatal(err)
	}

	if acked2.Seq != acked.Seq {
		t.Error("idempotent re-send changed acked seq")
	}
}

func TestReport_SigningPayload(t *testing.T) {
	t.Parallel()

	report, err := NewReport("test/v1", "fp-0000000000000000000000000000000000000000000000000000000000000000").
		Epoch(1).
		PrevHash("aaaa000000000000000000000000000000000000000000000000000000000000").
		Commits(1, 3, [][]byte{[]byte("x"), []byte("y"), []byte("z")}).
		Head("bbbb000000000000000000000000000000000000000000000000000000000000").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	payload := report.SigningPayload()

	// Must start with prefix.
	if string(payload[:len(sigPrefix)]) != sigPrefix {
		t.Error("payload missing signature prefix")
	}
}

func TestReport_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	fp := "fp-00000000000000000000000000000000000000000000000000000000000000000"
	domain := "test/v1"
	epoch := uint64(1)
	init := initHash(domain, fp, epoch)

	report, err := NewReport(domain, fp).
		Epoch(epoch).
		PrevHash(hexHash(init)).
		Commits(1, 2, [][]byte{[]byte("a"), []byte("b")}).
		Head(hexHash(nextHash(nextHash(init, 1, []byte("a")), 2, []byte("b")))).
		SetBuild("1.0.0").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	report.Sign(priv)

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	var decoded Report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Format != FormatV1 {
		t.Errorf("format = %d", decoded.Format)
	}

	if decoded.Fingerprint != fp {
		t.Errorf("fingerprint mismatch")
	}

	if decoded.SeqFrom != 1 || decoded.SeqTo != 2 {
		t.Errorf("seq range = %d-%d", decoded.SeqFrom, decoded.SeqTo)
	}

	if decoded.Signature == "" {
		t.Error("signature is empty")
	}
}

func TestIdempotencyKey(t *testing.T) {
	t.Parallel()

	r, err := NewReport("d", "fp-0000000000000000000000000000000000000000000000000000000000000000").
		Epoch(1).
		PrevHash("aaaa000000000000000000000000000000000000000000000000000000000000").
		Commits(1, 5, [][]byte{[]byte("x")}).
		Head("bbbb000000000000000000000000000000000000000000000000000000000000").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	key := r.IdempotencyKey()

	if key.Fingerprint != r.Fingerprint {
		t.Error("fingerprint mismatch in key")
	}

	if key.Epoch != r.Epoch {
		t.Error("epoch mismatch in key")
	}

	if key.SeqTo != r.SeqTo {
		t.Error("seqTo mismatch in key")
	}

	if key.Head != r.Head {
		t.Error("head mismatch in key")
	}
}

func TestDecodeEvents_FromReport(t *testing.T) {
	t.Parallel()

	report, err := NewReport("t", "fp-0000000000000000000000000000000000000000000000000000000000000000").
		Epoch(1).
		PrevHash("aaaa000000000000000000000000000000000000000000000000000000000000").
		Commits(1, 3, [][]byte{[]byte("a"), []byte("b"), []byte("c")}).
		Head("bbbb000000000000000000000000000000000000000000000000000000000000").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	events, err := DecodeEvents(report)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 3 {
		t.Fatalf("got %d events", len(events))
	}

	if string(events[0]) != "a" || string(events[1]) != "b" || string(events[2]) != "c" {
		t.Error("event data mismatch")
	}
}

func TestReport_IterEvents(t *testing.T) {
	t.Parallel()

	domain := "t"
	fp := "fp-0000000000000000000000000000000000000000000000000000000000000000"
	epoch := uint64(1)
	init := initHash(domain, fp, epoch)

	report, err := NewReport(domain, fp).
		Epoch(epoch).
		PrevHash(hexHash(init)).
		Commits(1, 2, [][]byte{[]byte("x"), []byte("y")}).
		Head(hexHash(nextHash(nextHash(init, 1, []byte("x")), 2, []byte("y")))).
		Build()
	if err != nil {
		t.Fatal(err)
	}

	iter, err := report.IterEvents()
	if err != nil {
		t.Fatal(err)
	}

	if len(iter) != 2 {
		t.Fatalf("got %d events", len(iter))
	}

	if iter[0].Seq != 1 {
		t.Errorf("seq[0] = %d", iter[0].Seq)
	}

	if iter[1].Seq != 2 {
		t.Errorf("seq[1] = %d", iter[1].Seq)
	}
}

// makeReport is a test helper that creates and signs a Report.
func makeReport(
	t *testing.T,
	domain, fp string,
	epoch, seqFrom, seqTo uint64,
	events [][]byte,
	acked Acked,
	pub ed25519.PublicKey,
	priv ed25519.PrivateKey,
) *Report {
	t.Helper()

	init := initHash(domain, fp, epoch)

	prevHash := hexHash(init)
	prev := init

	if !acked.IsZero() && acked.Epoch == epoch {
		var err error

		prev, err = parseHexHash(acked.Hash)
		if err != nil {
			t.Fatal(err)
		}

		prevHash = acked.Hash
	}

	head, _ := chainHead(prev, seqFrom-1, events)

	r, err := NewReport(domain, fp).
		Epoch(epoch).
		PrevHash(prevHash).
		Commits(seqFrom, seqTo, events).
		Head(hexHash(head)).
		SetBuild("test").
		Build()
	if err != nil {
		t.Fatal(err)
	}

	r.Sign(priv)

	return r
}

func TestJournal_Basic(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	domain := "test/v1"
	fp := "fp-0000000000000000000000000000000000000000000000000000000000000000"
	epoch := uint64(42)

	j := NewJournal(domain, fp, epoch, Acked{})

	// Append events.
	seq1, h1, err := j.Append([]byte("event-1"))
	if err != nil {
		t.Fatal(err)
	}

	seq2, h2, err := j.Append([]byte("event-2"))
	if err != nil {
		t.Fatal(err)
	}

	if seq1 != 1 || seq2 != 2 {
		t.Errorf("seq = %d, %d", seq1, seq2)
	}

	if h1 == "" || h2 == "" {
		t.Error("hash is empty")
	}

	// Get head.
	seq, hash := j.Head()
	if seq != 2 || hash == "" {
		t.Errorf("head = %d, %s", seq, hash)
	}

	// Build report.
	report, err := j.Report("1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	report.Sign(priv)

	acked, err := report.Verify(pub, Acked{})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if acked.Seq != 2 {
		t.Errorf("acked seq = %d", acked.Seq)
	}

	// Ack.
	j.Ack(acked)

	// Append more.
	if _, _, err := j.Append([]byte("event-3")); err != nil {
		t.Fatal(err)
	}

	report2, err := j.Report("1.0.0")
	if err != nil {
		t.Fatal(err)
	}

	report2.Sign(priv)

	acked2, err := report2.Verify(pub, acked)
	if err != nil {
		t.Fatalf("verify after ack: %v", err)
	}

	if acked2.Seq != 3 {
		t.Errorf("acked seq = %d", acked2.Seq)
	}
}

func BenchmarkChainHash(b *testing.B) {
	init := initHash("test/v1", "fp-0000000000000000000000000000000000000000000000000000000000000000", 1)
	h := init

	b.ResetTimer()

	for b.Loop() {
		h = nextHash(h, 1, []byte("benchmark-event-data"))
	}

	_ = h
}

func BenchmarkReportVerify(b *testing.B) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		b.Fatal(err)
	}
	domain := "t"
	fp := "fp-0000000000000000000000000000000000000000000000000000000000000000"
	init := initHash(domain, fp, 1)
	events := makeEvents(10)
	head, _ := chainHead(init, 0, events)

	r, err := NewReport(domain, fp).
		Epoch(1).
		PrevHash(hexHash(init)).
		Commits(1, 10, events).
		Head(hexHash(head)).
		SetBuild("1.0.0").
		Build()
	if err != nil {
		b.Fatal(err)
	}

	r.Sign(priv)

	for b.Loop() {
		if _, err := r.Verify(pub, Acked{}); err != nil {
			b.Fatal(err)
		}
	}
}

func makeEvents(n int) [][]byte {
	events := make([][]byte, n)

	for i := range n {
		events[i] = []byte(base64.StdEncoding.EncodeToString([]byte{byte(i)}))
	}

	return events
}
