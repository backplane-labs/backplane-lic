package domain

// Violation is one model-validation failure: the error code the wire
// renders, its verbatim detail, and the attribute or relationship it
// points at. The scenarios translate violations into the 422 document.
type Violation struct {
	Code   string
	Detail string
	// Attr is the wire attribute name (camelCase); Rel the wire
	// relationship name when the violation points at a relationship.
	Attr string
	Rel  string
}

// Pointer is the JSON pointer the violation points at.
func (v Violation) Pointer() string {
	if v.Rel != "" {
		return "/data/relationships/" + v.Rel
	}

	return "/data/attributes/" + v.Attr
}
