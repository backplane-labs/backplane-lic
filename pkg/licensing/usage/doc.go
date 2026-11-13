// Package usage implements a signed hash chain for usage journals.
//
// Every event extends a SHA-256 hash chain that starts from an init
// hash over (domain, fingerprint, epoch). A Report carries a run of
// events, the hash before and after them, and an Ed25519 signature of
// the installation key.
//
// This package provides pure data types, chain logic, signing and
// verification. The API methods (ReportUsage, Usage) live on
// *licensing.Machine in the parent licensing package, which can
// access the client's transport.
//
// Journal (installation side):
//
//	ack := usage.Acked{} // zero = fresh
//	j := usage.NewJournal("komeet-usage/v1", key.Fingerprint(), epoch, ack)
//	seq, hash, err := j.Append([]byte("event-data"))
//	report, pending, err := j.AckPending("1.0.0")
//	report.Sign(key.PrivateKey())
//	// ... send report to server, then j.Ack(ack) ...
//
// Server side verification:
//
//	pub, _ := installation.PublicKeyOf(machine)
//	acked, err := report.Verify(pub, lastAcked)
package usage
