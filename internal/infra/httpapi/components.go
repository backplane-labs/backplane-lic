package httpapi

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/machines"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

const (
	typeProcessSingular = "process"
	typeProcessPlural   = "processes"
)

// IndexComponents implements GET …/components.
func (a *API) IndexComponents(ctx context.Context, _ oas.IndexComponentsParams) (*oas.ComponentListDocument, error) {
	return a.componentList(ctx, func(req *request, params paging.Params) (machines.ComponentList, error) {
		return a.machines.IndexComponents(ctx, req.account, subFilter(req), params)
	})
}

// IndexMachineComponents implements GET …/machines/{id}/components.
func (a *API) IndexMachineComponents(
	ctx context.Context, params oas.IndexMachineComponentsParams,
) (*oas.ComponentListDocument, error) {
	return a.componentList(ctx, func(req *request, list paging.Params) (machines.ComponentList, error) {
		return a.machines.MachineComponents(ctx, req.account, params.MachineId, list)
	})
}

// listPage is the index prologue: the environment guard, the paging
// parameters and the scenario call.
func listPage[L any](
	ctx context.Context, run func(*request, paging.Params) (L, error),
) (*request, paging.Params, L, error) {
	var none L

	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, paging.Params{}, none, err
	}

	params, err := listParams(req)
	if err != nil {
		return nil, paging.Params{}, none, err
	}

	page, err := run(req, params)

	return req, params, page, err
}

func (a *API) componentList(
	ctx context.Context, run func(*request, paging.Params) (machines.ComponentList, error),
) (*oas.ComponentListDocument, error) {
	req, params, page, err := listPage(ctx, run)
	if err != nil {
		return nil, err
	}

	doc := &oas.ComponentListDocument{Data: make([]oas.ComponentResource, 0, len(page.Items))}
	for i := range page.Items {
		doc.Data = append(doc.Data, render.Component(req.account.ID, page.Items[i]))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowComponent implements GET …/components/{id}.
func (a *API) ShowComponent(ctx context.Context, params oas.ShowComponentParams) (*oas.ComponentDocument, error) {
	return a.componentDocument(ctx, func(req *request) (domain.MachineComponent, error) {
		return a.machines.ShowComponent(ctx, req.account, params.ComponentId)
	})
}

// ShowMachineComponent implements GET …/machines/{id}/components/{componentId}.
func (a *API) ShowMachineComponent(
	ctx context.Context, params oas.ShowMachineComponentParams,
) (*oas.ComponentDocument, error) {
	return a.componentDocument(ctx, func(req *request) (domain.MachineComponent, error) {
		return a.machines.MachineComponent(ctx, req.account, params.MachineId, params.ComponentId)
	})
}

// CreateComponent implements POST …/components.
func (a *API) CreateComponent(
	ctx context.Context, _ oas.CreateComponentReq, _ oas.CreateComponentParams,
) (*oas.ComponentDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	data, machineID, err := subresourceCreateBody(req, typeComponentSingular, typeComponentPlural, false)
	if err != nil {
		return nil, err
	}

	in, err := componentAttributes(data)
	if err != nil {
		return nil, err
	}

	component, err := a.machines.CreateComponent(ctx, req.account, machineID, in)
	if err != nil {
		return nil, err
	}

	return &oas.ComponentDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" + req.account.ID.String() + "/components/" +
			component.ID.String())),
		Response: componentDoc(req, component),
	}, nil
}

// UpdateComponent implements PATCH …/components/{id}.
func (a *API) UpdateComponent(
	ctx context.Context, _ oas.UpdateComponentReq, params oas.UpdateComponentParams,
) (*oas.ComponentDocument, error) {
	return a.componentDocument(ctx, func(req *request) (domain.MachineComponent, error) {
		return a.machines.UpdateComponent(ctx, req.account, params.ComponentId, func() (machines.ComponentUpdate, error) {
			allowed := []string{keyName}
			if machineGroupWriter(req) {
				allowed = append(allowed, "metadata")
			}

			attrs, err := subresourceUpdateAttributes(req, typeComponentSingular, typeComponentPlural, allowed)
			if err != nil {
				return machines.ComponentUpdate{}, err
			}

			var in machines.ComponentUpdate

			name, set, err := stringAttr(attrs, keyName, false)
			in.Name = machines.Field[string]{Set: set, Value: name}

			if err != nil {
				return in, err
			}

			in.Metadata, err = metadataField(attrs)

			return in, err
		})
	})
}

