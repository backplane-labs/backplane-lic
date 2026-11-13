// The typed-params body validation of the license routes: create,
// update, the relationship patches, attach/detach, validate,
// validate-key, usage and checkout — every unknown key an "unpermitted
// parameter", every wrong-typed value a "type mismatch", the
// bearer-dependent keys gated like the reference's conditional schemas.
package httpapi

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// The wire spellings of the license relationship types.
const (
	typeLicensePlural   = "licenses"
	typePolicySingular  = "policy"
	typePolicyPlural    = "policies"
	typeEntitlementSing = "entitlement"
	typeEntitlementPlur = "entitlements"
	typeGroupPlural2    = "groups"
)

// licensePrivileged answers whether the bearer may send the
// admin-side attributes (expiry, protected, suspended, max*).
func licensePrivileged(req *request) bool {
	if !req.principal.IsUser() {
		return req.principal.IsProduct()
	}

	switch req.principal.Role {
	case domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent:
		return true
	case domain.RoleSupportRole:
		return false // support reads but never writes license attributes
	default:
		return false
	}
}

// licenseUpdatePrivileged additionally admits support agents (the
// update schema's writer set).
func licenseUpdatePrivileged(req *request) bool {
	return licensePrivileged(req) ||
		(req.principal.IsUser() && req.principal.Role == domain.RoleSupportRole)
}

// licenseGroupPrivileged answers whether the bearer may set the group
// relationship (create: admin/dev/sales/support/product).
func licenseGroupPrivileged(req *request) bool {
	return licenseUpdatePrivileged(req)
}

// validateLicenseCreateBody decodes POST /licenses.
func validateLicenseCreateBody(req *request) (licenses.Input, error) {
	var in licenses.Input

	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return in, err
	}

	data, err := hash(body["data"], "/data")
	if err != nil {
		return in, err
	}

	if err := unpermitted(data, "/data/", "type", "id", "attributes", "relationships"); err != nil {
		return in, err
	}

	if err := requireType(data, typeLicenseSingular, typeLicensePlural); err != nil {
		return in, err
	}

	if err := licenseIDAttr(req, data, &in); err != nil {
		return in, err
	}

	if attrs, ok := data["attributes"]; ok && attrs != nil {
		if err := licenseAttributes(req, attrs, &in, false); err != nil {
			return in, err
		}
	}

	relsRaw, hasRels := data["relationships"]
	if !hasRels || relsRaw == nil {
		return in, apperr.BadRequestSource("is missing", "/data/relationships")
	}

	if err := licenseCreateRelationships(req, relsRaw, &in); err != nil {
		return in, err
	}

	if !in.PolicyID.Valid {
		return in, apperr.BadRequestSource("is missing", "/data/relationships/policy")
	}

	return in, nil
}

// validateLicenseUpdateBody decodes PATCH /licenses/{id}.
func validateLicenseUpdateBody(req *request) (licenses.Input, error) {
	var in licenses.Input

	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return in, err
	}

	data, err := hash(body["data"], "/data")
	if err != nil {
		return in, err
	}

	// data.id is accepted but noop; relationships and key are not.
	if err := unpermitted(data, "/data/", "type", "id", "attributes"); err != nil {
		return in, err
	}

	if err := requireType(data, typeLicenseSingular, typeLicensePlural); err != nil {
		return in, err
	}

	if raw, ok := data["id"]; ok && raw != nil {
		if _, err := str(raw, "/data/id"); err != nil {
			return in, err
		}
	}

	if attrs, ok := data["attributes"]; ok && attrs != nil {
		if err := licenseAttributes(req, attrs, &in, true); err != nil {
			return in, err
		}
	}

	return in, nil
}

// licenseIDAttr reads the privileged data.id.
func licenseIDAttr(req *request, data map[string]any, in *licenses.Input) error {
	raw, ok := data["id"]
	if !ok || raw == nil {
		return nil
	}

	if !licenseIDPrivileged(req) {
		return apperr.BadRequestSource("unpermitted parameter", "/data/id")
	}

	value, err := str(raw, "/data/id")
	if err != nil {
		return err
	}

	id, err := uuid.Parse(value)
	if err != nil {
		return apperr.BadRequestSource("type mismatch (received string expected UUID string)", "/data/id")
	}

	in.ID = uuid.NullUUID{UUID: id, Valid: true}

	return nil
}

