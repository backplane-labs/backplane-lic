package httpapi

import (
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/users"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// The singular/plural wire spellings of the user and group types
// (typeUserSingular already lives in body.go).
const (
	typeUserPlural    = "users"
	typeGroupSingular = "group"
	typeGroupPlural   = "groups"
)

// validateUserBody shape-validates the (underscored) body of POST and
// PATCH /users like the reference's typed params, including the
// bearer-dependent permitted keys (role is admin-only; password and
// metadata are privileged; the group relationship is privileged).
func validateUserBody(req *request, update bool) (users.Attrs, error) {
	var attrs users.Attrs

	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return attrs, err
	}

	dataRaw, hasData := body["data"]
	if !hasData || dataRaw == nil {
		return attrs, apperr.BadRequestSource("is missing", "/data")
	}

	data, err := hash(dataRaw, "/data")
	if err != nil {
		return attrs, err
	}

	if err := userDataHeader(data, update); err != nil {
		return attrs, err
	}

	admin := req.principal.IsUser() && req.principal.Role == domain.RoleAdmin
	privileged := privilegedBearer(req)
	// The group relationship additionally admits sales and support.
	groupPrivileged := privileged ||
		(req.principal.IsUser() &&
			(req.principal.Role == domain.RoleSalesAgent || req.principal.Role == domain.RoleSupportRole))

	attrs, err = userAttributes(data, update, admin, privileged)
	if err != nil {
		return attrs, err
	}

	link, err := userRelationships(data, groupPrivileged)
	if err != nil {
		return attrs, err
	}

	attrs.GroupSet, attrs.Group = link.set, link.id

	return attrs, nil
}

// groupLink is the decoded group relationship.
type groupLink struct {
	set bool
	id  uuid.NullUUID
}

// userDataHeader validates data's permitted keys and its type.
func userDataHeader(data map[string]any, update bool) error {
	dataKeys := []string{"type", "attributes", "relationships"}
	if update {
		dataKeys = append(dataKeys, "id")
	}

	if err := unpermitted(data, "/data/", dataKeys...); err != nil {
		return err
	}

	typeRaw, hasType := data["type"]
	if !hasType {
		return apperr.BadRequestSource("is missing", pointerOf("data", "type"))
	}

	typeStr, err := str(typeRaw, pointerOf("data", "type"))
	if err != nil {
		return err
	}

	if typeStr != typeUserSingular && typeStr != typeUserPlural {
		return apperr.BadRequestSource("is invalid", pointerOf("data", "type"))
	}

	return nil
}

// userAttributes decodes (and gates) data.attributes.
func userAttributes(data map[string]any, update, admin, privileged bool) (users.Attrs, error) {
	var attrs users.Attrs

	attrKeys := []string{"first_name", "last_name", "email"}
	if privileged {
		attrKeys = append(attrKeys, "password", "metadata")
	}

	if admin {
		attrKeys = append(attrKeys, "role")
	}

	attrsRaw, hasAttrs := data["attributes"]
	if !hasAttrs {
		if !update {
			return attrs, apperr.BadRequestSource("is missing", pointerOf("data", "attributes"))
		}

		return attrs, nil
	}

	attrObj, err := hash(attrsRaw, pointerOf("data", "attributes"))
	if err != nil {
		return attrs, err
	}

	if err := unpermitted(attrObj, "/data/attributes/", attrKeys...); err != nil {
		return attrs, err
	}

	return userAttrs(attrObj, update)
}

// userRelationships decodes (and gates) data.relationships.
func userRelationships(data map[string]any, groupPrivileged bool) (groupLink, error) {
	var link groupLink

	relsRaw, hasRels := data["relationships"]
	if !hasRels {
		return link, nil
	}

	rels, err := hash(relsRaw, pointerOf("data", "relationships"))
	if err != nil {
		return link, err
	}

	relKeys := []string{}
	if groupPrivileged {
		relKeys = append(relKeys, "group")
	}

	if err := unpermitted(rels, "/data/relationships/", relKeys...); err != nil {
		return link, err
	}

	groupRaw, ok := rels["group"]
	if !ok {
		return link, nil
	}

	link.set, link.id, err = userGroupRelationship(groupRaw)

	return link, err
}

// userAttrs decodes the attributes object.
func userAttrs(obj map[string]any, update bool) (users.Attrs, error) {
	var attrs users.Attrs

	if raw, present := obj["first_name"]; present && raw != nil {
		s, err := str(raw, pointerOf("data", "attributes", "first_name"))
		if err != nil {
			return attrs, err
		}

		attrs.FirstName = &s
	}

	if raw, present := obj["last_name"]; present && raw != nil {
		s, err := str(raw, pointerOf("data", "attributes", "last_name"))
		if err != nil {
			return attrs, err
		}

		attrs.LastName = &s
	}

	email, err := emailAttr(obj, update)
	if err != nil {
		return attrs, err
	}

	attrs.Email = email

	password, err := passwordAttr(obj)
	if err != nil {
		return attrs, err
	}

	attrs.Password = password

	attrs.Metadata, err = metadataAttrOf(obj)
	if err != nil {
		return attrs, err
	}

	if raw, present := obj["role"]; present {
		s, err := str(raw, pointerOf("data", "attributes", "role"))
		if err != nil {
			return attrs, err
		}

		switch s {
		case typeUserSingular, "admin", "developer", "sales-agent", "support-agent", "read-only":
		default:
			return attrs, apperr.BadRequestSource("is invalid", pointerOf("data", "attributes", "role"))
		}

		attrs.Role = s
	}

	return attrs, nil
}

