package httpapi

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// queryOf answers the request's original query (the account form the
// client used).
func queryOf(raw string) url.Values {
	if _, q, found := strings.Cut(raw, "?"); found {
		if values, err := url.ParseQuery(q); err == nil {
			return values
		}
	}

	return url.Values{}
}

// camelize lowerCamelCases an underscored body key, like the reference's
// error pointers.
func camelize(key string) string {
	parts := strings.Split(key, "_")

	var b strings.Builder
	b.WriteString(parts[0])

	for _, part := range parts[1:] {
		if part == "" {
			continue
		}

		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}

	return b.String()
}

// pointerOf builds a JSON pointer with camelized segments.
func pointerOf(segments ...string) string {
	out := make([]string, 0, len(segments))
	for _, segment := range segments {
		out = append(out, camelize(segment))
	}

	return "/" + strings.Join(out, "/")
}

// parsePaging parses the request's pagination (400 on bad values).
func parsePaging(req *request) (paging.Params, error) {
	p, err := paging.Parse(queryOf(req.originalURI))
	if err != nil {
		return p, err //nolint:wrapcheck // the paging package's own document
	}

	return p, nil
}

// userFilter builds the index filter from the query (the default
// roles[]=user applies when the parameter is absent).
func userFilter(query url.Values) users.Filter {
	f := users.Filter{Roles: defaultRoles(query)}

	f.Status = statusFilter(query.Get("status"))
	f.GroupID = queryUUID(query.Get("group"))
	f.ProductID = queryUUID(query.Get("product"))
	f.LicenseID = queryUUID(query.Get("license"))
	f.Assigned = queryBool(query, "assigned")

	if f.Assigned == nil {
		f.Assigned = queryBool(query, "active")
	}

	f.Metadata = metadataFilter(query)

	return f
}

func defaultRoles(query url.Values) []string {
	values := query["roles[]"]
	if len(values) == 0 {
		return []string{"user"}
	}

	out := make([]string, 0, len(values))
	for _, v := range values {
		if role, ok := domain.ParseRole(v); ok {
			out = append(out, string(role))
		}
	}

	return out
}

// statusFilter maps the status filter; unknown values match nothing.
// statusNoMatch is the status-filter sentinel for unknown values.
const statusNoMatch = "NONE"

func statusFilter(value string) string {
	switch strings.ToUpper(value) {
	case "ACTIVE", "INACTIVE", "BANNED":
		return strings.ToUpper(value)
	case "":
		return ""
	default:
		// Unknown values match nothing (the reference's none scope).
		return statusNoMatch
	}
}

func queryUUID(value string) uuid.NullUUID {
	if value == "" {
		return uuid.NullUUID{}
	}

	id, err := uuid.Parse(value)
	if err != nil {
		// An unparseable alias matches nothing.
		return uuid.NullUUID{UUID: uuid.Must(uuid.NewRandom()), Valid: true}
	}

	return uuid.NullUUID{UUID: id, Valid: true}
}

func queryBool(query url.Values, key string) *bool {
	raw := query.Get(key)
	if raw == "" {
		return nil
	}

	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return nil
	}

	return &parsed
}

