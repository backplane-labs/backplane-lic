// Package search is POST /search: it validates a typed query, authorizes
// the bearer, and resolves the matching ids of one resource type. The
// resource's own scenario then pages and renders those ids like its index.
package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// errNotObject is a value that is not a JSON object.
var errNotObject = errors.New("search: expected an object")

// MinQuery is the shortest string term the search accepts.
const MinQuery = 3

// The searchable resources (wire types).
const (
	Licenses     = "licenses"
	Machines     = "machines"
	Users        = "users"
	Products     = "products"
	Policies     = "policies"
	Groups       = "groups"
	Entitlements = "entitlements"
)

// Term is one attribute's search value in every form a query may need.
type Term struct {
	// ID is set when the value is a UUID: identifiers then match exactly.
	ID uuid.NullUUID
	// Text is the value as given; Like is it escaped for ILIKE.
	Text string
	Like string
	// Prefix is set when the value is made of UUID characters, so a
	// relationship also matches by id prefix; TSV is the tsquery operand.
	Prefix bool
	TSV    string
	// Raw and Cast are the metadata containment documents: the value as
	// given and type-cast ("true", "12", "1.5", "null").
	Raw, Cast []byte
}

// Store resolves one attribute's matches of a resource type.
type Store interface {
	SearchIDs(ctx context.Context, accountID uuid.UUID, resource, attribute string, term Term) ([]uuid.UUID, error)
}

// Field is one key of the query object, in request order.
type Field struct {
	Key   string
	Value json.RawMessage
}

// Request is the decoded meta object.
type Request struct {
	Type    string
	HasType bool
	Op      string
	Query   []Field
	// HasQuery reports a present query member; HasMeta a present meta.
	HasQuery bool
	HasMeta  bool
}

// Result is the matching ids of the resolved resource type.
type Result struct {
	Resource string
	IDs      []uuid.UUID
}

// Scenario runs searches.
type Scenario struct {
	store Store
}

// New builds the scenario.
func New(store Store) Scenario { return Scenario{store: store} }

type kind int

const (
	text  kind = iota // matched with ILIKE: the value must be a string
	loose             // the value is read as text (numbers allowed)
	meta              // a metadata containment object
)

// attributes lists each resource's searchable attributes.
var attributes = map[string]map[string]kind{ //nolint:gochecknoglobals // reference data
	Licenses: {
		"id": loose, "key": loose, "name": text, "metadata": meta, "owner": loose,
		"user": loose, "product": loose, "policy": loose,
	},
	Machines: {
		"id": loose, "fingerprint": loose, "name": text, "metadata": meta, "license": loose,
		"owner": loose, "user": loose, "product": loose, "policy": loose,
	},
	Users: {
		"id": loose, "email": text, "first_name": text, "last_name": text, "full_name": loose,
		"name": loose, "metadata": meta, "role": loose,
	},
	Products:     {"id": loose, "name": text, "metadata": meta},
	Policies:     {"id": loose, "name": text, "metadata": meta, "product": loose},
	Groups:       {"name": text},
	Entitlements: {"id": loose, "code": text, "name": text},
}

// searchers are the roles the search policy admits.
var searchers = []domain.Role{ //nolint:gochecknoglobals // reference data
	domain.RoleAdmin, domain.RoleDeveloper, domain.RoleReadOnly, domain.RoleSalesAgent, domain.RoleSupportRole,
}

