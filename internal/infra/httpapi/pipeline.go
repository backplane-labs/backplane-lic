// The middleware pipeline of the architecture contract: request id,
// bare-path rewrite, routing (Keygen 404), content negotiation, API
// version check, account resolution, body decoding, the password
// throttle, authentication, the default header set and the response
// signature.
package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/authn"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// The canonical default header values.
const (
	editionCE        = "CE"
	modeSingleplayer = "singleplayer"
	apiVersion       = "1.8"
	poweredBy        = "Ruby, Rails, and a lot of coffee. (And the occasional Islay.)"
	jsonapiCT        = "application/vnd.api+json; charset=utf-8"

	// passwordLimit is the reference's "req/ip/auth" throttle: 5
	// attempts per minute.
	passwordLimit  = 5
	passwordWindow = time.Minute

	// maxBodyBytes caps the request body before decoding.
	maxBodyBytes = 1 << 20
	// registrableLabels is the label count of a registrable domain (the
	// docs host is derived from the last two).
	registrableLabels = 2
)

// pipeline is the wrapper around the ogen server.
type pipeline struct {
	server         *oas.Server
	accounts       Accounts
	authn          authn.Scenario
	spec           Spec
	revision       string
	docsHost       string
	throttle       Throttler
	trustedProxies []*net.IPNet
	clock          func() time.Time
}

// ServeHTTP walks the request through the pipeline.
func (p *pipeline) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req := &request{
		id:          newRequestID(),
		challenge:   "Bearer",
		method:      r.Method,
		query:       r.URL.Query(),
		headers:     r.Header,
		originalURI: r.URL.RequestURI(),
		host:        hostWithoutPort(r.Host),
		scheme:      requestScheme(r),
		docsBase:    p.docsBase(r),
	}

	routePath := rewriteBare(r.URL.Path, p.spec)
	accountScoped := strings.HasPrefix(routePath, "/v1/accounts/")

	route, ok := p.server.FindRoute(r.Method, routePath)
	if !ok {
		// No route for this method+path: the reference's endpoint-level
		// 404 (it has no distinct 405).
		p.fail(w, req, apperr.NotFoundDefault(), "")

		return
	}

	switch route.Name() {
	case oas.PingOperation, oas.HealthOperation:
		p.servePlain(w, r, req, route.Name())

		return
	case oas.GenerateTokenOperation:
		p.serveAccountScoped(w, r, req, routePath, accountScoped, authModePasswordOrToken)

		return
	case oas.RequestPasswordResetOperation:
		// POST /passwords never authenticates (skip_verify_authorized).
		p.serveAccountScoped(w, r, req, routePath, accountScoped, authModeNone)

		return
	case oas.CreateUserOperation, oas.ResetUserPasswordOperation:
		// POST /users and the anonymous password reset authenticate
		// softly: an absent credential leaves an anonymous principal
		// for the policy to judge.
		p.serveAccountScoped(w, r, req, routePath, accountScoped, authModeOptional)

		return
	case oas.ValidateLicenseKeyOperation:
		// POST /licenses/actions/validate-key authenticates softly
		// (anonymous validation is allowed).
		p.serveAccountScoped(w, r, req, routePath, accountScoped, authModeOptional)

		return
	default:
		p.serveAccountScoped(w, r, req, routePath, accountScoped, authModeRequired)

		return
	}
}

// servePlain runs the ping/health routes: no negotiation, no account,
// no authentication, no signature — and no Date at all.
func (p *pipeline) servePlain(
	w http.ResponseWriter, r *http.Request, req *request, operation string,
) {
	rec := &recorder{header: http.Header{}}
	p.server.ServeHTTP(rec, r.WithContext(req.with(r.Context())))

	h := w.Header()
	copyHeader(h, rec.header)

	if operation == oas.PingOperation {
		h.Set("Content-Type", "text/plain; charset=utf-8")
	}

	p.addDefaults(h, req)
	h["Date"] = nil

	w.WriteHeader(rec.status())

	if operation == oas.PingOperation {
		_, _ = w.Write([]byte(" "))
	}
}

// The authentication modes of the account-scoped routes: how the
// pipeline treats the request's credential.
type authMode int

