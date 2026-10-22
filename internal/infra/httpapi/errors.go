package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// request carries everything the handlers and the finalizers learned
// about the request while the pipeline walked it. It rides the context.
type request struct {
	id uuid.UUID
	// account is the resolved account (zero value until resolution).
	account domain.Account
	// principal is the authenticated caller (zero when anonymous).
	principal access.Principal
	// token is the authentication's token row (nil for password and
	// license-key authentication).
	token *domain.Token
	// challenge is the WWW-Authenticate scheme for a later 401: the
	// scheme the client used, or Bearer.
	challenge string
	// body is the decoded, key-underscored request body (POST only).
	body map[string]any
	// query is the request's parsed query string (filters,
	// pagination, the environment parameter).
	query url.Values
	// headers carries the request headers the handlers consult (the
	// EE-only Keygen-Environment selector).
	headers http.Header
	// method is the request method (the signature's request-target).
	method string
	// originalURI is the path (with query) exactly as the client sent
	// it: what the response signature signs and self-links build on.
	originalURI string
	// host is the request host without the port (the signature's host
	// line); scheme feeds absolute links.
	host   string
	scheme string
	// docsBase is the origin of the docs links, e.g.
	// https://keygen.test.
	docsBase string
}

type requestKey struct{}

// requestOf loads the pipeline's request from the context (a zero value
// when absent — e.g. unit tests calling handlers directly).
func requestOf(ctx context.Context) *request {
	if r, ok := ctx.Value(requestKey{}).(*request); ok {
		return r
	}

	return &request{challenge: "Bearer"}
}

// with stores the request in the context.
func (r *request) with(ctx context.Context) context.Context {
	return context.WithValue(ctx, requestKey{}, r)
}

// location builds an absolute URL like the reference's route helpers:
// scheme://host/path.
func (r *request) location(path string) string {
	return r.scheme + "://" + r.host + path
}

// apperrFrom extracts the typed app error, replacing anything else
// (storage failures included) with the 500.
func apperrFrom(err error) *apperr.T {
	return apperr.From(err)
}

// errorStatus renders a typed app error as the ogen error wrapper.
func errorStatus(t *apperr.T, req *request) *oas.ErrorStatusCode {
	doc := oas.ErrorDocument{Meta: oas.ErrorDocumentMeta{ID: req.id.String()}}

	first := errorOf(t, req)
	doc.Errors = append(doc.Errors, first)

	for _, sibling := range t.Siblings() {
		doc.Errors = append(doc.Errors, errorOf(sibling, req))
	}

	return &oas.ErrorStatusCode{StatusCode: t.Status, Response: doc}
}

func errorOf(t *apperr.T, req *request) oas.Error {
	e := oas.Error{Title: t.Title, Detail: t.Detail}

	if t.Code != "" {
		// The reference omits the key on code-less errors (the typed
		// params 400s); the zero value omits it.
		e.Code = oas.NewOptNilString(t.Code)
	}

	if t.Source.Pointer != "" {
		e.Source = oas.NewOptErrorSource(
			oas.ErrorSource{Pointer: oas.NewOptString(t.Source.Pointer)},
		)
	}

	if t.Source.Parameter != "" {
		e.Source = oas.NewOptErrorSource(
			oas.ErrorSource{Parameter: oas.NewOptString(t.Source.Parameter)},
		)
	}

	if t.Source.Header != "" {
		e.Source = oas.NewOptErrorSource(
			oas.ErrorSource{Header: oas.NewOptString(t.Source.Header)},
		)
	}

	if t.About != "" {
		e.Links = oas.NewOptErrorLinks(
			oas.ErrorLinks{About: oas.NewOptString(req.docsBase + "/docs/api/" + t.About)},
		)
	}

	return e
}

// writeJSON writes a pre-rendered JSON:API document with the given
// status.
func writeJSON(w http.ResponseWriter, status int, doc any) {
	w.Header().Set("Content-Type", "application/vnd.api+json; charset=utf-8")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	_ = enc.Encode(doc) //nolint:errchkjson // nothing to do when the peer is gone
}
