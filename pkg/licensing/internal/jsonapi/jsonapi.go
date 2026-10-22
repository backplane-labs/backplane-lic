// Package jsonapi is the licensing client's private JSON:API transport:
// the document envelope, request encoding, and response decoding into the
// client's typed structs. It is intentionally small — only what the Keygen
// licensing endpoints emit and consume.
package jsonapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ContentType is the JSON:API media type Keygen speaks.
const ContentType = "application/vnd.api+json"

// Identifier is a resource object identifier: {type, id}.
type Identifier struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Relationship is a JSON:API relationship object. Data is polymorphic on
// the wire (null, a single identifier, or an array with embedded
// attributes), so it is typed as any on the encode side and re-decoded
// through RelID on the read side.
type Relationship struct {
	Data any `json:"data,omitempty"`
}

// Resource is a JSON:API resource object. Attributes stay raw so each
// caller decodes them into its own typed struct.
type Resource struct {
	ID            string                  `json:"id"`
	Type          string                  `json:"type"`
	Attributes    json.RawMessage         `json:"attributes,omitempty"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
}

// RelID returns the identifier of a to-one relationship, or "" when the
// relationship is absent, null, or malformed.
func (r Resource) RelID(name string) string {
	rel, ok := r.Relationships[name]
	if !ok || rel.Data == nil {
		return ""
	}

	raw, err := json.Marshal(rel.Data)
	if err != nil {
		return ""
	}

	var id Identifier
	if err := json.Unmarshal(raw, &id); err != nil {
		return ""
	}

	return id.ID
}

// Source points at the offending part of the request.
type Source struct {
	Pointer   string `json:"pointer,omitempty"`
	Parameter string `json:"parameter,omitempty"`
	Header    string `json:"header,omitempty"`
}

// Error is one member of a JSON:API errors array.
type Error struct {
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code,omitempty"`
	Source Source `json:"source,omitempty"`
}

// Document is the JSON:API envelope: single or collection data, meta,
// included resources, and errors.
type Document struct {
	Data     json.RawMessage `json:"data,omitempty"`
	Meta     json.RawMessage `json:"meta,omitempty"`
	Included []Resource      `json:"included,omitempty"`
	Errors   []Error         `json:"errors,omitempty"`
}

// Parse decodes a response body into a Document.
func Parse(body []byte) (Document, error) {
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return Document{}, fmt.Errorf("jsonapi: decode document: %w", err)
	}

	return doc, nil
}

// One returns the single resource in data. An absent or null data renders
// as a zero Resource with an empty Type.
func (d Document) One() (Resource, error) {
	if len(d.Data) == 0 || strings.EqualFold(strings.TrimSpace(string(d.Data)), "null") {
		return Resource{}, nil
	}

	var res Resource
	if err := json.Unmarshal(d.Data, &res); err != nil {
		return Resource{}, fmt.Errorf("jsonapi: decode data: %w", err)
	}

	return res, nil
}

// Many returns the collection in data as resources.
func (d Document) Many() ([]Resource, error) {
	if len(d.Data) == 0 || strings.EqualFold(strings.TrimSpace(string(d.Data)), "null") {
		return nil, nil
	}

	var res []Resource
	if err := json.Unmarshal(d.Data, &res); err != nil {
		return nil, fmt.Errorf("jsonapi: decode data: %w", err)
	}

	return res, nil
}

// DecodeAttributes decodes a resource's attributes into target.
func DecodeAttributes(res Resource, target any) error {
	if len(res.Attributes) == 0 {
		return nil
	}

	if err := json.Unmarshal(res.Attributes, target); err != nil {
		return fmt.Errorf("jsonapi: decode %s attributes: %w", res.Type, err)
	}

	return nil
}

// DecodeMeta decodes the document's meta object into target.
func (d Document) DecodeMeta(target any) error {
	if len(d.Meta) == 0 {
		return nil
	}

	if err := json.Unmarshal(d.Meta, target); err != nil {
		return fmt.Errorf("jsonapi: decode meta: %w", err)
	}

	return nil
}

// Body is a request body under construction: a single resource with
// attributes, relationships, and an optional top-level meta object.
type Body struct {
	Type          string
	ID            string
	Attributes    any
	Relationships map[string]Relationship
	Meta          any
}

type requestBody struct {
	Data any `json:"data,omitempty"`
	Meta any `json:"meta,omitempty"`
}

type requestResource struct {
	ID            string                  `json:"id,omitempty"`
	Type          string                  `json:"type"`
	Attributes    any                     `json:"attributes,omitempty"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
}

// Encode renders the body as a JSON:API document.
func (b Body) Encode() ([]byte, error) {
	res := requestResource{
		ID:            b.ID,
		Type:          b.Type,
		Attributes:    b.Attributes,
		Relationships: b.Relationships,
	}

	if len(b.Relationships) == 0 {
		res.Relationships = nil
	}

	out, err := json.Marshal(requestBody{Data: res, Meta: b.Meta})
	if err != nil {
		return nil, fmt.Errorf("jsonapi: encode document: %w", err)
	}

	return out, nil
}
