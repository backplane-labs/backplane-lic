package machines

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

// The usage journal is a backplane extension: an installation reports
// its signed usage hash chain (pkg/licensing/usage) to its machine; the
// product's backend reads the accepted chain back.

// Usage read bounds.
const (
	usageDefaultLimit = 100
	usageMaxLimit     = 1000
)

// UsageStore keeps a machine's accepted usage chain.
type UsageStore interface {
	// UsageHead is the accepted state, locked for the transaction; the
	// zero value before the first report.
	UsageHead(ctx context.Context, machineID uuid.UUID, forUpdate bool) (usage.Acked, error)
	// AppendUsage records an accepted report: its events, its ack and the
	// new head.
	AppendUsage(ctx context.Context, accountID, machineID uuid.UUID, report usage.Report, events []usage.Event,
		ack usage.Acked) error
	UsageEvents(ctx context.Context, machineID uuid.UUID, epoch, after uint64, limit int) ([]usage.ResponseEvent, error)
	UsageAcks(ctx context.Context, machineID uuid.UUID, epoch, after uint64, limit int) ([]usage.Acked, error)
}

// ReportUsage accepts a signed usage report of the machine's
// installation: the signature by the key in the machine's publicKey
// metadata, the chain continuing the accepted head. A report already
// accepted answers its ack again.
func (s Scenario) ReportUsage(
	ctx context.Context, account domain.Account, idOrFingerprint string, decode func() (usage.Report, error),
) (domain.Machine, usage.Acked, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return domain.Machine{}, usage.Acked{}, err
	}

	if err := s.authorize(ctx, "machine.heartbeat.ping", machine, access.MachinePing); err != nil {
		return domain.Machine{}, usage.Acked{}, err
	}

	report, err := decode()
	if err != nil {
		return domain.Machine{}, usage.Acked{}, err
	}

	key, err := installationKey(machine)
	if err != nil {
		return domain.Machine{}, usage.Acked{}, err
	}

	if report.Fingerprint != machine.Fingerprint {
		return domain.Machine{}, usage.Acked{}, apperr.Unprocessable("USAGE_FINGERPRINT_MISMATCH",
			"must be the machine's fingerprint", "/meta/report/fingerprint")
	}

	var ack usage.Acked

	err = s.write(ctx, func(ctx context.Context) error {
		head, err := s.usage.UsageHead(ctx, machine.ID, true)
		if err != nil {
			return fmt.Errorf("machines: usage head: %w", err)
		}

		if ack, err = report.Verify(key, head); err != nil {
			return usageRefusal(err)
		}

		if ack == head {
			return nil // the re-send of an accepted report
		}

		events, err := report.IterEvents()
		if err != nil {
			return usageRefusal(err)
		}

		if err := s.usage.AppendUsage(ctx, account.ID, machine.ID, report, events, ack); err != nil {
			return fmt.Errorf("machines: append usage: %w", err)
		}

		return nil
	})
	if err != nil {
		return domain.Machine{}, usage.Acked{}, err
	}

	return machine, ack, nil
}

// Usage answers the accepted chain of the machine's current epoch after
// a sequence number: its events, the acks of its reports and the head.
// Only the vendor side reads it: the admin family and the product.
func (s Scenario) Usage(
	ctx context.Context, account domain.Account, idOrFingerprint string, after uint64, limit int,
) (usage.Response, error) {
	machine, err := s.load(ctx, account, idOrFingerprint)
	if err != nil {
		return usage.Response{}, err
	}

	if err := s.authorize(ctx, "machine.read", machine, access.MachineUsageRead); err != nil {
		return usage.Response{}, err
	}

	if limit <= 0 {
		limit = usageDefaultLimit
	}

	limit = min(limit, usageMaxLimit)

	head, err := s.usage.UsageHead(ctx, machine.ID, false)
	if err != nil {
		return usage.Response{}, fmt.Errorf("machines: usage head: %w", err)
	}

	out := usage.Response{
		Epoch: head.Epoch, Events: []usage.ResponseEvent{}, Acks: []usage.Acked{},
		Head: usage.ResponseHead{Seq: head.Seq, Hash: head.Hash},
	}

	if head.IsZero() {
		return out, nil
	}

	if out.Events, err = s.usage.UsageEvents(ctx, machine.ID, head.Epoch, after, limit); err != nil {
		return usage.Response{}, fmt.Errorf("machines: usage events: %w", err)
	}

	if out.Acks, err = s.usage.UsageAcks(ctx, machine.ID, head.Epoch, after, limit); err != nil {
		return usage.Response{}, fmt.Errorf("machines: usage acks: %w", err)
	}

	return out, nil
}

// installationKey is the Ed25519 key in the machine's publicKey metadata.
func installationKey(machine domain.Machine) (ed25519.PublicKey, error) {
	encoded, _ := machine.Metadata["public_key"].(string)
	if encoded == "" {
		return nil, apperr.Unprocessable("USAGE_KEY_MISSING", "the machine has no publicKey metadata",
			"/data/attributes/metadata/publicKey")
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, apperr.Unprocessable("USAGE_KEY_INVALID", "the machine's publicKey metadata is not an Ed25519 key",
			"/data/attributes/metadata/publicKey")
	}

	return ed25519.PublicKey(raw), nil
}

// usageRefusal maps a rejected report to its refusal; anything else is a
// failure.
func usageRefusal(err error) error {
	switch {
	case errors.Is(err, usage.ErrInvalidSignature):
		return apperr.Unprocessable("USAGE_SIGNATURE_INVALID", "is invalid", "/meta/report/signature")
	case errors.Is(err, usage.ErrChainBroken):
		return apperr.Unprocessable("USAGE_CHAIN_BROKEN", "does not match the reported events", "/meta/report/head")
	case errors.Is(err, usage.ErrFork):
		return &apperr.T{
			Status: apperr.Conflict("").Status, Title: apperr.TitleConflict, Code: "USAGE_FORK",
			Detail: "the report forks from the accepted chain", Source: apperr.Source{Pointer: "/meta/report/prevHash"},
		}
	case errors.Is(err, usage.ErrGap):
		return &apperr.T{
			Status: apperr.Conflict("").Status, Title: apperr.TitleConflict, Code: "USAGE_GAP",
			Detail: "the report does not continue the accepted chain", Source: apperr.Source{Pointer: "/meta/report/seqFrom"},
		}
	case errors.Is(err, usage.ErrFormat):
		return apperr.Unprocessable("USAGE_FORMAT_INVALID", err.Error(), "/meta/report")
	default:
		return fmt.Errorf("machines: usage report: %w", err)
	}
}
