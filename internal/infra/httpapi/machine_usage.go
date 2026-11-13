package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strconv"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

// ReportMachineUsage implements POST …/machines/{id}/actions/report-usage.
func (a *API) ReportMachineUsage(
	ctx context.Context, _ oas.ReportMachineUsageReq, params oas.ReportMachineUsageParams,
) (*oas.MachineUsageAckDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	machine, ack, err := a.machines.ReportUsage(ctx, req.account, params.MachineId, func() (usage.Report, error) {
		return usageReportBody(req.raw)
	})
	if err != nil {
		return nil, err
	}

	wire, err := usageAck(ack)
	if err != nil {
		return nil, err
	}

	return &oas.MachineUsageAckDocument{
		Data: render.Machine(req.account.ID, machine), Meta: oas.MachineUsageAckDocumentMeta{Ack: wire},
	}, nil
}

// ShowMachineUsage implements GET …/machines/{id}/usage.
func (a *API) ShowMachineUsage(
	ctx context.Context, params oas.ShowMachineUsageParams,
) (*oas.MachineUsageDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	after, err := queryUint(req, "after")
	if err != nil {
		return nil, err
	}

	limit, err := queryUint(req, "limit")
	if err != nil {
		return nil, err
	}

	resp, err := a.machines.Usage(ctx, req.account, params.MachineId, after, int(min(limit, math.MaxInt32)))
	if err != nil {
		return nil, err
	}

	return usageDocument(resp)
}

// usageReportBody reads {"meta":{"report":{…}}} as sent (the report's
// keys are its wire names, not underscored attributes).
func usageReportBody(raw []byte) (usage.Report, error) {
	var body struct {
		Meta struct {
			Report *usage.Report `json:"report"`
		} `json:"meta"`
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&body); err != nil {
		return usage.Report{}, apperr.BadRequestSource("is invalid", "/meta/report")
	}

	if body.Meta.Report == nil {
		return usage.Report{}, apperr.BadRequestSource("is missing", "/meta/report")
	}

	return *body.Meta.Report, nil
}

func usageDocument(resp usage.Response) (*oas.MachineUsageDocument, error) {
	epoch, err := wireInt(resp.Epoch)
	if err != nil {
		return nil, err
	}

	headSeq, err := wireInt(resp.Head.Seq)
	if err != nil {
		return nil, err
	}

	meta := oas.MachineUsageDocumentMeta{
		Epoch: epoch, Head: oas.MachineUsageDocumentMetaHead{Seq: headSeq, Hash: resp.Head.Hash},
		Events: make([]oas.MachineUsageDocumentMetaEventsItem, 0, len(resp.Events)),
		Acks:   make([]oas.UsageAck, 0, len(resp.Acks)),
	}

	for _, event := range resp.Events {
		seq, err := wireInt(event.Seq)
		if err != nil {
			return nil, err
		}

		meta.Events = append(meta.Events, oas.MachineUsageDocumentMetaEventsItem{
			Seq: seq, Event: event.Event, Hash: event.Hash,
		})
	}

	for _, ack := range resp.Acks {
		wire, err := usageAck(ack)
		if err != nil {
			return nil, err
		}

		meta.Acks = append(meta.Acks, wire)
	}

	return &oas.MachineUsageDocument{Meta: meta}, nil
}

func usageAck(ack usage.Acked) (oas.UsageAck, error) {
	epoch, err := wireInt(ack.Epoch)
	if err != nil {
		return oas.UsageAck{}, err
	}

	seq, err := wireInt(ack.Seq)

	return oas.UsageAck{Epoch: epoch, Seq: seq, Hash: ack.Hash}, err
}

// errUsageNumber: a chain number the JSON integer cannot carry.
var errUsageNumber = apperr.Internal()

func wireInt(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, errUsageNumber
	}

	return int64(v), nil
}

// queryUint reads an optional non-negative integer query parameter.
func queryUint(req *request, name string) (uint64, error) {
	raw := req.query.Get(name)
	if raw == "" {
		return 0, nil
	}

	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, apperr.BadRequestParameter("must be a non-negative integer", name)
	}

	return value, nil
}