const (
	// authModeRequired: authenticate_with_token! — a missing or invalid
	// credential fails the request.
	authModeRequired authMode = iota
	// authModeOptional: authenticate_with_token (soft) — an ABSENT
	// credential leaves an anonymous principal; an invalid one still
	// fails.
	authModeOptional
	// authModeNone: the endpoint never authenticates (POST /passwords).
	authModeNone
	// authModePasswordOrToken: authenticate_with_password_or_token!
	// (POST /tokens).
	authModePasswordOrToken
)

// hasBody reports whether the method carries a request body at all.
func hasBody(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// serveAccountScoped runs an account-scoped route: the whole pipeline.
func (p *pipeline) serveAccountScoped(
	w http.ResponseWriter,
	r *http.Request,
	req *request,
	routePath string,
	accountScoped bool,
	mode authMode,
) {
	account, alg, ok := p.resolve(w, r, req, routePath)
	if !ok {
		return
	}

	// The password throttle: 5 attempts per minute per account segment,
	// IP and email, before anything else touches the body.
	if mode == authModePasswordOrToken && accountScoped && p.throttled(r, routePath) {
		p.throttleError(w, p.clock())

		return
	}

	cred := authn.Parse(r.Header.Get("Authorization"), queryCredential(r))

	otp := ""

	if hasBody(r.Method) {
		body, err := decodeBody(r)
		if err != nil {
			p.middlewareError(w, req.id, errJSONInvalid)

			return
		}

		req.body = body
		otp = bodyOTP(body)
	}

	var (
		principal access.Principal
		result    authn.Result
		err       error
	)

	authnMode := authn.ModeToken
	if mode == authModePasswordOrToken {
		authnMode = authn.ModePasswordOrToken
	}

	switch {
	case mode == authModeNone:
		// No authentication at all.
	case mode == authModeOptional && cred.Kind == authn.CredentialNone:
		// Soft authentication without a credential: anonymous principal.
	default:
		principal, result, err = p.authn.Authenticate(r.Context(), account, cred, authnMode, otp)
		if err != nil {
			req.challenge = challenge(cred)
			p.fail(w, req, apperrFrom(err), alg)

			return
		}
	}

	req.principal = principal
	req.token = result.Token
	req.challenge = challenge(cred)

	ctx := access.With(req.with(r.Context()), principal)

	// The typed body decoder must not see the raw body: the pipeline
	// has already decoded it (any non-empty body is JSON, whatever its
	// content type) and the handler reads it from the request on the
	// context. The spec's empty-body representation keeps ogen happy.
	routed := *r
	routed.URL = newURL(r, routePath)
	routed.Body = http.NoBody
	routed.ContentLength = 0
	routed.Header.Del("Content-Type")

	rec := &recorder{header: http.Header{}}
	p.server.ServeHTTP(rec, routed.WithContext(ctx))

	p.finalize(w, req, rec, alg)
}

// resolve runs the pre-authentication guard chain — negotiation, the
// API version, the account segment and row, the signature algorithm —
// answering the account and algorithm (ok false once the failure has
// been written).
func (p *pipeline) resolve(
	w http.ResponseWriter,
	r *http.Request,
	req *request,
	routePath string,
) (domain.Account, string, bool) {
	if !acceptable(r.Header) {
		p.middlewareError(w, req.id, errMimeInvalid)

		return domain.Account{}, "", false
	}

	if err := checkAPIVersion(r.Header.Get("Keygen-Version")); err != nil {
		p.versionError(w, req.id)

		return domain.Account{}, "", false
	}

	seg := accountSegment(routePath)
	if seg != p.spec.ID.String() && !strings.EqualFold(seg, p.spec.Slug) {
		p.fail(w, req, apperr.NotFound("account", seg), "")

		return domain.Account{}, "", false
	}

	account, err := p.accounts.Account(r.Context(), p.spec.ID)
	if err != nil {
		p.fail(w, req, apperr.Internal(), "")

		return domain.Account{}, "", false
	}

	req.account = account

	// The signature algorithm is validated as soon as the account is
	// known; an invalid header both fails the request and would leave
	// nothing to sign with.
	alg, err := crypto.ParseAcceptSignature(r.Header.Get("Keygen-Accept-Signature"), account.ID)
	if err != nil {
		p.fail(w, req, acceptSignatureError(err), "")

		return domain.Account{}, "", false
	}

	return account, alg, true
}

// finalize writes the recorded response with the default header set,
// the signature and the 401 challenge.
func (p *pipeline) finalize(w http.ResponseWriter, req *request, rec *recorder, alg string) {
	h := w.Header()
	copyHeader(h, rec.header)

	if h.Get("Content-Type") == "application/vnd.api+json" {
		h.Set("Content-Type", jsonapiCT)
	}

	p.addDefaults(h, req)
	p.sign(h, rec.buf.Bytes(), req, alg)

	if rec.status() == http.StatusUnauthorized {
		h.Set("WWW-Authenticate", req.challenge+` realm="keygen"`)
	}

	w.WriteHeader(rec.status())
	_, _ = w.Write(rec.buf.Bytes())
}

// sign puts the response signature headers on the response: Date,
// Digest, Keygen-Signature and their Keygen- mirrors, over the ORIGINAL
// request path and the host without port. Skipped (no headers, and no
// Date at all) without an account or a valid algorithm.
func (p *pipeline) sign(h http.Header, body []byte, req *request, alg string) {
	if req.account.ID == uuid.Nil || alg == "" {
		h["Date"] = nil

		return
	}

	digest := crypto.Digest(body)
	date := p.clock().UTC().Format(http.TimeFormat)

	sig, err := crypto.Sign(req.account.Keys, alg, crypto.SigningData(
		req.method, req.originalURI, req.host, date, digest,
	))
	if err != nil {
		// Like the reference's rescue: sign nothing on failure.
		h["Date"] = nil

		return
	}

	h.Set("Date", date)
	h.Set("Digest", digest)
	h.Set("Keygen-Signature", fmt.Sprintf(
		`keyid=%q, algorithm=%q, signature=%q, headers="(request-target) host date digest"`,
		req.account.ID.String(), alg, sig,
	))
	h.Set("Keygen-Date", date)
	h.Set("Keygen-Digest", digest)
}

// addDefaults writes the reference's default header set (everything it
// sends even without a signature).
func (p *pipeline) addDefaults(h http.Header, req *request) {
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("X-XSS-Protection", "0")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Permitted-Cross-Domain-Policies", "none")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

	reportTo := `{ "group": "csp-reports", "max_age": 10886400, "endpoints": ` +
		`[{ "url": "` + req.scheme + "://" + req.host + `/-/csp-reports" }] }`
	h.Set("Report-To", reportTo)
	h.Set("Content-Security-Policy", "default-src 'none'; report-uri /-/csp-reports; report-to csp-reports;")
	h.Set("Cache-Control", "no-store")

	account, bearer, token := "", "", ""
	if req.account.ID != uuid.Nil {
		account = req.account.ID.String()
	}

	if req.principal.BearerID != uuid.Nil {
		bearer = req.principal.BearerID.String()
	}

	if req.token != nil {
		token = req.token.ID.String()
	}

	h.Set("Keygen-Account", account)
	h.Set("Keygen-Bearer", bearer)
	h.Set("Keygen-Token", token)
	h.Set("Keygen-Environment", "")
	h.Set("Keygen-License", "")
	h.Set("Keygen-Edition", editionCE)
	h.Set("Keygen-Mode", modeSingleplayer)
	h.Set("Keygen-Revision", p.revision)
	h.Set("Keygen-Version", apiVersion)
	h.Set("X-Powered-By", poweredBy)
	h.Set("X-Request-Id", req.id.String())
	h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	h.Set("Vary", "Accept, Origin")
}

// fail renders a controller-level error document: the default header
// set, meta.id, the docs link, and the signature over the exact bytes
// (alg empty leaves the response unsigned).
func (p *pipeline) fail(w http.ResponseWriter, req *request, t *apperr.T, alg string) {
	h := w.Header()
	p.addDefaults(h, req)

	body := errorBytes(errorStatus(t, req))
	p.sign(h, body, req, alg)

	if t.Status == http.StatusUnauthorized {
		h.Set("WWW-Authenticate", req.challenge+` realm="keygen"`)
	}

	h.Set("Content-Type", jsonapiCT)
	w.WriteHeader(t.Status)
	_, _ = w.Write(body)
}

// middlewareError renders a pipeline-level 400 (MIME_TYPE_INVALID,
// JSON_INVALID): no meta, a minimal header set.
func (p *pipeline) middlewareError(w http.ResponseWriter, id uuid.UUID, e *apperr.T) {
	h := w.Header()
	h.Set("Content-Type", jsonapiCT)
	h.Set("X-Request-Id", id.String())
	h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
	h.Set("Vary", "Origin")
	h["Date"] = nil

	writeJSON(w, e.Status, middlewareDoc(e))
}

// versionError renders INVALID_API_VERSION: the middleware set plus the
// security headers (it renders below the default-header action, but
// above the controller).
func (p *pipeline) versionError(w http.ResponseWriter, id uuid.UUID) {
	h := w.Header()
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("X-XSS-Protection", "0")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Permitted-Cross-Domain-Policies", "none")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

	p.middlewareError(w, id, errInvalidVersion)
}

// middlewareDoc is the bare error document the pipeline renders (no
// meta, no links).
func middlewareDoc(e *apperr.T) map[string]any {
	return map[string]any{
		"errors": []map[string]any{{
			"title":  e.Title,
			"detail": e.Detail,
			"code":   e.Code,
		}},
	}
}

// throttleError renders the reference's 429: the rate-limit headers and
// a bare error document, no meta.
func (p *pipeline) throttleError(w http.ResponseWriter, now time.Time) {
	h := w.Header()
	h.Set("Content-Type", jsonapiCT)

	windowStart := now.Truncate(passwordWindow)
	reset := windowStart.Add(passwordWindow)

	h["X-RateLimit-Window"] = []string{"auth"}
	h["X-RateLimit-Count"] = []string{strconv.FormatInt(passwordLimit+1, 10)}
	h["X-RateLimit-Limit"] = []string{strconv.Itoa(passwordLimit)}
	h["X-RateLimit-Remaining"] = []string{"0"}
	h["X-RateLimit-Reset"] = []string{strconv.FormatInt(reset.Unix(), 10)}
	h.Set("Retry-After", strconv.Itoa(int(time.Until(reset).Seconds())+1))
	h["Date"] = nil

	writeJSON(w, http.StatusTooManyRequests, middlewareDoc(errTooManyRequests))
}

// throttled counts a password attempt and reports whether the limit was
// crossed (only POST /tokens with a Basic email:password credential).
func (p *pipeline) throttled(r *http.Request, routePath string) bool {
	if p.throttle == nil ||
		r.Method != http.MethodPost ||
		!strings.HasSuffix(routePath, "/tokens") ||
		!strings.HasPrefix(routePath, "/v1/accounts/") {
		return false
	}

	cred := authn.Parse(r.Header.Get("Authorization"), "")
	if cred.Kind != authn.CredentialBasic {
		return false
	}

	email := cred.Value
	if !strings.Contains(email, "@") || cred.Secret == "" {
		return false
	}

	// The bucket is keyed by the resolved account (never the path
	// segment: the id and the slug are one account), the client IP as
	// resolved through the trusted-proxy chain, and the email hash.
	hash := sha256.Sum256([]byte(strings.ToLower(email)))
	key := p.spec.ID.String() + "/" + p.clientIP(r) + "/" + hex.EncodeToString(hash[:])

	count, err := p.throttle.Inc(r.Context(), key, passwordWindow)

	return err == nil && count > passwordLimit
}

// docsBase answers the docs origin: the configured host or the request
// host's registrable part (its last two labels, like the reference
// derives its domain).
func (p *pipeline) docsBase(r *http.Request) string {
	if p.docsHost != "" {
		return "https://" + p.docsHost
	}

	host := hostWithoutPort(r.Host)
	labels := strings.Split(host, ".")

	if len(labels) > registrableLabels {
		host = strings.Join(labels[len(labels)-registrableLabels:], ".")
	}

	return "https://" + host
}

// decodeBody reads, decodes and key-underscored-transforms the request
// body (any non-empty body is JSON, whatever its content type).
func decodeBody(r *http.Request) (map[string]any, error) {
	if r.Body == nil {
		return map[string]any{}, nil
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		return nil, err //nolint:wrapcheck // read failures are the transport's
	}

	r.Body = io.NopCloser(bytes.NewReader(raw))

	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err //nolint:wrapcheck // the decode error maps to JSON_INVALID
	}

	obj, ok := decoded.(map[string]any)
	if !ok {
		obj = map[string]any{}
	}

	if under, transformed := jsonapi.UnderscoreDeep(obj).(map[string]any); transformed {
		return under, nil
	}

	return obj, nil
}

