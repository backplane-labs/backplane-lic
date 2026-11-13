package httpapi

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/machines"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

const (
	typeMachineSingular   = "machine"
	typeMachinePlural     = "machines"
	typeLicenseLinkSingle = "license"
	typeComponentSingular = "component"
	typeComponentPlural   = "components"
)

// Body keys the machine and component documents share.
const (
	keyAttributes  = "attributes"
	keyName        = "name"
	keyFingerprint = "fingerprint"
	keyMetadata    = "metadata"
	keyOwner       = "owner"
)

// IndexMachines implements GET …/machines.
func (a *API) IndexMachines(ctx context.Context, _ oas.IndexMachinesParams) (*oas.MachineListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, err
	}

	return a.machinePage(ctx, req, machinesFilter(req), params)
}

// machinePage pages the bearer's machines among filter and renders them.
func (a *API) machinePage(
	ctx context.Context, req *request, filter machines.Filter, params paging.Params,
) (*oas.MachineListDocument, error) {
	page, err := a.machines.Index(ctx, req.account, filter, params)
	if err != nil {
		return nil, err
	}

	doc := &oas.MachineListDocument{Data: make([]oas.MachineResource, 0, len(page.Items))}
	for i := range page.Items {
		doc.Data = append(doc.Data, render.Machine(req.account.ID, page.Items[i]))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowMachine implements GET …/machines/{id}.
func (a *API) ShowMachine(ctx context.Context, params oas.ShowMachineParams) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.Show(ctx, req.account, params.MachineId)
	})
}

// CreateMachine implements POST …/machines (an activation).
func (a *API) CreateMachine(
	ctx context.Context, _ oas.CreateMachineReq, _ oas.CreateMachineParams,
) (*oas.MachineDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	in, err := validateMachineCreateBody(req)
	if err != nil {
		return nil, err
	}

	machine, err := a.machines.Create(ctx, req.account, in)
	if err != nil {
		return nil, err
	}

	return &oas.MachineDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" + req.account.ID.String() + "/machines/" +
			machine.ID.String())),
		Response: machineDoc(req, machine),
	}, nil
}

// UpdateMachine implements PATCH …/machines/{id}.
func (a *API) UpdateMachine(
	ctx context.Context, _ oas.UpdateMachineReq, params oas.UpdateMachineParams,
) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.Update(ctx, req.account, params.MachineId, func() (machines.Input, error) {
			return validateMachineUpdateBody(req)
		})
	})
}

// DeleteMachine implements DELETE …/machines/{id} (a deactivation).
func (a *API) DeleteMachine(ctx context.Context, params oas.DeleteMachineParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.machines.Destroy(ctx, req.account, params.MachineId)
}

// PingMachine implements POST …/machines/{id}/actions/ping.
func (a *API) PingMachine(ctx context.Context, params oas.PingMachineParams) (*oas.MachineDocument, error) {
	return a.ping(ctx, params.MachineId)
}

// PingMachineHeartbeat implements POST …/machines/{id}/actions/ping-heartbeat.
func (a *API) PingMachineHeartbeat(
	ctx context.Context, _ oas.PingMachineHeartbeatReq, params oas.PingMachineHeartbeatParams,
) (*oas.MachineDocument, error) {
	return a.ping(ctx, params.MachineId)
}

func (a *API) ping(ctx context.Context, id string) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.Ping(ctx, req.account, id)
	})
}

// ResetMachine implements POST …/machines/{id}/actions/reset.
func (a *API) ResetMachine(ctx context.Context, params oas.ResetMachineParams) (*oas.MachineDocument, error) {
	return a.reset(ctx, params.MachineId)
}

// ResetMachineHeartbeat implements POST …/machines/{id}/actions/reset-heartbeat.
func (a *API) ResetMachineHeartbeat(
	ctx context.Context, _ oas.ResetMachineHeartbeatReq, params oas.ResetMachineHeartbeatParams,
) (*oas.MachineDocument, error) {
	return a.reset(ctx, params.MachineId)
}

func (a *API) reset(ctx context.Context, id string) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.Reset(ctx, req.account, id)
	})
}