// Search validates the request in the reference's order and answers the
// matching ids. Where the reference fails with a 500 (an unknown op, a
// non-string term for a text attribute, the EE request-log search) this
// answers a 400 in the same style instead.
func (s Scenario) Search(
	ctx context.Context, account domain.Account, req Request,
) (Result, error) {
	switch {
	case !req.HasMeta:
		return Result{}, apperr.BadRequestSource("is missing", "/meta")
	case !req.HasType:
		return Result{}, apperr.BadRequestSource("is missing", "/meta/type")
	case !req.HasQuery:
		return Result{}, apperr.BadRequestSource("is missing", "/meta/query")
	case len(req.Query) == 0:
		return Result{}, apperr.BadRequestSource("cannot be blank", "/meta/query")
	}

	resource := resourceOf(req.Type)

	operator := strings.ToUpper(req.Op)
	if operator == "" {
		operator = "AND"
	}

	if resource != "" && operator != "AND" && operator != "OR" {
		return Result{}, apperr.BadRequestSource(fmt.Sprintf("search operation '%s' is not supported", req.Op), "/meta/op")
	}

	if resource == "" {
		return Result{}, apperr.BadRequestSource(
			fmt.Sprintf("search type '%s' is not supported", lowerCamel(req.Type)), "/meta/type")
	}

	principal := access.PrincipalOf(ctx)
	if !principal.Console && !slices.Contains(searchers, principal.Role) {
		return Result{}, apperr.ForbiddenReason(access.ReasonAllResources)
	}

	var sets [][]uuid.UUID

	for _, field := range req.Query {
		attribute := underscore(field.Key)

		form, ok := attributes[resource][attribute]
		if !ok {
			return Result{}, apperr.BadRequestSource(
				fmt.Sprintf("search query '%s' is not supported for resource type '%s'",
					lowerCamel(attribute), lowerCamel(req.Type)),
				"/meta/query/"+lowerCamel(attribute))
		}

		ids, err := s.match(ctx, account.ID, resource, attribute, form, field.Value)
		if err != nil {
			return Result{}, err
		}

		sets = append(sets, ids)
	}

	return Result{Resource: resource, IDs: combine(sets, operator == "OR")}, nil
}

func (s Scenario) match(
	ctx context.Context, accountID uuid.UUID, resource, attribute string, form kind, value json.RawMessage,
) ([]uuid.UUID, error) {
	pointer := "/meta/query/" + lowerCamel(attribute)

	if form == meta {
		return s.metadata(ctx, accountID, resource, value, pointer)
	}

	term, isString, ok := scalar(value)
	if !ok || form == text && !isString {
		return nil, apperr.BadRequestSource(
			fmt.Sprintf("search query for '%s' must be a string", lowerCamel(attribute)), pointer)
	}

	if isString && len([]rune(term)) < MinQuery {
		return nil, apperr.BadRequestSource(
			fmt.Sprintf("search query for '%s' is too small (minimum %d characters)", lowerCamel(attribute), MinQuery), pointer)
	}

	if resource == Machines {
		return []uuid.UUID{}, nil // devices: deferred, the account has none
	}

	if attribute == "full_name" || attribute == "name" && resource == Users {
		return s.fullName(ctx, accountID, term)
	}

	if strings.TrimSpace(term) == "" {
		return []uuid.UUID{}, nil
	}

	ids, err := s.store.SearchIDs(ctx, accountID, resource, attribute, termOf(term))
	if err != nil {
		return nil, fmt.Errorf("search: %s %s: %w", resource, attribute, err)
	}

	return ids, nil
}

// fullName matches a first name and a last name; a single word matches
// nothing, as the reference's split leaves the last name blank.
func (s Scenario) fullName(ctx context.Context, accountID uuid.UUID, term string) ([]uuid.UUID, error) {
	first, last, found := strings.Cut(term, " ")
	if !found || strings.TrimSpace(first) == "" || strings.TrimSpace(last) == "" {
		return []uuid.UUID{}, nil
	}

	firsts, err := s.store.SearchIDs(ctx, accountID, Users, "first_name", termOf(first))
	if err != nil {
		return nil, fmt.Errorf("search: users first_name: %w", err)
	}

	lasts, err := s.store.SearchIDs(ctx, accountID, Users, "last_name", termOf(last))
	if err != nil {
		return nil, fmt.Errorf("search: users last_name: %w", err)
	}

	return combine([][]uuid.UUID{firsts, lasts}, false), nil
}

func (s Scenario) metadata(
	ctx context.Context, accountID uuid.UUID, resource string, value json.RawMessage, pointer string,
) ([]uuid.UUID, error) {
	terms, ok := object(value)
	if !ok {
		return nil, apperr.BadRequestSource(
			"search query for 'metadata' must be a hash of key-value search terms", pointer)
	}

	for _, field := range terms {
		if text, isString, _ := scalar(field.Value); isString && len([]rune(text)) < MinQuery {
			return nil, apperr.BadRequestSource(
				fmt.Sprintf("search query for '%s' is too small (minimum %d characters)", lowerCamel(field.Key), MinQuery),
				pointer+"/"+lowerCamel(field.Key))
		}
	}

	if resource == Machines {
		return []uuid.UUID{}, nil
	}

	sets := make([][]uuid.UUID, 0, len(terms))

	for _, field := range terms {
		key := underscore(field.Key)

		raw, err := json.Marshal(map[string]json.RawMessage{key: field.Value})
		if err != nil {
			return nil, fmt.Errorf("search: metadata: %w", err)
		}

		cast, err := json.Marshal(map[string]any{key: castValue(field.Value)})
		if err != nil {
			return nil, fmt.Errorf("search: metadata: %w", err)
		}

		ids, err := s.store.SearchIDs(ctx, accountID, resource, "metadata", Term{Raw: raw, Cast: cast})
		if err != nil {
			return nil, fmt.Errorf("search: %s metadata: %w", resource, err)
		}

		sets = append(sets, ids)
	}

	return combine(sets, false), nil
}