// DeleteComponent implements DELETE …/components/{id}.
func (a *API) DeleteComponent(ctx context.Context, params oas.DeleteComponentParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.machines.DestroyComponent(ctx, req.account, params.ComponentId)
}

// ShowComponentMachine implements GET …/components/{id}/machine.
func (a *API) ShowComponentMachine(
	ctx context.Context, params oas.ShowComponentMachineParams,
) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.ComponentMachine(ctx, req.account, params.ComponentId, "machine.read")
	})
}

// ShowComponentLicense implements GET …/components/{id}/license.
func (a *API) ShowComponentLicense(
	ctx context.Context, params oas.ShowComponentLicenseParams,
) (*oas.LicenseDocument, error) {
	return a.subresourceLicense(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.ComponentMachine(ctx, req.account, params.ComponentId, "license.read")
	})
}

// ShowComponentProduct implements GET …/components/{id}/product.
func (a *API) ShowComponentProduct(
	ctx context.Context, params oas.ShowComponentProductParams,
) (*oas.ProductDocument, error) {
	return a.subresourceProduct(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.ComponentMachine(ctx, req.account, params.ComponentId, "product.read")
	})
}

func (a *API) componentDocument(
	ctx context.Context, run func(*request) (domain.MachineComponent, error),
) (*oas.ComponentDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	component, err := run(req)
	if err != nil {
		return nil, err
	}

	doc := componentDoc(req, component)

	return &doc, nil
}

func componentDoc(req *request, component domain.MachineComponent) oas.ComponentDocument {
	return oas.ComponentDocument{Data: oas.NilComponentResource{Value: render.Component(req.account.ID, component)}}
}

// IndexProcesses implements GET …/processes.
func (a *API) IndexProcesses(ctx context.Context, _ oas.IndexProcessesParams) (*oas.ProcessListDocument, error) {
	return a.processList(ctx, func(req *request, params paging.Params) (machines.ProcessList, error) {
		return a.machines.IndexProcesses(ctx, req.account, subFilter(req), params)
	})
}

// IndexMachineProcesses implements GET …/machines/{id}/processes.
func (a *API) IndexMachineProcesses(
	ctx context.Context, params oas.IndexMachineProcessesParams,
) (*oas.ProcessListDocument, error) {
	return a.processList(ctx, func(req *request, list paging.Params) (machines.ProcessList, error) {
		return a.machines.MachineProcesses(ctx, req.account, params.MachineId,
			machines.SubFilter{Status: subFilter(req).Status}, list)
	})
}

func (a *API) processList(
	ctx context.Context, run func(*request, paging.Params) (machines.ProcessList, error),
) (*oas.ProcessListDocument, error) {
	req, params, page, err := listPage(ctx, run)
	if err != nil {
		return nil, err
	}

	doc := &oas.ProcessListDocument{Data: make([]oas.ProcessResource, 0, len(page.Items))}
	for i := range page.Items {
		doc.Data = append(doc.Data, render.Process(req.account.ID, page.Items[i]))
	}

	doc.Links = render.Links(linksDoc(req, params, page.Count, page.Next))

	return doc, nil
}

// ShowProcess implements GET …/processes/{id}.
func (a *API) ShowProcess(ctx context.Context, params oas.ShowProcessParams) (*oas.ProcessDocument, error) {
	return a.processDocument(ctx, func(req *request) (domain.MachineProcess, error) {
		return a.machines.ShowProcess(ctx, req.account, params.ProcessId)
	})
}

// ShowMachineProcess implements GET …/machines/{id}/processes/{processId}.
func (a *API) ShowMachineProcess(
	ctx context.Context, params oas.ShowMachineProcessParams,
) (*oas.ProcessDocument, error) {
	return a.processDocument(ctx, func(req *request) (domain.MachineProcess, error) {
		return a.machines.MachineProcess(ctx, req.account, params.MachineId, params.ProcessId)
	})
}