// CheckoutMachineRaw implements GET …/machines/{id}/actions/check-out
// (the certificate itself).
func (a *API) CheckoutMachineRaw(
	ctx context.Context, params oas.CheckoutMachineRawParams,
) (*oas.CheckoutMachineRawOKHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	checkout, cert, err := a.machineCheckout(ctx, req, params.MachineId, func() (machines.CheckoutParams, error) {
		return machines.CheckoutParams(checkoutQueryParams(req)), nil
	})
	if err != nil {
		return nil, err
	}

	return &oas.CheckoutMachineRawOKHeaders{
		ContentDisposition: oas.NewOptString("attachment; filename=\"" + checkout.Machine.ID.String() + ".lic\""),
		Response:           oas.CheckoutMachineRawOK{Data: strings.NewReader(cert)},
	}, nil
}

// CheckoutMachine implements POST …/machines/{id}/actions/check-out.
func (a *API) CheckoutMachine(
	ctx context.Context, _ oas.CheckoutMachineReq, params oas.CheckoutMachineParams,
) (*oas.MachineFileDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	checkout, cert, err := a.machineCheckout(ctx, req, params.MachineId, func() (machines.CheckoutParams, error) {
		meta, err := validateCheckoutMeta(req)
		if err != nil {
			return machines.CheckoutParams{}, err
		}

		return machines.CheckoutParams(mergeCheckoutParams(checkoutQueryParams(req), meta)), nil
	})
	if err != nil {
		return nil, err
	}

	return &oas.MachineFileDocument{Data: render.MachineFile(
		req.account.ID, uuid.New(), cert, checkout.Algorithm, checkout.Includes,
		checkout.TTL, checkout.IssuedAt, checkout.Expiry, checkout.License.ID, checkout.Machine.ID,
	)}, nil
}

// machineCheckout runs the scenario and builds the certificate.
func (a *API) machineCheckout(
	ctx context.Context, req *request, id string, decode func() (machines.CheckoutParams, error),
) (machines.Checkout, string, error) {
	checkout, err := a.machines.Checkout(ctx, req.account, id, decode)
	if err != nil {
		return checkout, "", err
	}

	payload, err := render.MachineCheckoutPayload(req.account.ID, checkout, a.licenseNow())
	if err != nil {
		return checkout, "", fmt.Errorf("httpapi: machine file payload: %w", err)
	}

	cert, err := crypto.BuildFileCertificate(crypto.FileCertificateInput{
		Keys: req.account.Keys, Payload: payload, Secret: checkout.License.Key + checkout.Machine.Fingerprint,
		Algorithm: checkout.Algorithm, RNG: rand.Reader,
		Prefix: "machine/", Begin: "-----BEGIN MACHINE FILE-----", End: "-----END MACHINE FILE-----",
	})
	if err != nil {
		return checkout, "", fmt.Errorf("httpapi: machine file certificate: %w", err)
	}

	return checkout, cert, nil
}

// GenerateMachineProof implements POST …/machines/{id}/actions/generate-offline-proof.
func (a *API) GenerateMachineProof(
	ctx context.Context, _ oas.GenerateMachineProofReq, params oas.GenerateMachineProofParams,
) (*oas.MachineProofDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	machine, proof, err := a.machines.Proof(ctx, req.account, params.MachineId, func() (map[string]any, error) {
		return validateProofBody(req)
	})
	if err != nil {
		return nil, err
	}

	return &oas.MachineProofDocument{
		Data: render.Machine(req.account.ID, machine),
		Meta: oas.MachineProofDocumentMeta{Proof: proof},
	}, nil
}

// ShowMachineProduct implements GET …/machines/{id}/product.
func (a *API) ShowMachineProduct(
	ctx context.Context, params oas.ShowMachineProductParams,
) (*oas.ProductDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	product, err := a.machines.Product(ctx, req.account, params.MachineId)
	if err != nil {
		return nil, err
	}

	return &oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)}, nil
}

