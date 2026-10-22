package httpapi

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
)

// checkEnvironment rejects the EE-only environment selector: the
// ?environment= query parameter (any value, blank included) and the
// Keygen-Environment header.
func checkEnvironment(req *request) error {
	t := &apperr.T{
		Status: 400, //nolint:mnd // the reference's literal bound
		Title:  apperr.TitleBadRequest,
		Detail: "is unsupported",
		Code:   "ENVIRONMENT_NOT_SUPPORTED",
	}

	if _, ok := req.query["environment"]; ok {
		t.Source = apperr.Source{Parameter: "environment"}

		return t
	}

	if req.headers.Get("Keygen-Environment") != "" {
		t.Source = apperr.Source{Header: "Keygen-Environment"}

		return t
	}

	return nil
}

// listParams parses the request's pagination (after the environment
// check; the reference parses the page before authorizing).
func listParams(req *request) (paging.Params, error) {
	params, err := paging.Parse(req.query)
	if err != nil {
		return params, err //nolint:wrapcheck // the pagination's own error
	}

	return params, nil
}

// listPath answers the original request path (query stripped): what
// the collection links build on.
func listPath(req *request) string {
	if index := strings.IndexByte(req.originalURI, '?'); index >= 0 {
		return req.originalURI[:index]
	}

	return req.originalURI
}

// linkQuery answers the original request's filter parameters (the
// pagination and sorting parameters are rebuilt): the collection links
// carry them along, like the reference's.
func linkQuery(req *request) url.Values {
	base := url.Values{}

	for key, values := range req.query {
		switch key {
		case "limit", "order", "page[number]", "page[size]", "page[cursor]":
			continue
		}

		for _, value := range values {
			base.Add(key, value)
		}
	}

	return base
}

// offsetDoc builds the offset-mode links against the original path
// (carrying the original filters).
func offsetDoc(req *request, params paging.Params, pages, count int) *paging.Doc {
	link := func(number int) string {
		q := linkQuery(req)
		q.Set("page[number]", strconv.Itoa(number))
		q.Set("page[size]", strconv.Itoa(params.SizeOrDefault()))

		return listPath(req) + "?" + q.Encode()
	}

	first, last := link(1), link(max(pages, 0))

	doc := paging.Doc{
		Self:  link(params.Number),
		First: &first,
		Last:  &last,
		Meta:  &paging.Meta{Pages: pages, Count: count},
	}

	if params.Number > 1 && params.Number <= pages {
		prev := link(params.Number - 1)
		doc.Prev = &prev
	}

	if params.Number < pages {
		next := link(params.Number + 1)
		doc.Next = &next
	}

	return &doc
}

// cursorDoc builds the keyset-mode links against the original path
// (carrying the original filters): self retains the cursor, next is
// the page's last row.
func cursorDoc(req *request, params paging.Params, next uuid.NullUUID) *paging.Doc {
	self := linkQuery(req)
	if params.Cursor.Valid {
		self.Set("page[cursor]", params.Cursor.UUID.String())
	}

	self.Set("page[size]", strconv.Itoa(params.SizeOrDefault()))

	doc := paging.Doc{Self: listPath(req) + "?" + self.Encode()}

	if next.Valid {
		q := linkQuery(req)
		q.Set("page[cursor]", next.UUID.String())
		q.Set("page[size]", strconv.Itoa(params.SizeOrDefault()))
		link := listPath(req) + "?" + q.Encode()
		doc.Next = &link
	}

	return &doc
}

// linksDoc answers the page's links document (nil in limit mode).
func linksDoc(req *request, params paging.Params, count int, next uuid.NullUUID) *paging.Doc {
	switch params.Mode {
	case paging.ModeOffset:
		pages := (count + params.SizeOrDefault() - 1) / params.SizeOrDefault()
		return offsetDoc(req, params, pages, count)
	case paging.ModeCursor:
		return cursorDoc(req, params, next)
	default:
		return nil
	}
}

// dataHash extracts the data object of a JSON:API request body (the
// decoded, key-underscored form).
func dataHash(body map[string]any) (map[string]any, error) {
	if body == nil {
		return nil, apperr.BadRequestSource("is missing", "/data")
	}

	return hash(body["data"], "/data")
}

// requireType checks data.type against the accepted spellings.
func requireType(data map[string]any, singular, plural string) error {
	raw, ok := data["type"]
	if !ok {
		return apperr.BadRequestSource("is missing", "/data/type")
	}

	value, err := str(raw, "/data/type")
	if err != nil {
		return err
	}

	if value != singular && value != plural {
		return apperr.BadRequestSource("is invalid", "/data/type")
	}

	return nil
}

