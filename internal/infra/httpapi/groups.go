package httpapi

import (
	"context"
	"strconv"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/groups"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// maxSubIndex is the window used when a sub-route's show resolves
// through its index (the collection's upper page bound).
const maxSubIndex = 100

// defaultLimit is the index default page size.
const defaultLimit = 10

// groupDoc renders one group.
func groupDoc(group domain.Group) oas.GroupDocument {
	return oas.GroupDocument{Data: oas.NilGroupResource{Value: render.Group(group)}}
}

// ListGroups implements GET /v1/accounts/{account}/groups.
func (a *API) ListGroups(ctx context.Context, _ oas.ListGroupsParams) (*oas.GroupCollectionDocument, error) {
	req := requestOf(ctx)

	p, err := parsePaging(req)
	if err != nil {
		return nil, err
	}

	page, err := a.groups.List(ctx, req.account, req.principal, p)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.GroupCollectionDocument{
		Data:  make([]oas.GroupResource, 0, len(page.Rows)),
		Links: render.Links(linksDoc(req, p, page.Total, uuid.NullUUID{})),
	}

	for i := range page.Rows {
		doc.Data = append(doc.Data, render.Group(page.Rows[i]))
	}

	return doc, nil
}

// CreateGroup implements POST /v1/accounts/{account}/groups.
func (a *API) CreateGroup(
	ctx context.Context, _ oas.CreateGroupReq, _ oas.CreateGroupParams,
) (*oas.GroupDocumentHeaders, error) {
	req := requestOf(ctx)

	attrs, err := validateGroupAttrs(req, false)
	if err != nil {
		return nil, err
	}

	group, err := a.groups.Create(ctx, req.account, req.principal, attrs)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.GroupDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/groups/" + group.ID.String())),
		Response: groupDoc(group),
	}, nil
}

// ShowGroup implements GET /v1/accounts/{account}/groups/{groupId}.
func (a *API) ShowGroup(ctx context.Context, params oas.ShowGroupParams) (*oas.GroupDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	group, err := a.groups.Show(ctx, req.account, req.principal, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := groupDoc(group)

	return &doc, nil
}

// UpdateGroup implements PATCH /v1/accounts/{account}/groups/{groupId}.
func (a *API) UpdateGroup(
	ctx context.Context, _ oas.UpdateGroupReq, params oas.UpdateGroupParams,
) (*oas.GroupDocument, error) {
	req := requestOf(ctx)

	attrs, err := validateGroupAttrs(req, true)
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	group, err := a.groups.Update(ctx, req.account, req.principal, id, attrs)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := groupDoc(group)

	return &doc, nil
}

// DeleteGroup implements DELETE /v1/accounts/{account}/groups/{groupId}.
func (a *API) DeleteGroup(ctx context.Context, params oas.DeleteGroupParams) error {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return apperr.NotFound("group", params.GroupId)
	}

	return a.groups.Delete(ctx, req.account, req.principal, id) //nolint:wrapcheck // the scenario's own error
}

// ListGroupUsers implements GET /groups/{groupId}/users.
func (a *API) ListGroupUsers(
	ctx context.Context, params oas.ListGroupUsersParams,
) (*oas.UserCollectionDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	rows, err := a.groups.Users(ctx, req.account, req.principal, id, limitOf(req))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.UserCollectionDocument{Data: make([]oas.UserResource, 0, len(rows))}
	for i := range rows {
		user := rows[i]

		member, status, err := a.users.Me(ctx, req.account.ID, user.ID)
		if err != nil {
			return nil, err //nolint:wrapcheck // the scenario's own error
		}

		doc.Data = append(doc.Data, render.User(req.account.ID, member, status))
	}

	return doc, nil
}