// ShowMachineLicense implements GET …/machines/{id}/license.
func (a *API) ShowMachineLicense(
	ctx context.Context, params oas.ShowMachineLicenseParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	license, err := a.machines.License(ctx, req.account, params.MachineId)
	if err != nil {
		return nil, err
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// ShowMachineOwner implements GET …/machines/{id}/owner.
func (a *API) ShowMachineOwner(
	ctx context.Context, params oas.ShowMachineOwnerParams,
) (*oas.LicenseOwnerDocument, error) {
	return a.machineUser(ctx, params.MachineId, a.machines.Owner)
}

// ShowMachineUserAlias implements GET …/machines/{id}/user (the v1.5
// "user": the license's owner).
func (a *API) ShowMachineUserAlias(
	ctx context.Context, params oas.ShowMachineUserAliasParams,
) (*oas.LicenseOwnerDocument, error) {
	return a.machineUser(ctx, params.MachineId, a.machines.LicenseOwner)
}

func (a *API) machineUser(
	ctx context.Context, id string,
	read func(context.Context, domain.Account, string) (domain.User, string, bool, error),
) (*oas.LicenseOwnerDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	owner, status, present, err := read(ctx, req.account, id)
	if err != nil {
		return nil, err
	}

	if !present {
		return &oas.LicenseOwnerDocument{Data: oas.NilUserResource{Null: true}}, nil
	}

	return &oas.LicenseOwnerDocument{Data: oas.NilUserResource{Value: render.User(req.account.ID, owner, status)}}, nil
}

// UpdateMachineOwner implements PATCH (and PUT) …/machines/{id}/owner.
func (a *API) UpdateMachineOwner(
	ctx context.Context, _ oas.UpdateMachineOwnerReq, params oas.UpdateMachineOwnerParams,
) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.SetOwner(ctx, req.account, params.MachineId, relationshipDecoder(req, "owner"))
	})
}

// ShowMachineGroup implements GET …/machines/{id}/group.
func (a *API) ShowMachineGroup(ctx context.Context, params oas.ShowMachineGroupParams) (*oas.GroupDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	group, err := a.machines.Group(ctx, req.account, params.MachineId)
	if err != nil {
		return nil, err
	}

	return &oas.GroupDocument{Data: oas.NilGroupResource{Value: render.Group(group)}}, nil
}

// UpdateMachineGroup implements PATCH (and PUT) …/machines/{id}/group.
func (a *API) UpdateMachineGroup(
	ctx context.Context, _ oas.UpdateMachineGroupReq, params oas.UpdateMachineGroupParams,
) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.SetGroup(ctx, req.account, params.MachineId, relationshipDecoder(req, "group"))
	})
}

// relationshipDecoder reads a to-one linkage body (null clears it).
func relationshipDecoder(req *request, kind string) func() (uuid.NullUUID, error) {
	return func() (uuid.NullUUID, error) {
		link, isNull, err := validateRelationshipPatchBody(req, kind)

		return groupLinkOf(link, isNull), err
	}
}

// machineDocument runs a single-machine route.
func (a *API) machineDocument(
	ctx context.Context, run func(req *request) (domain.Machine, error),
) (*oas.MachineDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	machine, err := run(req)
	if err != nil {
		return nil, err
	}

	doc := machineDoc(req, machine)

	return &doc, nil
}

func machineDoc(req *request, machine domain.Machine) oas.MachineDocument {
	return oas.MachineDocument{Data: oas.NilMachineResource{Value: render.Machine(req.account.ID, machine)}}
}

// machinesFilter reads the index filters (the reference's has_scope).
func machinesFilter(req *request) machines.Filter {
	query := req.query

	f := machines.Filter{
		Fingerprint: query.Get("fingerprint"), IP: query.Get("ip"), Hostname: query.Get("hostname"),
		ProductID: queryUUID(query.Get("product")), PolicyID: queryUUID(query.Get("policy")),
		LicenseID: queryUUID(query.Get("license")), Key: query.Get("key"), Owner: query.Get("owner"),
		User: query.Get("user"), GroupID: queryUUID(query.Get("group")), Metadata: metadataFilter(query),
	}

	if status := query.Get("status"); status != "" {
		f.Status = strings.ToUpper(status)
	}

	return f
}

// machineWriter answers whether the bearer may write the privileged
// update attributes (cores, memory, disk, metadata).
func machineWriter(req *request) bool {
	if req.principal.IsProduct() {
		return true
	}

	switch req.principal.Role {
	case domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent:
		return true
	default:
		return false
	}
}

// machineGroupWriter answers whether the bearer may set the group on
// activation (and send a proof dataset).
func machineGroupWriter(req *request) bool {
	return machineWriter(req) || req.principal.Role == domain.RoleSupportRole
}