// optionalAttributes extracts data.attributes when present.
func optionalAttributes(data map[string]any) (map[string]any, bool, error) {
	raw, ok := data["attributes"]
	if !ok {
		return nil, false, nil
	}

	attrs, err := hash(raw, "/data/attributes")

	return attrs, true, err
}

// stringAttr reads a string attribute; blank answers "" (present).
func stringAttr(attrs map[string]any, name string, required bool) (*string, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		if required {
			return nil, false, apperr.BadRequestSource("is missing", "/data/attributes/"+name)
		}

		return nil, false, nil
	}

	if raw == nil {
		return nil, true, nil
	}

	value, err := str(raw, "/data/attributes/"+name)
	if err != nil {
		return nil, true, err
	}

	if value == "" {
		return nil, true, apperr.BadRequestSource("cannot be blank", "/data/attributes/"+name)
	}

	return &value, true, nil
}

// nullableStringAttr reads a string attribute that may be null.
func nullableStringAttr(attrs map[string]any, name string) (*string, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return nil, false, nil
	}

	if raw == nil {
		return nil, true, nil
	}

	value, err := str(raw, "/data/attributes/"+name)

	return &value, true, err
}

// intAttr reads an integer attribute (nil allowed: clearing).
func intAttr(attrs map[string]any, name string) (*int32, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return nil, false, nil
	}

	if raw == nil {
		return nil, true, nil
	}

	value, err := int64Of(raw, "/data/attributes/"+name)
	if err != nil {
		return nil, true, err
	}

	if value > 2147483647 || value < -2147483648 {
		return nil, true, apperr.BadRequestSource(
			"type mismatch (received integer expected integer)", "/data/attributes/"+name)
	}

	v := int32(value)

	return &v, true, nil
}

// bigintAttr reads a 64-bit integer attribute (nil allowed).
func bigintAttr(attrs map[string]any, name string) (*int64, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return nil, false, nil
	}

	if raw == nil {
		return nil, true, nil
	}

	value, err := int64Of(raw, "/data/attributes/"+name)
	if err != nil {
		return nil, true, err
	}

	return &value, true, nil
}

// boolAttr reads a boolean attribute.
func boolAttr(attrs map[string]any, name string) (*bool, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return nil, false, nil
	}

	if raw == nil {
		return nil, true, apperr.BadRequestSource(
			"type mismatch (received null expected boolean)", "/data/attributes/"+name)
	}

	value, ok := raw.(bool)
	if !ok {
		return nil, true, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(raw)+" expected boolean)", "/data/attributes/"+name)
	}

	return &value, true, nil
}

// metadataAttr reads the metadata hash (nil allowed: clearing).
func metadataAttr(attrs map[string]any) (map[string]any, bool, error) {
	raw, ok := attrs["metadata"]
	if !ok || raw == nil {
		return nil, ok, nil
	}

	meta, err := hash(raw, "/data/attributes/metadata")

	return meta, true, err
}

// stringsAttr reads an array-of-strings attribute (nil allowed).
func stringsAttr(attrs map[string]any, name string) ([]string, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return nil, false, nil
	}

	if raw == nil {
		return nil, true, nil
	}

	list, ok := raw.([]any)
	if !ok {
		return nil, true, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(raw)+" expected array)", "/data/attributes/"+name)
	}

	out := make([]string, 0, len(list))
	for _, item := range list {
		value, ok := item.(string) //nolint:govet // mechanical
		if !ok {
			return nil, true, apperr.BadRequestSource(
				"type mismatch (received "+jsonKind(item)+" expected string)", "/data/attributes/"+name)
		}

		out = append(out, value)
	}

	return out, true, nil
}

// int64Of coerces a decoded JSON integer.
func int64Of(raw any, pointer string) (int64, error) {
	value, ok := raw.(float64)
	if !ok || value != trunc(value) {
		return 0, apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(raw)+" expected integer)", pointer)
	}

	return int64(value), nil
}

// nonBlankStringAttr reads an optional string attribute whose blank
// value the typed params reject ("cannot be blank").
func nonBlankStringAttr(attrs map[string]any, name string) (*string, bool, error) {
	raw, ok := attrs[name]
	if !ok {
		return nil, false, nil
	}

	if raw == nil {
		return nil, true, apperr.BadRequestSource("cannot be blank", "/data/attributes/"+name)
	}

	value, err := str(raw, "/data/attributes/"+name)
	if err != nil {
		return nil, true, err
	}

	if value == "" {
		return nil, true, apperr.BadRequestSource("cannot be blank", "/data/attributes/"+name)
	}

	return &value, true, nil
}
