// Package paging parses the three pagination modes (limit, offset,
// keyset) and the order parameter exactly like the reference, and builds
// the collection links (with pagination meta nested in links).
package paging

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
)

// Mode is the pagination mode a request selected.
type Mode int

// The modes.
const (
	// ModeLimit is the default: ?limit=N (or 10), no links.
	ModeLimit Mode = iota
	// ModeOffset is page[number]+page[size]: full links + meta.
	ModeOffset
	// ModeCursor is page[cursor]+page[size]: self/next links.
	ModeCursor
)

// Bounds shared by limit and page[size].
const (
	minLimit   = 1
	maxLimit   = 100
	maxPage    = 100
	defaultSiz = 10
)

// Order values (the default is desc, the production default).
const (
	OrderAsc  = "asc"
	OrderDesc = "desc"
)

// Params is the parsed pagination of a request.
type Params struct {
	Mode   Mode
	Limit  int
	Number int
	Size   int
	// Cursor is the keyset cursor; Valid false in the other modes.
	Cursor uuid.NullUUID
	Order  string
}

// Parse reads limit, page[number], page[size], page[cursor] and order
// from the query. Errors are the reference's 400 documents with
// source.parameter set.
func Parse(query url.Values) (Params, error) {
	p := Params{Mode: ModeLimit, Limit: defaultSiz, Order: OrderDesc}

	if err := parseLimit(query, &p); err != nil {
		return p, err
	}

	if err := parsePage(query, &p); err != nil {
		return p, err
	}

	if err := parseOrder(query, &p); err != nil {
		return p, err
	}

	return p, nil
}

func parseLimit(q url.Values, p *Params) error {
	raw := q.Get("limit")
	if raw == "" {
		return nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < minLimit || n > maxLimit {
		return parameter("limit", "limit must be a number between 1 and 100 (got %s)", raw)
	}

	p.Limit = n

	return nil
}

func parseOrder(q url.Values, p *Params) error {
	raw := q.Get("order")
	if raw == "" {
		return nil
	}

	switch strings.ToLower(raw) {
	case OrderAsc, OrderDesc:
		p.Order = strings.ToLower(raw)

		return nil
	default:
		return parameter("order", "order is invalid")
	}
}

// pageKeys answers which page[...] parameters the query carries; an
// unknown one is the reference's generic page error.
func pageKeys(query url.Values) (bool, bool, bool, error) {
	var (
		hasNumber bool
		hasCursor bool
		hasSize   bool
	)

	for key := range query {
		if !strings.HasPrefix(key, "page[") || !strings.HasSuffix(key, "]") {
			continue
		}

		switch key {
		case "page[number]":
			hasNumber = true
		case "page[size]":
			hasSize = true
		case "page[cursor]":
			hasCursor = true
		default:
			return false, false, false, pageError()
		}
	}

	return hasNumber, hasCursor, hasSize, nil
}

func pageError() error {
	return parameter("page", "page must be an object containing a cursor or number and optional size")
}

func parsePage(query url.Values, p *Params) error {
	hasNumber, hasCursor, hasSize, err := pageKeys(query)
	if err != nil {
		return err
	}

	switch {
	case hasNumber && hasCursor:
		return pageError()

	case hasNumber:
		p.Mode = ModeOffset

		return applyNumber(query, p, hasSize)

	case hasCursor:
		p.Mode = ModeCursor

		return applyCursor(query, p, hasSize)

	case hasSize:
		// page[size] alone selects no mode; the limit applies.
		return nil
	}

	return nil
}

func applyNumber(query url.Values, p *Params, hasSize bool) error {
	raw := query.Get("page[number]")

	n, err := strconv.Atoi(raw)
	if err != nil || n < minLimit {
		return parameter("page[number]", "page number must be a number between 1 and 100 (got %s)", raw)
	}

	if n > maxPage {
		return parameter(
			"page[number]",
			"page number must be a number between 1 and 100 (got %s, please use cursor-based pagination instead)",
			raw,
		)
	}

	p.Number = n

	return applySize(query, p, hasSize)
}

func applyCursor(query url.Values, p *Params, hasSize bool) error {
	if raw := query.Get("page[cursor]"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return parameter("page[cursor]", "page cursor must be a valid UUID (got %q)", raw)
		}

		p.Cursor = uuid.NullUUID{UUID: id, Valid: true}
	}

	return applySize(query, p, hasSize)
}

func applySize(q url.Values, p *Params, has bool) error {
	size, err := parseSize(q, p.Limit, has)
	if err != nil {
		return err
	}

	p.Size = size

	return nil
}

// SizeOrDefault answers the page size (limit mode included).
func (p Params) SizeOrDefault() int {
	if p.Mode == ModeLimit || p.Size == 0 {
		return p.Limit
	}

	return p.Size
}

// Doc is a collection's links document: the four offset links (prev/next
// nil at the ends) with pagination meta nested inside, or the keyset
// self/next pair.
type Doc struct {
	Self  string
	Prev  *string
	Next  *string
	First *string
	Last  *string
	// Meta is nil in cursor mode.
	Meta *Meta
}

// Meta is the nested pagination meta (offset mode only).
type Meta struct {
	Pages int
	Count int
}

// OffsetDoc builds the offset-mode links against the request's original
// path (the account form the caller used), with page[number] and
// page[size] as the only query parameters.
func OffsetDoc(path string, number, size, pages, count int) Doc {
	link := func(n int) string {
		q := url.Values{}
		q.Set("page[number]", strconv.Itoa(n))
		q.Set("page[size]", strconv.Itoa(size))

		return path + "?" + q.Encode()
	}

	first, last := link(1), link(max(pages, 0))

	doc := Doc{
		Self:  link(number),
		First: &first,
		Last:  &last,
		Meta:  &Meta{Pages: pages, Count: count},
	}

	if number > 1 && number <= pages {
		prev := link(number - 1)
		doc.Prev = &prev
	}

	if number < pages {
		next := link(number + 1)
		doc.Next = &next
	}

	return doc
}

// CursorDoc builds the keyset-mode links: self (retaining the cursor and
// the size) and next (the last record's id), nil when the page ran out.
func CursorDoc(path, cursor string, size int, next uuid.NullUUID) Doc {
	self := url.Values{}
	if cursor != "" {
		self.Set("page[cursor]", cursor)
	}

	self.Set("page[size]", strconv.Itoa(size))

	doc := Doc{Self: path + "?" + self.Encode()}

	if next.Valid {
		link := path + "?" + url.Values{
			"page[cursor]": []string{next.UUID.String()},
			"page[size]":   []string{strconv.Itoa(size)},
		}.Encode()
		doc.Next = &link
	}

	return doc
}

func parseSize(q url.Values, def int, has bool) (int, error) {
	if !has {
		return def, nil
	}

	raw := q.Get("page[size]")

	n, err := strconv.Atoi(raw)
	if err != nil || n < minLimit || n > maxLimit {
		return 0, parameter("page[size]", "page size must be a number between 1 and 100 (got %s)", raw)
	}

	return n, nil
}

func parameter(param, format string, args ...any) error {
	return &apperr.T{
		Status: http.StatusBadRequest,
		Title:  "Bad request",
		Detail: fmt.Sprintf(format, args...),
		Source: apperr.Source{Parameter: param},
	}
}
