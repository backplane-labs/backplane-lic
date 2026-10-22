package httpapi

import (
	"strconv"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/policies"
)

// The policy attributes the typed params admit, in wire (underscored)
// spelling. The update admits the same set minus the immutable three
// (sending those answers the same "unpermitted parameter").
var policyCreateAttrs = []string{ //nolint:gochecknoglobals // transcribed reference data
	"scheme", "encrypted", "use_pool", "name", "duration", "strict", "floating",
	"protected", "max_machines", "max_processes", "max_users", "max_cores",
	"max_memory", "max_disk", "max_uses", "machine_uniqueness_strategy",
	"machine_matching_strategy", "component_uniqueness_strategy",
	"component_matching_strategy", "expiration_strategy", "expiration_basis",
	"renewal_basis", "transfer_strategy", "authentication_strategy",
	"machine_leasing_strategy", "process_leasing_strategy", "overage_strategy",
	"require_product_scope", "require_policy_scope", "require_machine_scope",
	"require_fingerprint_scope", "require_components_scope", "require_user_scope",
	"require_checksum_scope", "require_version_scope", "require_check_in",
	"check_in_interval", "check_in_interval_count", "heartbeat_duration",
	"heartbeat_cull_strategy", "heartbeat_resurrection_strategy", "heartbeat_basis",
	"require_heartbeat", "metadata",
}

var policyUpdateAttrs = filterOut(policyCreateAttrs, "scheme", "encrypted", "use_pool") //nolint:gochecknoglobals,lll // transcribed reference data

func filterOut(list []string, drop ...string) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		removed := false
		for _, d := range drop { //nolint:wsl_v5 // dense by design
			if item == d {
				removed = true
				break
			}
		}
		if !removed { //nolint:wsl_v5 // dense by design
			out = append(out, item)
		}
	}

	return out
}

// validatePolicyBody shape-validates the create/update body like the
// reference's typed params: the create requires the product
// relationship; the update rejects the relationships block outright
// and the immutable attributes as unpermitted.
func validatePolicyBody( //nolint:gocognit,cyclop,funlen // the ladder, verbatim
	body map[string]any, update bool,
) (policies.Input, error) { // the validation table, verbatim
	var in policies.Input

	data, err := dataHash(body)
	if err != nil {
		return in, err
	}

	if update {
		if err := unpermitted(data, "/data/", "type", "id", "attributes"); err != nil {
			return in, err
		}
	} else if err := unpermitted(data, "/data/", "type", "attributes", "relationships"); err != nil {
		return in, err
	}

	if err := requireType(data, "policy", "policies"); err != nil {
		return in, err
	}

	attrs, hasAttrs, err := optionalAttributes(data)
	if err != nil {
		return in, err
	}

	if hasAttrs {
		allowed := policyCreateAttrs
		if update {
			allowed = policyUpdateAttrs
		}

		if err := unpermitted(attrs, "/data/attributes/", allowed...); err != nil {
			return in, err
		}

		if err := readPolicyAttrs(attrs, &in); err != nil {
			return in, err
		}
	}

	if update {
		if raw, ok := data["relationships"]; ok {
			if _, err := hash(raw, "/data/relationships"); err != nil {
				return in, err
			}

			return in, badRequest("unpermitted parameter", "/data/relationships")
		}

		return in, nil
	}

	// The create's product relationship is required.
	raw, ok := data["relationships"]
	if !ok {
		return in, badRequest("is missing", "/data/relationships")
	}

	rels, err := hash(raw, "/data/relationships")
	if err != nil {
		return in, err
	}

	if err := unpermitted(rels, "/data/relationships/", "product"); err != nil {
		return in, err
	}

	product, err := hash(rels["product"], "/data/relationships/product")
	if err != nil {
		return in, err
	}

	if err := unpermitted(product, "/data/relationships/product/", "data"); err != nil {
		return in, err
	}

	productData, err := hash(product["data"], "/data/relationships/product/data")
	if err != nil {
		return in, err
	}

	if err := unpermitted(productData, "/data/relationships/product/data/", "type", "id"); err != nil {
		return in, err
	}

	if err := requireType(productData, "product", "products"); err != nil {
		return in, err
	}

	idRaw, ok := productData["id"]
	if !ok {
		return in, badRequest("is missing", "/data/relationships/product/data/id")
	}

	idStr, err := str(idRaw, "/data/relationships/product/data/id")
	if err != nil {
		return in, err
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return in, badRequest(
			"type mismatch (received string expected UUID string)",
			"/data/relationships/product/data/id")
	}

	in.ProductID = id
	in.ProductOn = true

	return in, nil
}