// validateMachineCreateBody decodes POST …/machines.
func validateMachineCreateBody(req *request) (machines.Input, error) {
	var in machines.Input

	data, err := machineData(req, "type", "id", "attributes", "relationships")
	if err != nil {
		return in, err
	}

	if raw, ok := data["id"]; ok && raw != nil {
		value, err := str(raw, "/data/id")
		if err != nil {
			return in, err
		}

		if _, err := uuid.Parse(value); err != nil {
			return in, apperrTypeMismatchUUID("/data/id")
		}

		in.ID = value
	}

	attrs, err := hash(data[keyAttributes], "/data/attributes")
	if err != nil {
		return in, err
	}

	if err := unpermitted(attrs, "/data/attributes/",
		keyFingerprint, keyName, "ip", "hostname", "platform", "cores", "memory", "disk", "metadata"); err != nil {
		return in, err
	}

	fingerprint, _, err := stringAttr(attrs, keyFingerprint, true)
	if err != nil {
		return in, err
	}

	if fingerprint != nil {
		in.Fingerprint = *fingerprint
	}

	if err := machineAttributes(attrs, &in, true); err != nil {
		return in, err
	}

	rels, err := hash(data["relationships"], "/data/relationships")
	if err != nil {
		return in, err
	}

	err = machineRelationships(req, rels, &in)

	return in, err
}

// validateMachineUpdateBody decodes PATCH …/machines/{id}.
func validateMachineUpdateBody(req *request) (machines.Input, error) {
	var in machines.Input

	data, err := machineData(req, "type", "id", "attributes")
	if err != nil {
		return in, err
	}

	if raw, ok := data["id"]; ok && raw != nil {
		if _, err := str(raw, "/data/id"); err != nil {
			return in, err
		}
	}

	attrsRaw, ok := data[keyAttributes]
	if !ok || attrsRaw == nil {
		return in, apperr.BadRequestSource("is missing", "/data/attributes")
	}

	attrs, err := hash(attrsRaw, "/data/attributes")
	if err != nil {
		return in, err
	}

	allowed := []string{keyName, "ip", "hostname", "platform"}
	if machineWriter(req) {
		allowed = append(allowed, "cores", "memory", "disk", keyMetadata)
	}

	if err := unpermitted(attrs, "/data/attributes/", allowed...); err != nil {
		return in, err
	}

	err = machineAttributes(attrs, &in, false)

	return in, err
}

func apperrTypeMismatchUUID(pointer string) error {
	return apperr.BadRequestSource("type mismatch (received string expected UUID string)", pointer)
}

// machineData reads the body's data object with its permitted keys.
func machineData(req *request, allowed ...string) (map[string]any, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return nil, err
	}

	data, err := hash(body["data"], "/data")
	if err != nil {
		return nil, err
	}

	if err := unpermitted(data, "/data/", allowed...); err != nil {
		return nil, err
	}

	return data, requireType(data, typeMachineSingular, typeMachinePlural)
}

// machineAttributes reads the optional attributes.
func machineAttributes(attrs map[string]any, in *machines.Input, create bool) error {
	for _, text := range []struct {
		name  string
		field *machines.Field[string]
	}{
		{keyName, &in.Name}, {"ip", &in.IP}, {"hostname", &in.Hostname}, {"platform", &in.Platform},
	} {
		value, set, err := nullableStringAttr(attrs, text.name)
		if err != nil {
			return err
		}

		*text.field = machines.Field[string]{Set: set, Value: value}
	}

	for _, number := range []struct {
		name  string
		field *machines.Field[int64]
	}{
		{"cores", &in.Cores}, {"memory", &in.Memory}, {"disk", &in.Disk},
	} {
		value, set, err := bigintAttr(attrs, number.name)
		if err != nil {
			return err
		}

		*number.field = machines.Field[int64]{Set: set, Value: value}
	}

	if _, ok := attrs[keyMetadata]; ok || create {
		meta, set, err := metadataAttr(attrs)
		if err != nil {
			return err
		}

		in.Metadata = machines.Field[map[string]any]{Set: set}
		if meta != nil {
			in.Metadata.Value = &meta
		}
	}

	return nil
}