// bodyOTP pulls meta.otp out of the decoded body (already underscored).
func bodyOTP(body map[string]any) string {
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		return ""
	}

	otp, _ := meta["otp"].(string)

	return otp
}

// rewriteBare maps the singleplayer bare paths (/v1/me, /v1/users/…)
// onto their account-scoped routes. A path outside /v1 has no route at
// all.
func rewriteBare(path string, spec Spec) string {
	switch path {
	case "/v1/ping", "/v1/health", "/v1":
		if path == "/v1" {
			return ""
		}

		return path
	}

	if !strings.HasPrefix(path, "/v1/") {
		return ""
	}

	if strings.HasPrefix(path, "/v1/accounts/") {
		return path
	}

	return "/v1/accounts/" + spec.ID.String() + path[len("/v1"):]
}

// acceptable answers whether the request's Accept header admits the
// JSON:API response (the reference's mime constraint on account-scoped
// routes: jsonapi, json, or a wildcard; a missing header accepts).
func acceptable(h http.Header) bool {
	accept := strings.TrimSpace(h.Get("Accept"))
	if accept == "" {
		return true
	}

	for _, entry := range strings.Split(accept, ",") {
		media, _, _ := strings.Cut(entry, ";")
		media = strings.ToLower(strings.TrimSpace(media))
		media = strings.ToLower(media)

		switch media {
		case "*/*", "application/*", "application/vnd.api+json", "application/json":
			return true
		}
	}

	return false
}

