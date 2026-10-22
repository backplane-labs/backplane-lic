// Package apperr is the typed application error: everything a handler
// needs to render a Keygen error document (status, title, detail, code,
// source pointer/parameter/header, docs link).
package apperr

import (
	"errors"
	"net/http"
)

// The canonical titles.
const (
	TitleBadRequest    = "Bad request"
	TitleUnauthorized  = "Unauthorized"
	TitleForbidden     = "Access denied"
	TitleNotFound      = "Not found"
	TitleConflict      = "Conflict"
	TitleUnprocessable = "Unprocessable entity"
	// TitleUnprocessableResource is the model-validation 422 title
	// ("Unprocessable resource", with code and pointer).
	TitleUnprocessableResource = "Unprocessable resource"
	TitleTooManyRequests       = "Too many requests"
	TitleInternalServerError   = "Internal server error"
)

// The canonical default details.
const (
	detailBadRequest    = "The request could not be completed"
	detailUnauthorized  = "You must be authenticated to complete the request"
	detailForbidden     = "You do not have permission to complete the request"
	detailNotFound      = "The requested endpoint was not found (check your HTTP method, Accept header, and URL path)"
	detailConflict      = "The request could not be completed because of a conflict"
	detailUnprocessable = "The request could not be completed"
	detailInternal      = "Looks like something went wrong! Our engineers have been notified. " +
		"If you continue to have problems, please contact support@keygen.sh."
)

// Source points at what caused the error: a JSON pointer into the
// request body, a query parameter, or a request header.
type Source struct {
	Pointer   string
	Parameter string
	Header    string
}

// T is a typed application error. About is a docs topic resolved against
// the installation's docs host at render time (the reference builds
// https://<keygen-domain>/docs/api/... links).
type T struct {
	Status int
	Title  string
	Detail string
	Code   string
	Source Source
	// About is the docs topic after "/docs/api/", e.g.
	// "authentication/#token-authentication"; empty for none.
	About string
	// siblings are the remaining errors of a multi-error document
	// (MultiOf); nil for a single one.
	siblings []*T
}

// Siblings answers the additional errors carried alongside this one.
func (t *T) Siblings() []*T { return t.siblings }

// Error implements error.
func (t *T) Error() string {
	return t.Title + ": " + t.Detail
}

// BadRequest is a 400 without a code (typed-params style).
func BadRequest(detail string) *T {
	return &T{Status: http.StatusBadRequest, Title: TitleBadRequest, Detail: detail}
}

// BadRequestSource is a 400 with a source pointer.
func BadRequestSource(detail, pointer string) *T {
	return &T{Status: http.StatusBadRequest, Title: TitleBadRequest, Detail: detail, Source: Source{Pointer: pointer}}
}

// Unauthorized builds a 401: code with its default or given detail, and
// the authentication docs topic.
func Unauthorized(code, detail string) *T {
	if detail == "" {
		detail = detailUnauthorized
	}

	return &T{
		Status: http.StatusUnauthorized,
		Title:  TitleUnauthorized,
		Detail: detail,
		Code:   code,
		About:  "authentication/",
	}
}

// TokenUnauthorized is a 401 whose docs link targets token
// authentication specifically (the bare TOKEN_MISSING keeps the generic
// authentication link, like the reference).
func TokenUnauthorized(code, detail string) *T {
	t := Unauthorized(code, detail)
	if code != "TOKEN_MISSING" {
		t.About = "authentication/#token-authentication"
	}

	return t
}

// UnauthorizedSource is a 401 with a source header (password flow) or a
// body pointer (the OTP errors).
func UnauthorizedSource(code, detail, header, pointer string) *T {
	t := Unauthorized(code, detail)
	t.Source = Source{Header: header, Pointer: pointer}

	// The password flow's errors carry no docs link (the reference's
	// email/password 401s render bare).
	t.About = ""

	return t
}

// Forbidden builds a 403 with a code (LICENSE_BANNED, USER_BANNED, ...).
func Forbidden(code, detail string) *T {
	return &T{Status: http.StatusForbidden, Title: TitleForbidden, Detail: detail, Code: code}
}

// ForbiddenReason is the permission-denied 403: the default detail with
// the policy's inline reason appended, e.g.
// "(ensure the token or license is allowed to access all resources)".
func ForbiddenReason(reason string) *T {
	return &T{Status: http.StatusForbidden, Title: TitleForbidden, Detail: detailForbidden + reason}
}

// NotFoundDefault is the endpoint-not-found 404.
func NotFoundDefault() *T {
	return &T{Status: http.StatusNotFound, Title: TitleNotFound, Detail: detailNotFound, Code: "NOT_FOUND"}
}

// NotFound is a record-not-found 404: "The requested <resource> '<id>'
// was not found".
func NotFound(resource, id string) *T {
	return &T{
		Status: http.StatusNotFound,
		Title:  TitleNotFound,
		Detail: "The requested " + resource + " '" + id + "' was not found",
		Code:   "NOT_FOUND",
	}
}

// ForbiddenIndex is the codeless indexed-pointer 403 of the detach
// guards ("cannot detach user '<uuid>' (…)").
func ForbiddenIndex(detail, pointer string) *T {
	return &T{
		Status: http.StatusForbidden, Title: TitleForbidden, Detail: detail,
		Source: Source{Pointer: pointer},
	}
}

// UnprocessableNoCode is the codeless 422 of the detach guards
// ("cannot detach entitlement '<uuid>' (…)", title "Unprocessable
// entity").
func UnprocessableNoCode(detail, pointer string) *T {
	return &T{
		Status: http.StatusUnprocessableEntity, Title: TitleUnprocessable, Detail: detail,
		Source: Source{Pointer: pointer},
	}
}

// Conflict is a 409.
func Conflict(detail string) *T {
	return &T{Status: http.StatusConflict, Title: TitleConflict, Detail: detail}
}

// Unprocessable builds a 422 (validation-style errors: code, detail,
// pointer) with the "Unprocessable resource" title the reference's
// render_unprocessable_resource emits.
func Unprocessable(code, detail, pointer string) *T {
	return &T{
		Status: http.StatusUnprocessableEntity,
		Title:  "Unprocessable resource",
		Detail: detail,
		Code:   code,
		Source: Source{Pointer: pointer},
	}
}

// Internal is the 500.
func Internal() *T {
	return &T{Status: http.StatusInternalServerError, Title: TitleInternalServerError, Detail: detailInternal}
}

// From extracts a *T, replacing anything else with the 500.
func From(err error) *T {
	var t *T

	if errors.As(err, &t) {
		return t
	}

	return Internal()
}

// Validation builds a model-validation 422 ("Unprocessable resource",
// with code, pointer and the docs link of the attribute or
// relationship the violation points at).
func Validation(code, detail, pointer, about string) *T {
	return &T{
		Status: http.StatusUnprocessableEntity,
		Title:  TitleUnprocessableResource,
		Detail: detail,
		Code:   code,
		Source: Source{Pointer: pointer},
		About:  about,
	}
}

// MultiOf carries several typed errors in one document (the reference's
// multi-violation 422s, e.g. KEY_NOT_ALLOWED and KEY_TOO_SHORT side by
// side): the first error, with the rest riding as siblings. Every
// carried error shares the first one's status and title.
func MultiOf(errs ...*T) error {
	if len(errs) == 0 {
		return nil
	}

	first := *errs[0]
	first.siblings = errs[1:]

	return &first
}