// metadataFilter collects the metadata[k]=v containment filter with the
// reference's value type-cast.
func metadataFilter(query url.Values) map[string]any {
	out := map[string]any{}

	for key, values := range query {
		if !strings.HasPrefix(key, "metadata[") || !strings.HasSuffix(key, "]") {
			continue
		}

		if len(values) == 0 {
			continue
		}

		inner := key[len("metadata[") : len(key)-1]
		out[inner] = castMetaValue(values[0])
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func castMetaValue(v string) any {
	switch v {
	case "true":
		return true
	case "false":
		return false
	case "null":
		return nil
	}

	if i, err := strconv.Atoi(v); err == nil {
		return i
	}

	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f
	}

	return v
}

// userDoc renders a listed user.
func userDoc(row users.Listed) oas.UserDocument {
	return oas.UserDocument{Data: render.User(row.User.AccountID, row.User, row.Status)}
}

// listedUsers renders the page.
func listedUsers(req *request, page users.Page, p paging.Params) *oas.UserCollectionDocument {
	doc := &oas.UserCollectionDocument{
		Data:  make([]oas.UserResource, 0, len(page.Rows)),
		Links: render.Links(linksDoc(req, p, page.Total, uuid.NullUUID{})),
	}

	for i := range page.Rows {
		doc.Data = append(doc.Data, render.User(page.Rows[i].User.AccountID, page.Rows[i].User, page.Rows[i].Status))
	}

	return doc
}

// ListUsers implements GET /v1/accounts/{account}/users.
func (a *API) ListUsers(ctx context.Context, _ oas.ListUsersParams) (*oas.UserCollectionDocument, error) {
	req := requestOf(ctx)

	p, err := parsePaging(req)
	if err != nil {
		return nil, err
	}

	page, err := a.users.List(ctx, req.account, req.principal, userFilter(queryOf(req.originalURI)), p)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return listedUsers(req, page, p), nil
}

// CreateUser implements POST /v1/accounts/{account}/users.
func (a *API) CreateUser(
	ctx context.Context, _ oas.CreateUserReq, _ oas.CreateUserParams,
) (*oas.UserDocumentHeaders, error) {
	req := requestOf(ctx)

	attrs, err := validateUserBody(req, false)
	if err != nil {
		return nil, err
	}

	row, err := a.users.Create(ctx, req.account, req.principal, attrs)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.UserDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/users/" + row.User.ID.String())),
		Response: userDoc(row),
	}, nil
}

// ShowUser implements GET /v1/accounts/{account}/users/{userId}.
func (a *API) ShowUser(ctx context.Context, params oas.ShowUserParams) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	row, err := a.users.Show(ctx, req.account, req.principal, params.UserId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := userDoc(row)

	return &doc, nil
}

// UpdateUser implements PATCH /v1/accounts/{account}/users/{userId}.
func (a *API) UpdateUser(
	ctx context.Context, _ oas.UpdateUserReq, params oas.UpdateUserParams,
) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	attrs, err := validateUserBody(req, true)
	if err != nil {
		return nil, err
	}

	row, err := a.users.Update(ctx, req.account, req.principal, params.UserId, attrs)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := userDoc(row)

	return &doc, nil
}

// DeleteUser implements DELETE /v1/accounts/{account}/users/{userId}.
func (a *API) DeleteUser(ctx context.Context, params oas.DeleteUserParams) error {
	req := requestOf(ctx)

	return a.users.Delete(ctx, req.account, req.principal, params.UserId) //nolint:wrapcheck // the scenario's own error
}

// UpdateUserPassword implements the update-password action.
func (a *API) UpdateUserPassword(
	ctx context.Context, _ oas.UpdateUserPasswordReq, params oas.UpdateUserPasswordParams,
) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	oldPassword, newPassword, err := validatePasswordChange(req, "old_password", "new_password")
	if err != nil {
		return nil, err
	}

	row, err := a.users.UpdatePassword(ctx, req.account, req.principal, params.UserId, oldPassword, newPassword)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := userDoc(row)

	return &doc, nil
}

// ResetUserPassword implements the reset-password action.
func (a *API) ResetUserPassword(
	ctx context.Context, _ oas.ResetUserPasswordReq, params oas.ResetUserPasswordParams,
) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	token, newPassword, err := validatePasswordChange(req, "password_reset_token", "new_password")
	if err != nil {
		return nil, err
	}

	row, err := a.users.ResetPassword(ctx, req.account, req.principal, params.UserId, token, newPassword)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := userDoc(row)

	return &doc, nil
}

// BanUser implements the ban action.
func (a *API) BanUser(ctx context.Context, params oas.BanUserParams) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	row, err := a.users.Ban(ctx, req.account, req.principal, params.UserId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := userDoc(row)

	return &doc, nil
}

// UnbanUser implements the unban action.
func (a *API) UnbanUser(ctx context.Context, params oas.UnbanUserParams) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	row, err := a.users.Unban(ctx, req.account, req.principal, params.UserId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := userDoc(row)

	return &doc, nil
}