// checkAPIVersion validates the Keygen-Version request header (the
// versions the reference serves, 1.0 through the current).
func checkAPIVersion(header string) error {
	if header == "" {
		return nil
	}

	version := strings.TrimPrefix(strings.TrimSpace(header), "v")

	for _, v := range []string{"1.0", "1.1", "1.2", "1.3", "1.4", "1.5", "1.6", "1.7", "1.8"} {
		if version == v {
			return nil
		}
	}

	return errInvalidVersion
}

// accountSegment extracts the {account} segment of an account-scoped
// path.
func accountSegment(path string) string {
	rest := strings.TrimPrefix(path, "/v1/accounts/")
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}

	return rest
}

// queryCredential pulls the ?token=/?auth= query credential.
func queryCredential(r *http.Request) string {
	q := r.URL.Query()
	if v := q.Get("token"); v != "" {
		return v
	}

	return q.Get("auth")
}

// challenge answers the WWW-Authenticate scheme: the client's own
// scheme, capitalized, or the Bearer default.
func challenge(cred authn.Credentials) string {
	if cred.Scheme == "" {
		return "Bearer"
	}

	return strings.ToUpper(cred.Scheme[:1]) + strings.ToLower(cred.Scheme[1:])
}

// clientIP answers the requesting IP: the remote address, or — only
// when that address lies inside a configured trusted-proxy CIDR — the
// right-most X-Forwarded-For hop outside the trusted set (the first
// hop a spoofing client cannot rewrite). A client rotating a spoofed
// X-Forwarded-For from an untrusted peer never changes the key.
func (p *pipeline) clientIP(r *http.Request) string {
	remote := bareHost(r.RemoteAddr)
	if !p.trustedProxy(remote) {
		return remote
	}

	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := bareHost(hops[i])
		if hop == "" {
			continue
		}

		if !p.trustedProxy(hop) {
			return hop
		}
	}

	return remote
}