func licenseIDPrivileged(req *request) bool {
	if !req.principal.IsUser() {
		return req.principal.IsProduct()
	}

	switch req.principal.Role {
	case domain.RoleAdmin, domain.RoleDeveloper, domain.RoleSalesAgent:
		return true
	default:
		return false
	}
}

// licenseAttributes decodes data.attributes (the writer set depends on
// the route and the bearer).
func licenseAttributes( //nolint:cyclop,funlen // the attribute ladder
	req *request, raw any, in *licenses.Input, update bool,
) error {
	attrs, err := hash(raw, "/data/attributes")
	if err != nil {
		return err
	}

	allowed := []string{keyName, keyMetadata}
	if !update {
		allowed = append(allowed, "key")
	}

	if licenseAttrWriter(req, update) {
		allowed = append(allowed, "protected", "expiry", "suspended",
			attrMaxMachines, "max_cores", "max_memory", "max_disk",
			"max_uses", "max_processes", attrMaxUsers)
	}

	if err := unpermitted(attrs, "/data/attributes/", allowed...); err != nil {
		return err
	}

	if nameRaw, hasName := attrs[keyName]; hasName && nameRaw != nil {
		value, err := str(nameRaw, "/data/attributes/name")
		if err != nil {
			return err
		}

		in.Name = &value
	}

	if keyRaw, hasKey := attrs["key"]; hasKey && keyRaw != nil {
		value, err := str(keyRaw, "/data/attributes/key")
		if err != nil {
			return err
		}

		in.Key = &value
	}

	if err := licenseLimitAttrs(attrs, in); err != nil {
		return err
	}

	if _, ok := attrs["protected"]; ok {
		value, _, err := boolAttr(attrs, "protected")
		if err != nil {
			return err
		}

		in.Protected, in.ProtectedSet = value, true
	}

	if _, ok := attrs["suspended"]; ok {
		value, _, err := boolAttr(attrs, "suspended")
		if err != nil {
			return err
		}

		in.Suspended, in.SuspendedSet = value, true
	}

	if _, hasExpiry := attrs["expiry"]; hasExpiry {
		in.Expiry, in.ExpirySet, err = timeAttr(attrs, "expiry")
		if err != nil {
			return err
		}
	}

	if _, hasMeta := attrs[keyMetadata]; hasMeta {
		meta, present, metaErr := metadataAttr(attrs)
		if metaErr != nil {
			return metaErr
		}

		in.Metadata, in.MetadataSet = meta, present
	}

	return nil
}

// licenseAttrWriter answers whether the bearer may write the
// privileged attributes (create and update differ for support).
func licenseAttrWriter(req *request, update bool) bool {
	if update {
		return licenseUpdatePrivileged(req)
	}

	return licensePrivileged(req)
}

func licenseLimitAttrs(attrs map[string]any, in *licenses.Input) error {
	var err error

	if in.MaxMachines, in.MaxMachinesSet, err = intAttr(attrs, attrMaxMachines); err != nil {
		return err
	}

	if in.MaxCores, in.MaxCoresSet, err = intAttr(attrs, "max_cores"); err != nil {
		return err
	}

	if in.MaxMemory, in.MaxMemorySet, err = bigintAttr(attrs, "max_memory"); err != nil {
		return err
	}

	if in.MaxDisk, in.MaxDiskSet, err = bigintAttr(attrs, "max_disk"); err != nil {
		return err
	}

	if in.MaxUses, in.MaxUsesSet, err = intAttr(attrs, "max_uses"); err != nil {
		return err
	}

	if in.MaxProcesses, in.MaxProcessesSet, err = intAttr(attrs, "max_processes"); err != nil {
		return err
	}

	in.MaxUsers, in.MaxUsersSet, err = intAttr(attrs, attrMaxUsers)

	return err
}

func timeAttr(attrs map[string]any, name string) (*time.Time, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return nil, false, nil
	}

	if raw == nil {
		return nil, true, nil
	}

	value, err := str(raw, "/data/attributes/"+name)
	if err != nil {
		return nil, true, err
	}

	if t, parsed := parseTime(value); parsed {
		return &t, true, nil
	}

	// The reference's lax coercion drops unparseable times.
	return nil, false, nil
}