// machineRelationships reads the activation relationships: the license
// (required), the owner, the group (privileged) and nested components.
func machineRelationships(req *request, rels map[string]any, in *machines.Input) error {
	allowed := []string{typeLicenseLinkSingle, keyOwner, typeComponentPlural}
	if machineGroupWriter(req) {
		allowed = append(allowed, "group")
	}

	if err := unpermitted(rels, "/data/relationships/", allowed...); err != nil {
		return err
	}

	license, err := hash(rels["license"], "/data/relationships/license")
	if err != nil {
		return err
	}

	if err := unpermitted(license, "/data/relationships/license/", "data"); err != nil {
		return err
	}

	in.LicenseID, err = typedLinkage(license["data"], "/data/relationships/license/data", typeLicenseLinkSingle,
		typeLicensePlural)
	if err != nil {
		return err
	}

	if raw, ok := rels[keyOwner]; ok && raw != nil {
		if in.Owner, _, err = userLinkage(raw, keyOwner); err != nil {
			return err
		}
	}

	if raw, ok := rels["group"]; ok && raw != nil {
		if in.Group, err = groupLinkage(raw); err != nil {
			return err
		}
	}

	if raw, ok := rels[typeComponentPlural]; ok && raw != nil {
		if in.Components, err = componentsLinkage(raw); err != nil {
			return err
		}
	}

	return nil
}

// componentsLinkage reads the nested components of an activation.
func componentsLinkage(raw any) ([]machines.ComponentInput, error) {
	rel, err := hash(raw, "/data/relationships/components")
	if err != nil {
		return nil, err
	}

	if err := unpermitted(rel, "/data/relationships/components/", "data"); err != nil {
		return nil, err
	}

	list, ok := rel["data"].([]any)
	if !ok {
		return nil, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(rel["data"])+" expected array)", "/data/relationships/components/data")
	}

	out := make([]machines.ComponentInput, 0, len(list))

	for index, item := range list {
		component, err := componentItem(item, "/data/relationships/components/data/"+itoa(index))
		if err != nil {
			return nil, err
		}

		out = append(out, component)
	}

	return out, nil
}

func componentItem(entry any, pointer string) (machines.ComponentInput, error) {
	var out machines.ComponentInput

	item, err := hash(entry, pointer)
	if err != nil {
		return out, err
	}

	if err := unpermitted(item, pointer+"/", "type", "attributes"); err != nil {
		return out, err
	}

	typ, err := str(item["type"], pointer+"/type")
	if err != nil {
		return out, err
	}

	if typ != typeComponentSingular && typ != typeComponentPlural {
		return out, apperr.BadRequestSource("is invalid", pointer+"/type")
	}

	attrs, err := hash(item[keyAttributes], pointer+"/attributes")
	if err != nil {
		return out, err
	}

	if err := unpermitted(attrs, pointer+"/attributes/", "fingerprint", "name", "metadata"); err != nil {
		return out, err
	}

	for _, field := range []struct {
		name string
		dst  *string
	}{{keyFingerprint, &out.Fingerprint}, {keyName, &out.Name}} {
		fieldPointer := pointer + "/attributes/" + field.name

		raw, ok := attrs[field.name]
		if !ok {
			return out, apperr.BadRequestSource("is missing", fieldPointer)
		}

		if *field.dst, err = str(raw, fieldPointer); err != nil {
			return out, err
		}

		if *field.dst == "" {
			return out, apperr.BadRequestSource("cannot be blank", fieldPointer)
		}
	}

	if raw, ok := attrs[keyMetadata]; ok && raw != nil {
		if out.Metadata, err = metadataOf(raw, pointer+"/attributes/metadata"); err != nil {
			return out, err
		}
	}

	return out, nil
}

// validateProofBody reads the optional proof dataset (privileged).
func validateProofBody(req *request) (map[string]any, error) {
	// absent is the nil dataset: the scenario then signs the default one.
	var absent map[string]any

	body := req.body
	if body == nil {
		return absent, nil
	}

	allowed := []string{}
	if machineGroupWriter(req) {
		allowed = append(allowed, "meta")
	}

	if err := unpermitted(body, "/", allowed...); err != nil {
		return nil, err
	}

	raw, ok := body["meta"]
	if !ok || raw == nil {
		return absent, nil
	}

	meta, err := hash(raw, "/meta")
	if err != nil {
		return nil, err
	}

	if err := unpermitted(meta, "/meta/", "dataset"); err != nil {
		return nil, err
	}

	return hash(meta["dataset"], "/meta/dataset")
}