// ShowUserGroup implements GET /users/{userId}/group.
func (a *API) ShowUserGroup(ctx context.Context, params oas.ShowUserGroupParams) (*oas.GroupDocument, error) {
	req := requestOf(ctx)

	group, err := a.users.Group(ctx, req.account, req.principal, params.UserId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	if group == nil {
		return &oas.GroupDocument{Data: oas.NilGroupResource{Null: true}}, nil
	}

	return &oas.GroupDocument{Data: oas.NilGroupResource{Value: render.Group(*group)}}, nil
}

// UpdateUserGroup implements PUT /users/{userId}/group.
func (a *API) UpdateUserGroup(
	ctx context.Context, _ oas.UpdateUserGroupReq, params oas.UpdateUserGroupParams,
) (*oas.UserDocument, error) {
	req := requestOf(ctx)

	group, remove, err := validateGroupBody(req)
	if err != nil {
		return nil, err
	}

	_ = remove

	row, err := a.users.SetGroup(ctx, req.account, req.principal, params.UserId, group)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := userDoc(row)

	return &doc, nil
}

// ListUserProducts implements GET /users/{userId}/products.
func (a *API) ListUserProducts(
	ctx context.Context, params oas.ListUserProductsParams,
) (*oas.UserProductCollectionDocument, error) {
	req := requestOf(ctx)

	ids, err := a.users.Related(ctx, req.account, req.principal, params.UserId, "products")
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.UserProductCollectionDocument{Data: make([]oas.UserProductResource, 0, len(ids))}
	for _, id := range ids {
		doc.Data = append(doc.Data, render.ProductStub(req.account.ID, id))
	}

	return doc, nil
}

// ShowUserProduct implements GET /users/{userId}/products/{productId}.
func (a *API) ShowUserProduct(
	ctx context.Context, params oas.ShowUserProductParams,
) (*oas.UserProductDocument, error) {
	req := requestOf(ctx)

	id, err := a.users.RelatedShow(ctx, req.account, req.principal, params.UserId, "products", params.ProductId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.UserProductDocument{Data: render.ProductStub(req.account.ID, id)}, nil
}

// ListUserLicenses implements GET /users/{userId}/licenses.
func (a *API) ListUserLicenses(
	ctx context.Context, params oas.ListUserLicensesParams,
) (*oas.LicenseListDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	listParams, err := listParams(req)
	if err != nil {
		return nil, err
	}

	user, err := a.users.RelatedUser(ctx, req.account, req.principal, params.UserId, "licenses")
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	page, err := a.licenses.OfUser(ctx, req.account, user.ID, licenseFilter(req.query), listParams)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return a.licenseListDocument(req, page, listParams), nil
}

// ShowUserLicense implements GET /users/{userId}/licenses/{licenseId}.
func (a *API) ShowUserLicense(
	ctx context.Context, params oas.ShowUserLicenseParams,
) (*oas.LicenseDocument, error) {
	req := requestOf(ctx)

	if err := checkEnvironment(req); err != nil {
		return nil, err
	}

	user, err := a.users.RelatedUser(ctx, req.account, req.principal, params.UserId, "licenses")
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	license, err := a.licenses.OfUserShow(ctx, req.account, user.ID, params.LicenseId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.LicenseDocument{Data: a.licenseDocument(req, &license, nil)}, nil
}

// ListUserMachines implements GET /users/{userId}/machines.
func (a *API) ListUserMachines(
	ctx context.Context, params oas.ListUserMachinesParams,
) (*oas.UserMachineCollectionDocument, error) {
	req := requestOf(ctx)

	ids, err := a.users.Related(ctx, req.account, req.principal, params.UserId, "machines")
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.UserMachineCollectionDocument{Data: make([]oas.UserMachineResource, 0, len(ids))}
	for _, id := range ids {
		doc.Data = append(doc.Data, render.MachineStub(req.account.ID, id))
	}

	return doc, nil
}

// ShowUserMachine implements GET /users/{userId}/machines/{machineId}.
func (a *API) ShowUserMachine(
	ctx context.Context, params oas.ShowUserMachineParams,
) (*oas.UserMachineDocument, error) {
	req := requestOf(ctx)

	id, err := a.users.RelatedShow(ctx, req.account, req.principal, params.UserId, "machines", params.MachineId)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.UserMachineDocument{Data: render.MachineStub(req.account.ID, id)}, nil
}