// licenseCreateRelationships decodes the create relationships.
func licenseCreateRelationships(req *request, raw any, in *licenses.Input) error {
	rels, err := hash(raw, "/data/relationships")
	if err != nil {
		return err
	}

	allowed := []string{typePolicySingular, keyOwner, typeUserSingular}
	if licenseGroupPrivileged(req) {
		allowed = append(allowed, "group")
	}

	if err := unpermitted(rels, "/data/relationships/", allowed...); err != nil {
		return err
	}

	if err := licensePolicyRelationship(rels, in); err != nil {
		return err
	}

	ownerRaw, hasOwner := rels[keyOwner]
	userRaw, hasUser := rels["user"]

	if hasOwner && hasUser {
		return apperr.BadRequestSource("unpermitted parameter", "/data/relationships/user")
	}

	linkRaw := ownerRaw
	pointer := keyOwner

	if hasUser {
		linkRaw, pointer = userRaw, "user"
	}

	if linkRaw != nil {
		in.Owner, in.OwnerSet, err = userLinkage(linkRaw, pointer)
		if err != nil {
			return err
		}
	}

	if groupRaw, ok := rels["group"]; ok && groupRaw != nil {
		in.Group, err = groupLinkage(groupRaw)
		if err != nil {
			return err
		}

		in.GroupSet = true
	}

	return nil
}

func licensePolicyRelationship(rels map[string]any, in *licenses.Input) error {
	raw, ok := rels[typePolicySingular]
	if !ok || raw == nil {
		return apperr.BadRequestSource("is missing", "/data/relationships/policy")
	}

	rel, err := hash(raw, "/data/relationships/policy")
	if err != nil {
		return err
	}

	if err := unpermitted(rel, "/data/relationships/policy/", "data"); err != nil {
		return err
	}

	id, err := typedLinkage(rel["data"], "/data/relationships/policy/data", typePolicySingular, typePolicyPlural)
	if err != nil {
		return err
	}

	in.PolicyID = uuid.NullUUID{UUID: id, Valid: true}

	return nil
}

// userLinkage decodes an owner (user) relationship whose data may be
// null (clearing).
func userLinkage(raw any, name string) (uuid.NullUUID, bool, error) {
	rel, err := hash(raw, "/data/relationships/"+name)
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	if err := unpermitted(rel, "/data/relationships/"+name+"/", "data"); err != nil {
		return uuid.NullUUID{}, false, err
	}

	dataRaw, ok := rel["data"]
	if !ok {
		return uuid.NullUUID{}, false, apperr.BadRequestSource("is missing", "/data/relationships/"+name+"/data")
	}

	if dataRaw == nil {
		return uuid.NullUUID{}, true, nil
	}

	data, err := hash(dataRaw, "/data/relationships/"+name+"/data")
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	if err := unpermitted(data, "/data/relationships/"+name+"/data/", "type", "id"); err != nil {
		return uuid.NullUUID{}, false, err
	}

	id, err := typedLinkage(data, "/data/relationships/"+name+"/data", typeUserSingular, typeUserPlural)
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	return uuid.NullUUID{UUID: id, Valid: true}, false, nil
}