// ShowGroupUser implements GET /groups/{groupId}/users/{userId}.
func (a *API) ShowGroupUser(ctx context.Context, params oas.ShowGroupUserParams) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	groupID, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	rows, err := a.groups.Users(
		ctx, req.account, req.principal, groupID, maxSubIndex,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	alias := params.UserId

	for i := range rows {
		member := rows[i]
		if member.ID.String() == alias || member.Email == alias {
			row, err := a.users.Show(ctx, req.account, req.principal, alias)
			if err != nil {
				return nil, err //nolint:wrapcheck // the scenario's own error
			}

			doc := userDoc(row)

			return &doc, nil
		}
	}

	return nil, apperr.NotFound("user", alias)
}

// ListGroupLicenses implements GET /groups/{groupId}/licenses.
func (a *API) ListGroupLicenses(
	ctx context.Context, params oas.ListGroupLicensesParams,
) (*oas.UserLicenseCollectionDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	ids, err := a.groups.LicenseIDs(ctx, req.account, req.principal, id, limitOf(req))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.UserLicenseCollectionDocument{Data: make([]oas.UserLicenseResource, 0, len(ids))}
	for _, licenseID := range ids {
		doc.Data = append(doc.Data, render.LicenseStub(req.account.ID, licenseID))
	}

	return doc, nil
}

// ShowGroupLicense implements GET /groups/{groupId}/licenses/{licenseId}.
func (a *API) ShowGroupLicense(
	ctx context.Context, params oas.ShowGroupLicenseParams,
) (*oas.UserLicenseDocument, error) {
	req := requestOf(ctx)

	groupID, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	ids, err := a.groups.LicenseIDs(ctx, req.account, req.principal, groupID, maxSubIndex)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	for _, licenseID := range ids {
		if licenseID.String() == params.LicenseId {
			return &oas.UserLicenseDocument{Data: render.LicenseStub(req.account.ID, licenseID)}, nil
		}
	}

	return nil, apperr.NotFound("license", params.LicenseId)
}

// ListGroupMachines implements GET /groups/{groupId}/machines.
func (a *API) ListGroupMachines(
	ctx context.Context, params oas.ListGroupMachinesParams,
) (*oas.UserMachineCollectionDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	ids, err := a.groups.MachineIDs(ctx, req.account, req.principal, id, limitOf(req))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.UserMachineCollectionDocument{Data: make([]oas.UserMachineResource, 0, len(ids))}
	for _, machineID := range ids {
		doc.Data = append(doc.Data, render.MachineStub(req.account.ID, machineID))
	}

	return doc, nil
}

// ShowGroupMachine implements GET /groups/{groupId}/machines/{machineId}.
func (a *API) ShowGroupMachine(
	ctx context.Context, params oas.ShowGroupMachineParams,
) (*oas.UserMachineDocument, error) {
	req := requestOf(ctx)

	groupID, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	ids, err := a.groups.MachineIDs(ctx, req.account, req.principal, groupID, maxSubIndex)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	for _, machineID := range ids {
		if machineID.String() == params.MachineId {
			return &oas.UserMachineDocument{Data: render.MachineStub(req.account.ID, machineID)}, nil
		}
	}

	return nil, apperr.NotFound("machine", params.MachineId)
}

// ListGroupOwners implements GET /groups/{groupId}/owners.
func (a *API) ListGroupOwners(
	ctx context.Context, params oas.ListGroupOwnersParams,
) (*oas.GroupOwnerCollectionDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	owners, err := a.groups.Owners(ctx, req.account, req.principal, id, limitOf(req))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.GroupOwnerCollectionDocument{Data: make([]oas.GroupOwnerResource, 0, len(owners))}
	for i := range owners {
		owner := owners[i]
		doc.Data = append(doc.Data, render.GroupOwner(owner))
	}

	return doc, nil
}