var (
	integer  = regexp.MustCompile(`^\d+$`)
	decimal  = regexp.MustCompile(`^\d+\.\d+$`)
	uuidChar = regexp.MustCompile(`(?i)^[-0-9a-f]+$`)
	tsvStrip = regexp.MustCompile(`['?\\:‘’|&!*]`)
)

// castValue is the reference's type cast of a metadata search string.
func castValue(value json.RawMessage) any {
	var text string
	if json.Unmarshal(value, &text) != nil {
		return value
	}

	switch {
	case text == "true":
		return true
	case text == "false":
		return false
	case text == "null":
		return nil
	case integer.MatchString(text):
		return json.Number(text)
	case decimal.MatchString(text):
		return json.Number(text)
	default:
		return text
	}
}

func termOf(text string) Term {
	term := Term{Text: text, Like: escapeLike(text)}
	if id, err := uuid.Parse(text); err == nil && len(text) == 36 {
		term.ID = uuid.NullUUID{UUID: id, Valid: true}
	}

	if uuidChar.MatchString(text) {
		term.Prefix = true
		term.TSV = tsvStrip.ReplaceAllString(text, " ")
	}

	return term
}

// escapeLike escapes the LIKE wildcards, as sanitize_sql_like does.
func escapeLike(text string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text)
}

// scalar reads a string or number term: the text, whether it was a
// string, and false for anything else.
func scalar(value json.RawMessage) (string, bool, bool) {
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text, true, true
	}

	var number json.Number
	if json.Unmarshal(value, &number) == nil {
		return number.String(), false, true
	}

	return "", false, false
}

// object reads a JSON object keeping its key order.
func object(value json.RawMessage) ([]Field, bool) {
	fields, err := ObjectFields(value)

	return fields, err == nil
}

// ObjectFields decodes a JSON object into its members in document order.
func ObjectFields(value json.RawMessage) ([]Field, error) {
	decoder := json.NewDecoder(strings.NewReader(string(value)))

	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errNotObject
	}

	var fields []Field

	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("search: object key: %w", err)
		}

		key, _ := token.(string)

		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return nil, fmt.Errorf("search: object value: %w", err)
		}

		fields = append(fields, Field{Key: key, Value: raw})
	}

	return fields, nil
}

// combine reduces the per-attribute sets: AND keeps the common ids in the
// first set's order, OR their union.
func combine(sets [][]uuid.UUID, union bool) []uuid.UUID {
	if len(sets) == 0 {
		return []uuid.UUID{}
	}

	out := slices.Clone(sets[0])
	if out == nil {
		out = []uuid.UUID{}
	}

	for _, set := range sets[1:] {
		if union {
			for _, id := range set {
				if !slices.Contains(out, id) {
					out = append(out, id)
				}
			}

			continue
		}

		out = slices.DeleteFunc(out, func(id uuid.UUID) bool { return !slices.Contains(set, id) })
	}

	return out
}

// resourceOf maps the request's type (plural or singular, any casing or
// separator) onto a searchable resource; "" when it is not one.
func resourceOf(kind string) string {
	name := underscore(kind)
	singular := map[string]string{
		"license": Licenses, "machine": Machines, "user": Users, "product": Products,
		"policy": Policies, "group": Groups, "entitlement": Entitlements,
	}

	if resource, ok := singular[name]; ok {
		return resource
	}

	if _, ok := attributes[name]; ok {
		return name
	}

	return ""
}

// underscore is ActiveSupport's underscore plus parameterize('_').
func underscore(key string) string {
	var b strings.Builder

	runes := []rune(key)
	for i, r := range runes {
		switch {
		case unicode.IsUpper(r):
			if i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
				b.WriteByte('_')
			}

			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}

	return strings.Trim(b.String(), "_")
}

// lowerCamel is camelize(:lower) of an underscored or dashed name.
func lowerCamel(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' })
	for i := 1; i < len(parts); i++ {
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}

	return strings.Join(parts, "")
}