// groupLinkage reads the group linkage of a create body; a null linkage
// is the invalid (absent) id.
func groupLinkage(raw any) (uuid.NullUUID, error) {
	rel, err := hash(raw, "/data/relationships/group")
	if err != nil {
		return uuid.NullUUID{}, err
	}

	if err := unpermitted(rel, "/data/relationships/group/", "data"); err != nil {
		return uuid.NullUUID{}, err
	}

	dataRaw, ok := rel["data"]
	if !ok {
		return uuid.NullUUID{}, apperr.BadRequestSource("is missing", "/data/relationships/group/data")
	}

	if dataRaw == nil {
		return uuid.NullUUID{}, nil
	}

	data, err := hash(dataRaw, "/data/relationships/group/data")
	if err != nil {
		return uuid.NullUUID{}, err
	}

	if err := unpermitted(data, "/data/relationships/group/data/", "type", "id"); err != nil {
		return uuid.NullUUID{}, err
	}

	id, err := typedLinkage(data, "/data/relationships/group/data", typeGroupSingular, typeGroupPlural2)
	if err != nil {
		return uuid.NullUUID{}, err
	}

	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// typedLinkage reads a {type, id} linkage object, checking the type
// spellings and the UUID.
func typedLinkage(raw any, pointer, singular, plural string) (uuid.UUID, error) {
	data, ok := raw.(map[string]any)
	if !ok {
		return uuid.Nil, apperr.BadRequestSource("is missing", pointer)
	}

	typeRaw, ok := data["type"]
	if !ok {
		return uuid.Nil, apperr.BadRequestSource("is missing", pointer+"/type")
	}

	typeStr, err := str(typeRaw, pointer+"/type")
	if err != nil {
		return uuid.Nil, err
	}

	if typeStr != singular && typeStr != plural {
		return uuid.Nil, apperr.BadRequestSource("is invalid", pointer+"/type")
	}

	idRaw, ok := data["id"]
	if !ok {
		return uuid.Nil, apperr.BadRequestSource("is missing", pointer+"/id")
	}

	idStr, err := str(idRaw, pointer+"/id")
	if err != nil {
		return uuid.Nil, err
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, apperr.BadRequestSource("type mismatch (received string expected UUID string)", pointer+"/id")
	}

	return id, nil
}

// validateRelationshipPatchBody decodes the PATCH policy/group/owner
// (and v1.5 user) bodies.
func validateRelationshipPatchBody(
	req *request, kind string,
) (uuid.NullUUID, bool, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return uuid.NullUUID{}, false, err
	}

	if body["data"] == nil {
		if kind == typePolicySingular {
			return uuid.NullUUID{}, false, apperr.BadRequestSource("is missing", "/data")
		}

		// A null linkage clears the relationship.
		return uuid.NullUUID{}, true, nil
	}

	data, err := hash(body["data"], "/data")
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	if err := unpermitted(data, "/data/", "type", "id"); err != nil {
		return uuid.NullUUID{}, false, err
	}

	singular, plural := typeGroupSingular, typeGroupPlural2

	switch kind {
	case typePolicySingular:
		singular, plural = typePolicySingular, typePolicyPlural
	case keyOwner, typeUserSingular:
		singular, plural = typeUserSingular, typeUserPlural
	case "group":
	}

	id, err := typedLinkage(data, "/data", singular, plural)
	if err != nil {
		return uuid.NullUUID{}, false, err
	}

	return uuid.NullUUID{UUID: id, Valid: true}, false, nil
}

// validateAttachBody decodes the users/entitlements attach/detach
// bodies (an array of 1..100 linkage objects).
func validateAttachBody(req *request, singular, plural string) ([]uuid.UUID, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "data"); err != nil {
		return nil, err
	}

	dataRaw, ok := body["data"]
	if !ok || dataRaw == nil {
		return nil, apperr.BadRequestSource("is missing", "/data")
	}

	list, ok := dataRaw.([]any)
	if !ok {
		return nil, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(dataRaw)+" expected array)", "/data")
	}

	if len(list) < 1 || len(list) > 100 {
		return nil, apperr.BadRequestSource("length must be between 1 and 100 (inclusive)", "/data")
	}

	ids := make([]uuid.UUID, 0, len(list))
	for index, item := range list {
		pointer := "/data/" + itoa(index)

		data, err := hash(item, pointer)
		if err != nil {
			return nil, err
		}

		if err := unpermitted(data, pointer+"/", "type", "id"); err != nil {
			return nil, err
		}

		id, err := typedLinkage(data, pointer, singular, plural)
		if err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}

	digits := ""
	for i > 0 {
		digits = string(rune('0'+i%10)) + digits
		i /= 10
	}

	return digits
}

// validateValidationBody decodes the POST validate meta (nonce and
// scope).
func validateValidationBody(req *request, byKey bool) (validationBody, error) {
	var out validationBody

	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "meta"); err != nil {
		return out, err
	}

	metaRaw, ok := body["meta"]
	if !ok || metaRaw == nil {
		if byKey {
			return out, apperr.BadRequestSource("is missing", "/meta/key")
		}

		return out, nil
	}

	meta, err := hash(metaRaw, "/meta")
	if err != nil {
		return out, err
	}

	if err := validationMetaKeys(meta, byKey); err != nil {
		return out, err
	}

	nonce, hasNonce, err := validationNonce(meta)
	if err != nil {
		return out, err
	}

	if hasNonce {
		out.Nonce = &nonce
	}

	out.Legacy = meta["legacy_encrypted"] == true || meta["encrypted"] == true

	if out.Key, err = validationKey(meta, byKey); err != nil {
		return out, err
	}

	if scopeRaw, hasScope := meta["scope"]; hasScope && scopeRaw != nil {
		if out.Scope, err = validateScope(scopeRaw); err != nil {
			return out, err
		}

		out.RawScope = validationScopeEcho(scopeRaw)
	}

	out.Scope.Nonce, out.Scope.Echo = out.Nonce, out.RawScope

	return out, nil
}