// trustedProxy reports whether the address lies inside one of the
// configured proxy CIDRs (empty list: nothing is trusted).
func (p *pipeline) trustedProxy(addr string) bool {
	parsed := net.ParseIP(addr)
	if parsed == nil {
		return false
	}

	for _, cidr := range p.trustedProxies {
		if cidr.Contains(parsed) {
			return true
		}
	}

	return false
}

// bitsPerByte converts an IP's byte length to a prefix length.
const bitsPerByte = 8

// parseCIDRs parses the configured proxy list: each entry a CIDR or a
// bare IP (treated as a single host); unparseable entries are skipped.
func parseCIDRs(entries []string) []*net.IPNet {
	cidrs := make([]*net.IPNet, 0, len(entries))

	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		if _, network, err := net.ParseCIDR(entry); err == nil {
			cidrs = append(cidrs, network)

			continue
		}

		if parsed := net.ParseIP(entry); parsed != nil {
			bits := len(parsed) * bitsPerByte
			cidrs = append(cidrs, &net.IPNet{IP: parsed, Mask: net.CIDRMask(bits, bits)})
		}
	}

	return cidrs
}

// bareHost strips the port of an address, tolerating a bare IP.
func bareHost(addr string) string {
	addr = strings.TrimSpace(addr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}

	return addr
}

func hostWithoutPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err != nil {
		return host
	}

	h, _, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}

	return h
}

func requestScheme(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}

	if r.TLS != nil {
		return "https"
	}

	return "https"
}

func newURL(r *http.Request, path string) *url.URL {
	next := *r.URL
	next.Path = path
	next.RawPath = ""

	return &next
}

func newRequestID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.Must(uuid.NewRandom())
	}

	return id
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// The pipeline-level error documents.
var (
	errMimeInvalid = &apperr.T{
		Status: http.StatusBadRequest,
		Title:  apperr.TitleBadRequest,
		Detail: "The mime type of the request is not acceptable (check content-type and accept headers)",
		Code:   "MIME_TYPE_INVALID",
	}

	errJSONInvalid = &apperr.T{
		Status: http.StatusBadRequest,
		Title:  apperr.TitleBadRequest,
		Detail: "The request could not be completed because it contains invalid JSON (check formatting/encoding)",
		Code:   "JSON_INVALID",
	}

	errTooManyRequests = &apperr.T{
		Status: http.StatusTooManyRequests,
		Title:  apperr.TitleTooManyRequests,
		Detail: "Throttle limit has been reached for your IP address. Please slow down. " +
			"See https://keygen.sh/docs/api/rate-limiting/ for more info.",
		Code: "TOO_MANY_REQUESTS",
	}
)

// errInvalidVersion is the shape a bad Keygen-Version header maps onto.
var errInvalidVersion = &apperr.T{
	Status: http.StatusBadRequest,
	Title:  apperr.TitleBadRequest,
	Detail: "unsupported API version requested",
	Code:   "INVALID_API_VERSION",
}

// acceptSignatureError maps the negotiation failures onto the
// reference's three 400 details.
func acceptSignatureError(err error) *apperr.T {
	var acceptErr *crypto.AcceptSignatureError

	detail := "invalid accept-signature header (malformed)"

	if errors.As(err, &acceptErr) {
		switch acceptErr.Kind {
		case crypto.AcceptSignatureUnsupported:
			detail = "invalid accept-signature header (unsupported algorithm)"
		case crypto.AcceptSignatureKeyIDNotFound:
			detail = "invalid accept-signature header (keyid not found)"
		case crypto.AcceptSignatureMalformed:
		}
	}

	return &apperr.T{
		Status: http.StatusBadRequest,
		Title:  apperr.TitleBadRequest,
		Detail: detail,
	}
}

// errorBytes renders an error wrapper's document to bytes (the
// signature covers the exact bytes on the wire).
func errorBytes(st *oas.ErrorStatusCode) []byte {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)

	_ = enc.Encode(st.Response) //nolint:errchkjson // nothing to do on a marshal of our own types

	return buf.Bytes()
}

// recorder buffers one response for the finalizers.
type recorder struct {
	header http.Header
	buf    bytes.Buffer
	code   int
}

func (rec *recorder) Header() http.Header { return rec.header }

func (rec *recorder) Write(b []byte) (int, error) {
	if rec.code == 0 {
		rec.code = http.StatusOK
	}

	return rec.buf.Write(b) //nolint:wrapcheck // the buffer cannot fail
}

func (rec *recorder) WriteHeader(code int) {
	if rec.code == 0 {
		rec.code = code
	}
}

func (rec *recorder) status() int {
	if rec.code == 0 {
		return http.StatusOK
	}

	return rec.code
}