// ShowGroupOwner implements GET /groups/{groupId}/owners/{ownerId}.
func (a *API) ShowGroupOwner(
	ctx context.Context, params oas.ShowGroupOwnerParams,
) (*oas.GroupOwnerDocument, error) {
	req := requestOf(ctx)

	groupID, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	ownerID, err := uuid.Parse(params.OwnerId)
	if err != nil {
		return nil, apperr.NotFound("owner", params.OwnerId)
	}

	owner, err := a.groups.Owner(ctx, req.account, req.principal, groupID, ownerID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.GroupOwnerDocument{Data: render.GroupOwner(owner)}, nil
}

// AttachGroupOwners implements POST /groups/{groupId}/owners.
func (a *API) AttachGroupOwners(
	ctx context.Context, _ oas.AttachGroupOwnersReq, params oas.AttachGroupOwnersParams,
) (*oas.GroupOwnerCollectionDocument, error) {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return nil, apperr.NotFound("group", params.GroupId)
	}

	userIDs, err := validateOwnerList(req)
	if err != nil {
		return nil, err
	}

	owners, err := a.groups.AttachOwners(ctx, req.account, req.principal, id, userIDs)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.GroupOwnerCollectionDocument{Data: make([]oas.GroupOwnerResource, 0, len(owners))}
	for i := range owners {
		owner := owners[i]
		doc.Data = append(doc.Data, render.GroupOwner(owner))
	}

	return doc, nil
}

// DetachGroupOwners implements DELETE /groups/{groupId}/owners.
func (a *API) DetachGroupOwners(
	ctx context.Context, _ oas.DetachGroupOwnersReq, params oas.DetachGroupOwnersParams,
) error {
	req := requestOf(ctx)

	id, err := uuid.Parse(params.GroupId)
	if err != nil {
		return apperr.NotFound("group", params.GroupId)
	}

	userIDs, err := validateOwnerList(req)
	if err != nil {
		return err
	}

	err = a.groups.DetachOwners(ctx, req.account, req.principal, id, userIDs)

	return err //nolint:wrapcheck // the scenario's own error
}

// limitOf answers the index window size (the limit mode default).
func limitOf(req *request) int {
	p, err := parsePaging(req)
	if err != nil {
		return defaultLimit
	}

	return p.SizeOrDefault()
}

// validateGroupAttrs decodes the group write body.
func validateGroupAttrs(req *request, update bool) (groups.Attrs, error) {
	var attrs groups.Attrs

	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return attrs, err
	}

	dataRaw, ok := body["data"]
	if !ok || dataRaw == nil {
		return attrs, apperr.BadRequestSource("is missing", "/data")
	}

	data, err := hash(dataRaw, "/data")
	if err != nil {
		return attrs, err
	}

	keys := []string{"type", "attributes"}
	if update {
		keys = append(keys, "id")
	}

	if err := unpermitted(data, "/data/", keys...); err != nil {
		return attrs, err
	}

	if err := groupDataHeader(data); err != nil {
		return attrs, err
	}

	attrsObj, hasAttrs, err := groupAttrsObject(data, update)
	if err != nil || !hasAttrs {
		return attrs, err
	}

	attrKeys := []string{"name", "max_users", "max_licenses", "max_machines", "metadata"}
	if err := unpermitted(attrsObj, "/data/attributes/", attrKeys...); err != nil {
		return attrs, err
	}

	if attrs.Name, err = nameAttr(attrsObj, update); err != nil {
		return attrs, err
	}

	if err := groupLimits(attrsObj, &attrs); err != nil {
		return attrs, err
	}

	attrs.Metadata, err = optionalMetadataAttr(attrsObj)

	return attrs, err
}

// groupDataHeader validates data.type.
func groupDataHeader(data map[string]any) error {
	typeStr, err := str(data["type"], pointerOf("data", "type"))
	if err != nil {
		return err
	}

	if typeStr != typeGroupSingular && typeStr != typeGroupPlural {
		return apperr.BadRequestSource("is invalid", pointerOf("data", "type"))
	}

	return nil
}