// validationBody is the decoded validate meta.
type validationBody struct {
	Nonce    *int64
	Scope    licenses.Scope
	RawScope map[string]any
	// Key and Legacy are the validate-key body's key and its legacy flag.
	Key    string
	Legacy bool
}

func validationMetaKeys(meta map[string]any, byKey bool) error {
	if byKey {
		return unpermitted(meta, "/meta/", "key", "legacy_encrypted", "encrypted", "nonce", "scope")
	}

	return unpermitted(meta, "/meta/", "nonce", "scope")
}

// validationNonce reads the optional nonce; present is false when absent.
func validationNonce(meta map[string]any) (int64, bool, error) {
	nonceRaw, ok := meta["nonce"]
	if !ok || nonceRaw == nil {
		return 0, false, nil
	}

	value, err := int64Of(nonceRaw, "/meta/nonce")
	if err != nil {
		return 0, false, err
	}

	return value, true, nil
}

func validationKey(meta map[string]any, byKey bool) (string, error) {
	keyRaw, ok := meta["key"]
	if !ok {
		if byKey {
			return "", apperr.BadRequestSource("is missing", "/meta/key")
		}

		return "", nil
	}

	value, err := str(keyRaw, "/meta/key")
	if err != nil {
		return "", err
	}

	if value == "" {
		return "", apperr.BadRequestSource("cannot be blank", "/meta/key")
	}

	return value, nil
}

// validationScopeEcho mirrors typed_params dropping optional nil fields,
// while retaining the explicitly nullable user scope.
func validationScopeEcho(raw any) map[string]any {
	out := make(map[string]any)

	switch fields := raw.(type) { //nolint:gocritic // project rules prefer a type switch to an assertion
	case map[string]any:
		for name, value := range fields {
			if value != nil || name == "user" {
				out[name] = value
			}
		}
	}

	return out
}

func scopeString(obj map[string]any, name string) (*string, error) {
	value, err := str(obj[name], "/meta/scope/"+name)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(value) == "" {
		return nil, apperr.BadRequestSource("cannot be blank", "/meta/scope/"+name)
	}

	return &value, nil
}

// validateScope decodes meta.scope.
func validateScope(raw any) (licenses.Scope, error) { //nolint:cyclop // the typed-params ladder
	var scope licenses.Scope

	obj, err := hash(raw, "/meta/scope")
	if err != nil {
		return scope, err
	}

	if err := unpermitted(obj, "/meta/scope/",
		"product", "policy", "user", "machine", "fingerprint",
		"fingerprints", "components", "entitlements", "checksum", "version"); err != nil {
		return scope, err
	}

	scope.Present = true

	for _, name := range []string{typeProductSingular, typePolicySingular, "machine", "checksum", "version"} {
		if _, has := obj[name]; has && obj[name] != nil {
			ptr, err := scopeString(obj, name)
			if err != nil {
				return scope, err
			}

			switch name {
			case typeProductSingular:
				scope.Product = ptr
			case typePolicySingular:
				scope.Policy = ptr
			case "machine":
				scope.Machine = ptr
			case "checksum":
				scope.Checksum = ptr
			case "version":
				scope.Version = ptr
			}
		}
	}

	_, scope.UserSet = obj["user"]

	if scope.UserSet && obj["user"] != nil {
		ptr, err := scopeString(obj, "user")
		if err != nil {
			return scope, err
		}

		scope.User = ptr
	}

	if err := scopeLists(obj, &scope); err != nil {
		return scope, err
	}

	return scope, nil
}