// CreateProcess implements POST …/processes (a spawn).
func (a *API) CreateProcess(
	ctx context.Context, _ oas.CreateProcessReq, _ oas.CreateProcessParams,
) (*oas.ProcessDocumentHeaders, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	data, machineID, err := subresourceCreateBody(req, typeProcessSingular, typeProcessPlural, true)
	if err != nil {
		return nil, err
	}

	in, err := processAttributes(data)
	if err != nil {
		return nil, err
	}

	process, err := a.machines.SpawnProcess(ctx, req.account, machineID, in)
	if err != nil {
		return nil, err
	}

	return &oas.ProcessDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" + req.account.ID.String() + "/processes/" +
			process.ID.String())),
		Response: processDoc(req, process),
	}, nil
}

// UpdateProcess implements PATCH …/processes/{id}.
func (a *API) UpdateProcess(
	ctx context.Context, _ oas.UpdateProcessReq, params oas.UpdateProcessParams,
) (*oas.ProcessDocument, error) {
	return a.processDocument(ctx, func(req *request) (domain.MachineProcess, error) {
		return a.machines.UpdateProcess(ctx, req.account, params.ProcessId, func() (machines.Field[map[string]any], error) {
			var allowed []string
			if machineGroupWriter(req) {
				allowed = append(allowed, "metadata")
			}

			attrs, err := subresourceUpdateAttributes(req, typeProcessSingular, typeProcessPlural, allowed)
			if err != nil {
				return machines.Field[map[string]any]{}, err
			}

			return metadataField(attrs)
		})
	})
}

// DeleteProcess implements DELETE …/processes/{id} (a kill).
func (a *API) DeleteProcess(ctx context.Context, params oas.DeleteProcessParams) error {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return err
	}

	return a.machines.KillProcess(ctx, req.account, params.ProcessId)
}

// PingProcess implements POST …/processes/{id}/actions/ping.
func (a *API) PingProcess(
	ctx context.Context, _ oas.PingProcessReq, params oas.PingProcessParams,
) (*oas.ProcessDocument, error) {
	return a.processDocument(ctx, func(req *request) (domain.MachineProcess, error) {
		return a.machines.PingProcess(ctx, req.account, params.ProcessId)
	})
}

// ShowProcessMachine implements GET …/processes/{id}/machine.
func (a *API) ShowProcessMachine(
	ctx context.Context, params oas.ShowProcessMachineParams,
) (*oas.MachineDocument, error) {
	return a.machineDocument(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.ProcessMachine(ctx, req.account, params.ProcessId, "machine.read")
	})
}

// ShowProcessLicense implements GET …/processes/{id}/license.
func (a *API) ShowProcessLicense(
	ctx context.Context, params oas.ShowProcessLicenseParams,
) (*oas.LicenseDocument, error) {
	return a.subresourceLicense(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.ProcessMachine(ctx, req.account, params.ProcessId, "license.read")
	})
}

// ShowProcessProduct implements GET …/processes/{id}/product.
func (a *API) ShowProcessProduct(
	ctx context.Context, params oas.ShowProcessProductParams,
) (*oas.ProductDocument, error) {
	return a.subresourceProduct(ctx, func(req *request) (domain.Machine, error) {
		return a.machines.ProcessMachine(ctx, req.account, params.ProcessId, "product.read")
	})
}

func (a *API) processDocument(
	ctx context.Context, run func(*request) (domain.MachineProcess, error),
) (*oas.ProcessDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	process, err := run(req)
	if err != nil {
		return nil, err
	}

	doc := processDoc(req, process)

	return &doc, nil
}

func processDoc(req *request, process domain.MachineProcess) oas.ProcessDocument {
	return oas.ProcessDocument{Data: oas.NilProcessResource{Value: render.Process(req.account.ID, process)}}
}