// groupAttrsObject answers the decoded attributes object (hasAttrs
// false when the update body carries none).
func groupAttrsObject(data map[string]any, update bool) (map[string]any, bool, error) {
	attrsRaw, hasAttrs := data["attributes"]
	if !hasAttrs {
		if !update {
			return nil, false, apperr.BadRequestSource("is missing", pointerOf("data", "attributes"))
		}

		return nil, false, nil
	}

	obj, err := hash(attrsRaw, pointerOf("data", "attributes"))

	return obj, true, err
}

// nameAttr decodes the required (create) or optional (update) name.
func nameAttr(obj map[string]any, update bool) (*string, error) {
	raw, present := obj["name"]
	if !present || raw == nil {
		if update {
			return nil, nil //nolint:nilnil // absent optional attribute
		}

		return nil, apperr.BadRequestSource("is missing", pointerOf("data", "attributes", "name"))
	}

	s, err := str(raw, pointerOf("data", "attributes", "name"))
	if err != nil {
		return nil, err
	}

	return &s, nil
}

// groupLimits decodes the three limit attributes.
func groupLimits(obj map[string]any, attrs *groups.Attrs) error {
	var err error

	if attrs.MaxUsers, err = optionalIntAttr(obj, "max_users"); err != nil {
		return err
	}

	if attrs.MaxLicenses, err = optionalIntAttr(obj, "max_licenses"); err != nil {
		return err
	}

	attrs.MaxMachines, err = optionalIntAttr(obj, "max_machines")

	return err
}

// optionalMetadataAttr decodes the optional nullable metadata attribute.
func optionalMetadataAttr(obj map[string]any) (map[string]any, error) {
	raw, present := obj["metadata"]
	if !present {
		return nil, nil //nolint:nilnil // absent optional attribute
	}

	if raw == nil {
		return map[string]any{}, nil
	}

	return hash(raw, pointerOf("data", "attributes", "metadata"))
}

// optionalIntAttr decodes an optional nullable integer attribute.
func optionalIntAttr(obj map[string]any, key string) (groups.OptionalInt, error) {
	var out groups.OptionalInt

	raw, present := obj[key]
	if !present {
		return out, nil
	}

	if raw == nil {
		return groups.OptionalInt{Set: true, Null: true}, nil
	}

	n, ok := raw.(float64)
	if !ok || n != float64(int(n)) {
		return out, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(raw)+" expected integer)",
			pointerOf("data", "attributes", key),
		)
	}

	return groups.OptionalInt{Set: true, Value: int(n)}, nil
}

// validateOwnerList decodes data:[{type,id},…] of the owners routes.
func validateOwnerList(req *request) ([]uuid.UUID, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return nil, err
	}

	dataRaw, ok := body["data"]
	if !ok {
		return nil, apperr.BadRequestSource("is missing", "/data")
	}

	list, ok := dataRaw.([]any)
	if !ok {
		return nil, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(dataRaw)+" expected array)", "/data",
		)
	}

	out := make([]uuid.UUID, 0, len(list))
	for index, item := range list {
		obj, err := hash(item, "/data/"+strconv.Itoa(index))
		if err != nil {
			return nil, err
		}

		if err := unpermitted(obj, "/data/"+strconv.Itoa(index)+"/", "type", "id"); err != nil {
			return nil, err
		}

		typeStr, err := str(obj["type"], "/data/"+strconv.Itoa(index)+"/type")
		if err != nil {
			return nil, err
		}

		if typeStr != "user" && typeStr != "users" {
			return nil, apperr.BadRequestSource("is invalid", "/data/"+strconv.Itoa(index)+"/type")
		}

		idStr, err := str(obj["id"], "/data/"+strconv.Itoa(index)+"/id")
		if err != nil {
			return nil, err
		}

		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, apperr.BadRequestSource(
				"type mismatch (received string expected UUID string)",
				"/data/"+strconv.Itoa(index)+"/id",
			)
		}

		out = append(out, id)
	}

	return out, nil
}