func scopeLists(obj map[string]any, scope *licenses.Scope) error {
	for _, name := range []string{keyFingerprint, "fingerprints", typeComponentPlural, "entitlements"} {
		if obj[name] == nil {
			continue
		}

		item := obj[name]

		var (
			list []string
			err  error
		)

		if name == keyFingerprint {
			var single *string

			single, err = scopeString(obj, name)
			if err == nil {
				list = []string{*single}
			}
		} else {
			list, err = scopeStrings(item, "/meta/scope/"+name)
		}

		if err != nil {
			return err
		}

		switch name {
		case keyFingerprint, "fingerprints":
			if scope.Fingerprints == nil {
				scope.Fingerprints = []string{}
			}

			scope.Fingerprints = append(scope.Fingerprints, list...)
		case typeComponentPlural:
			scope.Components = list
		case "entitlements":
			scope.Entitlements = list
		}
	}

	return nil
}

func scopeStrings(raw any, pointer string) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, apperr.BadRequestSource("type mismatch (received "+jsonKind(raw)+" expected array)", pointer)
	}

	out := make([]string, 0, len(items))
	for index, item := range items {
		path := fmt.Sprintf("%s/%d", pointer, index)

		value, err := str(item, path)
		if err != nil {
			return nil, err
		}

		if strings.TrimSpace(value) == "" {
			return nil, apperr.BadRequestSource("cannot be blank", path)
		}

		out = append(out, value)
	}

	return out, nil
}

// validateUsageBody decodes meta.increment / meta.decrement.
func validateUsageBody(req *request, key string) (int64, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	if err := unpermitted(body, "/", "meta"); err != nil {
		return 1, err
	}

	metaRaw, ok := body["meta"]
	if !ok || metaRaw == nil {
		return 1, nil
	}

	meta, err := hash(metaRaw, "/meta")
	if err != nil {
		return 1, err
	}

	if err := unpermitted(meta, "/meta/", key); err != nil {
		return 1, err
	}

	raw, ok := meta[key]
	if !ok || raw == nil {
		return 1, nil
	}

	value, err := int64Of(raw, "/meta/"+key)
	if err != nil {
		return 1, err
	}

	return value, nil
}

// validateCheckoutMeta decodes the checkout meta (the query parameters
// merge over it).
func validateCheckoutMeta(req *request) (licenses.CheckoutParams, error) {
	body := req.body
	if body == nil {
		body = map[string]any{}
	}

	var params licenses.CheckoutParams

	if err := unpermitted(body, "/", "meta"); err != nil {
		return params, err
	}

	metaRaw, hasMeta := body["meta"]
	if !hasMeta || metaRaw == nil {
		return params, nil
	}

	meta, err := hash(metaRaw, "/meta")
	if err != nil {
		return params, err
	}

	if err := unpermitted(meta, "/meta/", "encrypt", "algorithm", "ttl", "include"); err != nil {
		return params, err
	}

	encrypt, hasEncrypt, err := checkoutEncrypt(meta)
	if err != nil {
		return params, err
	}

	if hasEncrypt {
		params.Encrypt = &encrypt
	}

	if algRaw, hasAlg := meta["algorithm"]; hasAlg && algRaw != nil {
		if params.Algorithm, err = str(algRaw, "/meta/algorithm"); err != nil {
			return params, err
		}
	}

	if params.TTL, params.TTLSet, err = checkoutTTL(meta); err != nil {
		return params, err
	}

	params.Include, _, err = stringsAttr(meta, "include")

	return params, err
}

// checkoutEncrypt reads meta.encrypt; present is false when absent or null.
func checkoutEncrypt(meta map[string]any) (bool, bool, error) {
	encRaw, ok := meta["encrypt"]
	if !ok || encRaw == nil {
		return false, false, nil
	}

	switch value := encRaw.(type) {
	case bool:
		return value, true, nil
	default:
		return false, false, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(encRaw)+" expected boolean)", "/meta/encrypt")
	}
}

// checkoutTTL reads meta.ttl. An explicit null asks for a perpetual file:
// set is true and the ttl nil.
func checkoutTTL(meta map[string]any) (*int32, bool, error) {
	ttlRaw, set := meta["ttl"]
	if !set || ttlRaw == nil {
		return nil, set, nil
	}

	value, err := int64Of(ttlRaw, "/meta/ttl")
	if err != nil {
		return nil, false, err
	}

	if value > math.MaxInt32 || value < math.MinInt32 {
		return nil, false, apperr.BadRequestSource("integer is too large", "/meta/ttl")
	}

	ttl := int32(value)

	return &ttl, true, nil
}