// emailAttr decodes the required (create) or optional (update) email.
func emailAttr(obj map[string]any, update bool) (*string, error) {
	raw, present := obj["email"]
	if !present {
		if !update {
			return nil, apperr.BadRequestSource("is missing", pointerOf("data", "attributes", "email"))
		}

		return nil, nil //nolint:nilnil // absent optional attribute
	}

	if raw == nil {
		if !update {
			return nil, apperr.BadRequestSource("is missing", pointerOf("data", "attributes", "email"))
		}

		return nil, nil //nolint:nilnil // absent optional attribute
	}

	s, err := str(raw, pointerOf("data", "attributes", "email"))

	return &s, err
}

// passwordAttr decodes the optional nullable password (null clears it).
func passwordAttr(obj map[string]any) (*string, error) {
	raw, present := obj["password"]
	if !present {
		return nil, nil //nolint:nilnil // absent optional attribute
	}

	if raw == nil {
		empty := ""

		return &empty, nil
	}

	s, err := str(raw, pointerOf("data", "attributes", "password"))

	return &s, err
}

// metadataAttrOf decodes the optional nullable metadata attribute.
func metadataAttrOf(obj map[string]any) (map[string]any, error) {
	raw, present := obj["metadata"]
	if !present {
		return nil, nil //nolint:nilnil // absent optional attribute
	}

	if raw == nil {
		return map[string]any{}, nil
	}

	m, err := hash(raw, pointerOf("data", "attributes", "metadata"))

	return underscoredMetadata(m), err
}

// userGroupRelationship decodes the group relationship's data (null
// removes the membership).
func userGroupRelationship(raw any) (bool, uuid.NullUUID, error) {
	if raw == nil {
		return true, uuid.NullUUID{}, nil
	}

	rel, err := hash(raw, "/data/relationships/group")
	if err != nil {
		return false, uuid.NullUUID{}, err
	}

	if err := unpermitted(rel, "/data/relationships/group/", "data"); err != nil {
		return false, uuid.NullUUID{}, err
	}

	dataRaw, ok := rel["data"]
	if !ok {
		return false, uuid.NullUUID{}, apperr.BadRequestSource("is missing", "/data/relationships/group/data")
	}

	if dataRaw == nil {
		return true, uuid.NullUUID{}, nil
	}

	data, err := hash(dataRaw, "/data/relationships/group/data")
	if err != nil {
		return false, uuid.NullUUID{}, err
	}

	if err := unpermitted(data, "/data/relationships/group/data/", "type", "id"); err != nil {
		return false, uuid.NullUUID{}, err
	}

	typeStr, err := str(data["type"], "/data/relationships/group/data/type")
	if err != nil {
		return false, uuid.NullUUID{}, err
	}

	if typeStr != typeGroupSingular && typeStr != typeGroupPlural {
		return false, uuid.NullUUID{}, apperr.BadRequestSource("is invalid", "/data/relationships/group/data/type")
	}

	idStr, err := str(data["id"], "/data/relationships/group/data/id")
	if err != nil {
		return false, uuid.NullUUID{}, err
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return false, uuid.NullUUID{}, apperr.BadRequestSource(
			"type mismatch (received string expected UUID string)",
			"/data/relationships/group/data/id",
		)
	}

	return true, uuid.NullUUID{UUID: id, Valid: true}, nil
}

// underscoredMetadata re-underscores the metadata keys (they arrive
// underscored from the pipeline's deep transform already; nested keys
// too).
func underscoredMetadata(m map[string]any) map[string]any {
	return m
}

// validateGroupBody decodes the body of PUT /users/{id}/group.
func validateGroupBody(req *request) (uuid.NullUUID, bool, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return uuid.NullUUID{}, false, err
	}

	dataRaw, ok := body["data"]
	if !ok {
		return uuid.NullUUID{}, false, apperr.BadRequestSource("is missing", "/data")
	}

	if dataRaw == nil {
		return uuid.NullUUID{}, true, nil
	}

	data, err := hash(dataRaw, "/data")
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	if err := unpermitted(data, "/data/", "type", "id"); err != nil {
		return uuid.NullUUID{}, false, err
	}

	typeStr, err := str(data["type"], pointerOf("data", "type"))
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	if typeStr != "group" && typeStr != "groups" {
		return uuid.NullUUID{}, false, apperr.BadRequestSource("is invalid", pointerOf("data", "type"))
	}

	idStr, err := str(data["id"], pointerOf("data", "id"))
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.NullUUID{}, false, apperr.BadRequestSource(
			"type mismatch (received string expected UUID string)",
			pointerOf("data", "id"),
		)
	}

	return uuid.NullUUID{UUID: id, Valid: true}, false, nil
}

// privilegedBearer answers whether the principal may send
// password/metadata/group on user writes.
func privilegedBearer(req *request) bool {
	p := req.principal

	return (p.IsUser() && (p.Role == domain.RoleAdmin || p.Role == domain.RoleDeveloper)) || p.IsProduct()
}