// readPolicyAttrs reads every admitted attribute.
func readPolicyAttrs(attrs map[string]any, in *policies.Input) error { //nolint:funlen // the attribute table
	nameValue, _, nameErr := nonBlankStringAttr(attrs, "name")
	if nameErr != nil {
		return nameErr
	}

	in.Name = nameValue

	strs := []struct {
		dst  **string
		name string
	}{
		{&in.Scheme, "scheme"},
		{&in.Scheme, "scheme"},
		{&in.MachineUniquenessStrategy, "machine_uniqueness_strategy"},
		{&in.MachineMatchingStrategy, "machine_matching_strategy"},
		{&in.ComponentUniquenessStrategy, "component_uniqueness_strategy"},
		{&in.ComponentMatchingStrategy, "component_matching_strategy"},
		{&in.ExpirationStrategy, "expiration_strategy"},
		{&in.ExpirationBasis, "expiration_basis"},
		{&in.RenewalBasis, "renewal_basis"},
		{&in.TransferStrategy, "transfer_strategy"},
		{&in.AuthenticationStrategy, "authentication_strategy"},
		{&in.MachineLeasingStrategy, "machine_leasing_strategy"},
		{&in.ProcessLeasingStrategy, "process_leasing_strategy"},
		{&in.OverageStrategy, "overage_strategy"},
		{&in.CheckInInterval, "check_in_interval"},
		{&in.HeartbeatCullStrategy, "heartbeat_cull_strategy"},
		{&in.HeartbeatResurrectionStrategy, "heartbeat_resurrection_strategy"},
		{&in.HeartbeatBasis, "heartbeat_basis"},
	}

	for _, field := range strs {
		value, _, err := nullableStringAttr(attrs, field.name)
		if err != nil {
			return err
		}

		*field.dst = value
	}

	ints := []struct {
		dst  **int32
		set  *bool
		name string
	}{
		{&in.Duration, &in.DurationSet, "duration"},
		{&in.MaxMachines, &in.MaxMachinesSet, "max_machines"},
		{&in.MaxProcesses, &in.MaxProcessesSet, "max_processes"},
		{&in.MaxUsers, &in.MaxUsersSet, "max_users"},
		{&in.MaxCores, &in.MaxCoresSet, "max_cores"},
		{&in.MaxUses, &in.MaxUsesSet, "max_uses"},
		{&in.CheckInIntervalCount, &in.CheckInIntervalCountSet, "check_in_interval_count"},
		{&in.HeartbeatDuration, &in.HeartbeatDurationSet, "heartbeat_duration"},
	}

	for _, field := range ints {
		value, set, err := intAttr(attrs, field.name)
		if err != nil {
			return err
		}

		*field.dst = value
		*field.set = set
	}

	var err error

	if in.MaxMemory, in.MaxMemorySet, err = bigintAttr(attrs, "max_memory"); err != nil {
		return err
	}

	if in.MaxDisk, in.MaxDiskSet, err = bigintAttr(attrs, "max_disk"); err != nil {
		return err
	}

	bools := []struct {
		dst  **bool
		name string
	}{
		{&in.Strict, "strict"},
		{&in.Floating, "floating"},
		{&in.UsePool, "use_pool"},
		{&in.Encrypted, "encrypted"},
		{&in.RequireProductScope, "require_product_scope"},
		{&in.RequirePolicyScope, "require_policy_scope"},
		{&in.RequireMachineScope, "require_machine_scope"},
		{&in.RequireFingerprintScope, "require_fingerprint_scope"},
		{&in.RequireComponentsScope, "require_components_scope"},
		{&in.RequireUserScope, "require_user_scope"},
		{&in.RequireChecksumScope, "require_checksum_scope"},
		{&in.RequireVersionScope, "require_version_scope"},
		{&in.RequireCheckIn, "require_check_in"},
		{&in.RequireHeartbeat, "require_heartbeat"},
	}

	for _, field := range bools {
		value, _, err := boolAttr(attrs, field.name)
		if err != nil {
			return err
		}

		*field.dst = value
	}

	if in.Protected, in.ProtectedSet, err = boolAttr(attrs, "protected"); err != nil {
		return err
	}

	in.Metadata, in.MetadataSet, err = metadataAttr(attrs)

	return err
}

// validateEntitlementIDsBody shape-validates the attach/detach body:
// {"data":[{"type":"entitlement","id":"<uuid>"}…]} of 1..100 items.
func validateEntitlementIDsBody(body map[string]any) ([]uuid.UUID, error) {
	if body == nil {
		return nil, badRequest("is missing", "/data")
	}

	raw, ok := body["data"]
	if !ok || raw == nil {
		return nil, badRequest("is missing", "/data")
	}

	items, ok := raw.([]any)
	if !ok {
		return nil, badRequest("type mismatch (received "+jsonKind(raw)+" expected array)", "/data")
	}

	if len(items) < 1 || len(items) > 100 {
		return nil, badRequest("length must be between 1 and 100 (inclusive)", "/data")
	}

	ids := make([]uuid.UUID, 0, len(items))

	for index, item := range items {
		pointer := "/data/" + strconv.Itoa(index)

		entry, err := hash(item, pointer)
		if err != nil {
			return nil, err
		}

		if err := unpermitted(entry, pointer+"/", "type", "id"); err != nil {
			return nil, err
		}

		if err := requireType(entry, "entitlement", "entitlements"); err != nil {
			return nil, err
		}

		idRaw, ok := entry["id"] //nolint:govet // mechanical
		if !ok {
			return nil, badRequest("is missing", pointer+"/id")
		}

		idStr, err := str(idRaw, pointer+"/id")
		if err != nil {
			return nil, err
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, badRequest("type mismatch (received string expected UUID string)", pointer+"/id")
		}

		ids = append(ids, id)
	}

	return ids, nil
}