// subresourceLicense renders the license of a sub-resource's machine.
func (a *API) subresourceLicense(
	ctx context.Context, machineOf func(*request) (domain.Machine, error),
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	machine, err := machineOf(req)
	if err != nil {
		return nil, err
	}

	license, err := a.machines.License(ctx, req.account, machine.ID.String())
	if err != nil {
		return nil, err
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// subresourceProduct renders the product of a sub-resource's machine.
func (a *API) subresourceProduct(
	ctx context.Context, machineOf func(*request) (domain.Machine, error),
) (*oas.ProductDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	machine, err := machineOf(req)
	if err != nil {
		return nil, err
	}

	product, err := a.machines.Product(ctx, req.account, machine.ID.String())
	if err != nil {
		return nil, err
	}

	return &oas.ProductDocument{Data: render.ProductResource(req.account.ID, product)}, nil
}

// subFilter reads the component and process index filters.
func subFilter(req *request) machines.SubFilter {
	query := req.query

	f := machines.SubFilter{
		MachineID: queryUUID(query.Get("machine")), ProductID: queryUUID(query.Get("product")),
		LicenseID: queryUUID(query.Get("license")), OwnerID: queryUUID(query.Get("owner")), User: query.Get("user"),
	}

	if status := query.Get("status"); status != "" {
		f.Status = strings.ToUpper(status)
	}

	return f
}

// subresourceCreateBody reads a component or process create body: its
// data object and the machine relationship.
func subresourceCreateBody(req *request, singular, plural string, withID bool) (map[string]any, uuid.UUID, error) {
	allowed := []string{metaType, keyAttributes, "relationships"}
	if withID {
		allowed = append(allowed, "id")
	}

	data, err := subresourceData(req, singular, plural, allowed...)
	if err != nil {
		return nil, uuid.Nil, err
	}

	rels, err := hash(data["relationships"], "/data/relationships")
	if err != nil {
		return nil, uuid.Nil, err
	}

	if err := unpermitted(rels, "/data/relationships/", typeMachineSingular); err != nil {
		return nil, uuid.Nil, err
	}

	machine, err := hash(rels[typeMachineSingular], "/data/relationships/machine")
	if err != nil {
		return nil, uuid.Nil, err
	}

	if err := unpermitted(machine, "/data/relationships/machine/", "data"); err != nil {
		return nil, uuid.Nil, err
	}

	id, err := typedLinkage(machine["data"], "/data/relationships/machine/data", typeMachineSingular, typeMachinePlural)

	return data, id, err
}

func subresourceData(req *request, singular, plural string, allowed ...string) (map[string]any, error) {
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

	return data, requireType(data, singular, plural)
}

// subresourceUpdateAttributes reads an update body's attributes with the
// bearer's writable set.
func subresourceUpdateAttributes(req *request, singular, plural string, allowed []string) (map[string]any, error) {
	data, err := subresourceData(req, singular, plural, "type", "id", keyAttributes)
	if err != nil {
		return nil, err
	}

	attrs, err := hash(data[keyAttributes], "/data/attributes")
	if err != nil {
		return nil, err
	}

	return attrs, unpermitted(attrs, "/data/attributes/", allowed...)
}

func componentAttributes(data map[string]any) (machines.ComponentInput, error) {
	var in machines.ComponentInput

	attrs, err := hash(data[keyAttributes], "/data/attributes")
	if err != nil {
		return in, err
	}

	if err := unpermitted(attrs, "/data/attributes/", keyFingerprint, keyName, keyMetadata); err != nil {
		return in, err
	}

	for _, field := range []struct {
		name string
		dst  *string
	}{{keyFingerprint, &in.Fingerprint}, {keyName, &in.Name}} {
		value, _, err := stringAttr(attrs, field.name, true)
		if err != nil {
			return in, err
		}

		*field.dst = *value
	}

	meta, err := metadataField(attrs)
	if meta.Value != nil {
		in.Metadata = *meta.Value
	}

	return in, err
}

func processAttributes(data map[string]any) (machines.ProcessInput, error) {
	var in machines.ProcessInput

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

	if err := unpermitted(attrs, "/data/attributes/", "pid", keyMetadata); err != nil {
		return in, err
	}

	pid, _, err := stringAttr(attrs, "pid", true)
	if err != nil {
		return in, err
	}

	in.PID = *pid

	meta, err := metadataField(attrs)
	if meta.Value != nil {
		in.Metadata = *meta.Value
	}

	return in, err
}

func metadataField(attrs map[string]any) (machines.Field[map[string]any], error) {
	meta, set, err := metadataAttr(attrs)

	field := machines.Field[map[string]any]{Set: set}
	if meta != nil {
		field.Value = &meta
	}

	return field, err
}
