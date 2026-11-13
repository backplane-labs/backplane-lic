package request

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/yaronf/httpsign"
	"golang.org/x/time/rate"

	"github.com/backplane-labs/backplane-lic/pkg/licensing"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/installation"
)

// AuthOptions configures an Authenticator.
type AuthOptions struct {
	// MaxAge is the maximum age of the created signature parameter.
	// Zero means 5 minutes; negative is rejected.
	MaxAge time.Duration

	// FutureSkew is how far a created timestamp may be in the future
	// (clock skew). Zero means 30 seconds; negative is rejected.
	FutureSkew time.Duration

	// CacheTTL is how long authenticated installations are cached.
	// The cache only skips the licensing API lookup; digest, created,
	// nonce and signature checks run on every request. Zero means
	// 1 minute; negative disables caching.
	CacheTTL time.Duration

	// NegativeTTL is how long a stable refusal (an unknown key, an
	// invalid license) is cached; key mismatches and upstream failures
	// are looked up again on the next request. Zero means 2 seconds;
	// negative disables negative caching.
	NegativeTTL time.Duration

	// LookupRate and LookupBurst bound the licensing API lookups (the
	// requests the cache cannot answer): past them a request is refused
	// as UPSTREAM_UNAVAILABLE instead of reaching the licensing API, so
	// forged keyids cannot flood it. Zero means 20 per second and a burst
	// of 50; negative is rejected.
	LookupRate  float64
	LookupBurst int

	// LookupTimeout bounds one licensing API lookup. Zero means
	// 10 seconds; negative is rejected.
	LookupTimeout time.Duration

	// MaxBodySize caps the request body read for digest validation.
	// Zero means 4 MiB; negative is rejected.
	MaxBodySize int64

	// SchemeFromRequest overrides the @scheme used to reconstruct
	// @target-uri during verification, e.g. by reading
	// X-Forwarded-Proto behind a TLS-terminating proxy. Nil means
	// derive it from the request's TLS state.
	SchemeFromRequest func(*http.Request) string

	// ReplayGuard rejects replayed nonces. When nil, an in-memory
	// guard with a bounded map is used whose TTL covers at least
	// 2×MaxAge and MaxAge+FutureSkew.
	ReplayGuard ReplayGuard
}

// replayTTLFactor is how many MaxAge windows a nonce is remembered.
const replayTTLFactor = 2

const (
	defaultMaxAge       = 5 * time.Minute
	defaultFutureSkew   = 30 * time.Second
	defaultCacheTTL     = 1 * time.Minute
	defaultNegTTL       = 2 * time.Second
	defaultLookupTime   = 10 * time.Second
	defaultLookupRate   = 20
	defaultLookupBurst  = 50
	defaultReplaySize   = 100_000
	defaultCacheSize    = 10_000
	defaultMaxBodyBytes = int64(4 << 20) // 4 MiB
)

// Authenticator verifies RFC 9421 signed requests against the Keygen
// licensing API. It is safe for concurrent use.
type Authenticator struct {
	client            *licensing.Client
	maxAge            time.Duration
	futureSkew        time.Duration
	cacheTTL          time.Duration
	negTTL            time.Duration
	lookupTimeout     time.Duration
	maxBodySize       int64
	schemeFromRequest func(*http.Request) string
	replayGuard       ReplayGuard
	lookups           *rate.Limiter

	cacheMu sync.Mutex
	cache   map[string]*cacheEntry

	flightsMu sync.Mutex
	flights   map[string]*flight
}

// cacheEntry is one cached lookup outcome: a positive Installation or
// a negative refusal error, with its expiry.
type cacheEntry struct {
	inst Installation
	err  error
	exp  time.Time
}

// flight is one in-flight licensing API lookup, shared by all
// concurrent requests for the same fingerprint (single-flight).
type flight struct {
	done chan struct{}
	inst Installation
	err  error
}

// NewAuthenticator builds an Authenticator with client and opts. The
// client must be authenticated with a product or admin token (not a
// license key). A negative MaxAge, FutureSkew, LookupTimeout,
// MaxBodySize, LookupRate or LookupBurst is ErrInvalidOptions: they must
// not silently weaken verification.
func NewAuthenticator(client *licensing.Client, opts AuthOptions) (*Authenticator, error) {
	if opts.MaxAge < 0 || opts.FutureSkew < 0 || opts.LookupTimeout < 0 || opts.MaxBodySize < 0 ||
		opts.LookupRate < 0 || opts.LookupBurst < 0 {
		return nil, ErrInvalidOptions
	}

	a := &Authenticator{
		client:            client,
		maxAge:            opts.MaxAge,
		futureSkew:        opts.FutureSkew,
		cacheTTL:          opts.CacheTTL,
		negTTL:            opts.NegativeTTL,
		lookupTimeout:     opts.LookupTimeout,
		maxBodySize:       opts.MaxBodySize,
		schemeFromRequest: opts.SchemeFromRequest,
		replayGuard:       opts.ReplayGuard,
		cache:             make(map[string]*cacheEntry),
		flights:           make(map[string]*flight),
	}

	if err := a.applyDefaults(opts); err != nil {
		return nil, err
	}

	return a, nil
}

// applyDefaults resolves the zero values.
func (a *Authenticator) applyDefaults(opts AuthOptions) error {
	if opts.MaxAge == 0 {
		a.maxAge = defaultMaxAge
	}

	if opts.FutureSkew == 0 {
		a.futureSkew = defaultFutureSkew
	}

	if opts.LookupTimeout == 0 {
		a.lookupTimeout = defaultLookupTime
	}

	if opts.MaxBodySize == 0 {
		a.maxBodySize = defaultMaxBodyBytes
	}

	lookupRate, lookupBurst := opts.LookupRate, opts.LookupBurst
	if lookupRate == 0 {
		lookupRate = defaultLookupRate
	}

	if lookupBurst == 0 {
		lookupBurst = defaultLookupBurst
	}

	a.lookups = rate.NewLimiter(rate.Limit(lookupRate), lookupBurst)

	if opts.CacheTTL == 0 {
		a.cacheTTL = defaultCacheTTL
	}

	if opts.NegativeTTL == 0 {
		a.negTTL = defaultNegTTL
	}

	if a.replayGuard == nil {
		// A nonce must stay remembered at least as long as a request
		// is acceptable: until created+MaxAge, where created may be up
		// to FutureSkew in the future.
		replayTTL := max(replayTTLFactor*a.maxAge, a.maxAge+a.futureSkew)

		guard, err := NewInMemoryReplayGuard(replayTTL, defaultReplaySize)
		if err != nil {
			return err
		}

		a.replayGuard = guard
	}

	return nil
}

// Middleware returns an http.Handler that authenticates every request
// before delegating to next. Authenticated installations are stored in
// the request context, accessible via FromContext. Refusals are
// answered with a JSON body {"error":{"code":…,"message":…}} and the
// mapped status (401/403/413/503, 500 for internal failures).
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inst, err := a.Authenticate(r)
		if err != nil {
			writeAuthError(w, err)

			return
		}

		next.ServeHTTP(w, r.WithContext(withInstallation(r.Context(), inst)))
	})
}

// Authenticate verifies the request's signature and returns the
// authenticated Installation. It is safe for concurrent use.
//
// On every request the following are always checked, cache or no
// cache: the signature parameters (keyid, created, nonce), the created
// window (past and future), the content-digest against the body, the
// nonce uniqueness (replay guard), and the Ed25519 signature against
// the machine's public key. The cache only avoids re-fetching the
// machine, its public key and the license state from the licensing
// API.
func (a *Authenticator) Authenticate(r *http.Request) (Installation, error) {
	details, err := a.parseSignature(r)
	if err != nil {
		return Installation{}, err
	}

	if err := a.checkCreated(*details.Created); err != nil {
		return Installation{}, err
	}

	bodyLen, err := readBody(r, a.maxBodySize)
	if err != nil {
		return Installation{}, err
	}

	if err := a.checkDigest(r, bodyLen); err != nil {
		return Installation{}, err
	}

	fingerprint := *details.KeyID

	inst, err := a.resolveInstallation(r.Context(), fingerprint)
	if err != nil {
		return Installation{}, err
	}

	if err := a.verifySignature(r, fingerprint, *details.Nonce, inst.PublicKey); err != nil {
		return Installation{}, err
	}

	return inst, nil
}

// parseSignature extracts the unverified signature parameters and
// checks the shape this package requires: a keyid that is an
// installation fingerprint, a created timestamp and a nonce.
func (a *Authenticator) parseSignature(r *http.Request) (*httpsign.MessageDetails, error) {
	details, err := httpsign.RequestDetails(sigLabel, r)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrSignatureMissing, err)
	}

	if details.KeyID == nil {
		return nil, fmt.Errorf("%w: keyid parameter is required", ErrSignatureMissing)
	}

	if !installation.ValidFingerprint(*details.KeyID) {
		return nil, fmt.Errorf("%w: keyid is not an installation fingerprint", ErrSignatureInvalid)
	}

	if details.Created == nil {
		return nil, ErrCreatedMissing
	}

	if details.Nonce == nil {
		return nil, ErrNonceMissing
	}

	return details, nil
}

// checkCreated validates the created timestamp is within the allowed
// window, both in the past and the future.
func (a *Authenticator) checkCreated(created time.Time) error {
	now := time.Now()

	if now.Sub(created) > a.maxAge {
		return ErrCreatedExpired
	}

	if created.Sub(now) > a.futureSkew {
		return ErrCreatedExpired
	}

	return nil
}

// checkDigest validates the Content-Digest header (RFC 9530, sha-256)
// against the request body. A request with a body must carry the
// header; a request without a body must not need it.
func (a *Authenticator) checkDigest(r *http.Request, bodyLen int) error {
	received := r.Header.Values(headerContentDigest)
	if len(received) == 0 {
		if bodyLen > 0 {
			return ErrDigestRequired
		}

		return nil
	}

	digestOpts := httpsign.NewDigestOptions().SetMaxBodySize(a.maxBodySize)

	err := httpsign.ValidateContentDigestHeader(received, &r.Body, []string{httpsign.DigestSha256}, digestOpts)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDigestMismatch, err)
	}

	return nil
}

// verifySignature checks the Ed25519 signature over the RFC 9421
// signature base, enforcing the created window, the keyid, the
// algorithm and the nonce uniqueness (replay guard) as library
// policies. Every policy is covered by the signature itself, so a
// tampered parameter fails verification.
func (a *Authenticator) verifySignature(r *http.Request, fingerprint, nonce string, pub ed25519.PublicKey) error {
	config := httpsign.NewVerifyConfig().
		SetVerifyCreated(true).
		SetNotNewerThan(a.futureSkew).
		SetNotOlderThan(a.maxAge).
		SetKeyID(fingerprint).
		SetAllowedAlgs([]string{algEd25519}).
		SetMaxBodySize(a.maxBodySize)

	if a.schemeFromRequest != nil {
		config.SetSchemeFromRequest(a.schemeFromRequest)
	}

	verifier, err := httpsign.NewEd25519Verifier(pub, config, signedFields())
	if err != nil {
		return fmt.Errorf("%w: %w", ErrSignatureInvalid, err)
	}

	if err := httpsign.VerifyRequest(sigLabel, *verifier, r); err != nil {
		return fmt.Errorf("%w: %w", ErrSignatureInvalid, err)
	}

	// The nonce is remembered only after the signature verified: the
	// library would run a nonce validator before the Ed25519 check, which
	// lets unauthenticated traffic fill the replay store.
	if !a.replayGuard.CheckAndRemember(fingerprint, nonce) {
		return ErrNonceReplayed
	}

	return nil
}

// resolveInstallation returns the Installation for the fingerprint,
// from the cache, a shared in-flight lookup, or a fresh licensing API
// lookup. Concurrent requests for the same fingerprint collapse into
// one lookup (single-flight).
func (a *Authenticator) resolveInstallation(ctx context.Context, fingerprint string) (Installation, error) {
	if inst, ok, err := a.cacheGet(fingerprint); ok {
		return inst, err
	}

	f := &flight{done: make(chan struct{})}

	a.flightsMu.Lock()
	existing, loading := a.flights[fingerprint]

	if !loading {
		a.flights[fingerprint] = f
	}

	a.flightsMu.Unlock()

	if loading {
		return a.waitFlight(ctx, existing)
	}

	var (
		inst Installation
		err  error
	)

	if a.lookups.Allow() {
		inst, err = a.lookup(ctx, fingerprint)
	} else {
		err = fmt.Errorf("%w: licensing lookups are rate limited", ErrUpstreamUnavailable)
	}

	f.inst, f.err = inst, err

	a.flightsMu.Lock()
	delete(a.flights, fingerprint)
	a.flightsMu.Unlock()

	close(f.done)

	return inst, err
}

// waitFlight blocks until the shared lookup finished or ctx is done.
func (a *Authenticator) waitFlight(ctx context.Context, f *flight) (Installation, error) {
	select {
	case <-f.done:
		return f.inst, f.err
	case <-ctx.Done():
		return Installation{}, fmt.Errorf("%w: %w", ErrUpstreamUnavailable, ctx.Err())
	}
}

// lookup fetches the machine and license state from the licensing API
// and updates the cache. It fails closed: an installation is only
// returned for a machine whose metadata public key hashes to the
// fingerprint and whose license validation says VALID.
func (a *Authenticator) lookup(ctx context.Context, fingerprint string) (Installation, error) {
	ctx, cancel := context.WithTimeout(ctx, a.lookupTimeout)
	defer cancel()

	machine, lic, err := a.client.ValidateMachine(ctx, fingerprint)
	if err == nil {
		err = licenseRefusal(lic)
	}

	if err != nil {
		mapped := mapAuthError(err, fingerprint, lic)

		// Only an unknown key and an invalid license are stable negative
		// results. Transport failures, cancellations and unexpected API
		// answers say nothing about the key and must be retried, never
		// cached.
		if errors.Is(mapped, ErrLicenseInvalid) || errors.Is(mapped, ErrUnknownKey) {
			a.cachePut(fingerprint, Installation{}, mapped)
		}

		return Installation{}, mapped
	}

	pub, err := installation.PublicKeyOf(machine)
	if err != nil {
		return Installation{}, fmt.Errorf("%w: %w", ErrKeyMismatch, err)
	}

	// Never trust the lookup alone: the key must hash to the
	// fingerprint the signature claims.
	if installation.Fingerprint(pub) != fingerprint {
		return Installation{}, fmt.Errorf("%w: key hashes to a different fingerprint", ErrKeyMismatch)
	}

	inst := Installation{
		Fingerprint: fingerprint,
		MachineID:   machine.ID,
		LicenseID:   machine.LicenseID,
		PublicKey:   pub,
		LicenseCode: string(lic.Validation.Code),
	}

	a.cachePut(fingerprint, inst, nil)

	return inst, nil
}

// licenseRefusal fails closed on a license whose validation result is
// missing or not VALID.
func licenseRefusal(lic *licensing.License) error {
	switch {
	case lic == nil || lic.Validation == nil:
		return &LicenseError{Detail: "validation result is missing"}
	case lic.Validation.Code != licensing.CodeValid || !lic.Validation.Valid:
		return &LicenseError{Code: lic.Validation.Code, Detail: lic.Validation.Detail}
	default:
		return nil
	}
}

// mapAuthError translates licensing client errors into this package's
// typed refusals.
func mapAuthError(err error, fingerprint string, lic *licensing.License) error {
	switch {
	case errors.Is(err, licensing.ErrNotFound):
		return fmt.Errorf("%w: fingerprint %s", ErrUnknownKey, fingerprint)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	}

	var rateLimit *licensing.RateLimitError
	if errors.As(err, &rateLimit) {
		return fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	}

	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	}

	var apiErr *licensing.APIError

	switch {
	case errors.As(err, &apiErr) && apiErr.Status >= http.StatusInternalServerError:
		return fmt.Errorf("%w: %w", ErrUpstreamUnavailable, err)
	case errors.As(err, &apiErr):
		// A 4xx on the lookup itself means the authenticator's own
		// credentials or policy are wrong: an internal failure, not a
		// client refusal.
		return fmt.Errorf("licensing API refused the machine lookup: %w", err)
	}

	if lic != nil && lic.Validation != nil && lic.Validation.Code != licensing.CodeValid {
		return &LicenseError{Code: lic.Validation.Code, Detail: lic.Validation.Detail, cause: err}
	}

	return fmt.Errorf("machine lookup failed: %w", err)
}

// cacheGet returns a cached lookup outcome and whether the cache had
// a live entry.
func (a *Authenticator) cacheGet(fingerprint string) (Installation, bool, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	entry, ok := a.cache[fingerprint]
	if !ok {
		return Installation{}, false, nil
	}

	if !time.Now().Before(entry.exp) {
		delete(a.cache, fingerprint)

		return Installation{}, false, nil
	}

	return entry.inst, true, entry.err
}

// cachePut stores a positive (lookupErr == nil, cached for CacheTTL)
// or negative (cached for NegativeTTL) outcome. A non-positive TTL
// disables the respective cache; a full cache skips storing.
func (a *Authenticator) cachePut(fingerprint string, inst Installation, lookupErr error) {
	ttl := a.cacheTTL
	if lookupErr != nil {
		ttl = a.negTTL
	}

	if ttl <= 0 {
		return
	}

	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()

	if len(a.cache) >= defaultCacheSize {
		a.evictExpired(time.Now())
	}

	if len(a.cache) >= defaultCacheSize {
		return
	}

	a.cache[fingerprint] = &cacheEntry{inst: inst, err: lookupErr, exp: time.Now().Add(ttl)}
}

// evictExpired drops all expired cache entries. The caller holds
// a.cacheMu.
func (a *Authenticator) evictExpired(now time.Time) {
	for key, entry := range a.cache {
		if !now.Before(entry.exp) {
			delete(a.cache, key)
		}
	}
}

// errorBody is the JSON error document written by Middleware.
type errorBody struct {
	Error errorDetail `json:"error"`
}

// errorDetail is the stable machine code and the message of a refusal.
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// authErrorStatus maps a typed refusal to its HTTP status and stable
// error code.
func authErrorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, ErrSignatureMissing),
		errors.Is(err, ErrSignatureInvalid),
		errors.Is(err, ErrCreatedMissing),
		errors.Is(err, ErrNonceMissing):
		return http.StatusUnauthorized, "SIGNATURE_INVALID"
	case errors.Is(err, ErrDigestMismatch),
		errors.Is(err, ErrDigestRequired):
		return http.StatusUnauthorized, "DIGEST_INVALID"
	case errors.Is(err, ErrBodyTooLarge):
		return http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE"
	case errors.Is(err, ErrCreatedExpired):
		return http.StatusUnauthorized, "SIGNATURE_EXPIRED"
	case errors.Is(err, ErrNonceReplayed):
		return http.StatusUnauthorized, "NONCE_REPLAYED"
	case errors.Is(err, ErrUnknownKey):
		return http.StatusForbidden, "UNKNOWN_KEY"
	case errors.Is(err, ErrKeyMismatch):
		return http.StatusForbidden, "KEY_MISMATCH"
	case errors.Is(err, ErrLicenseInvalid):
		return http.StatusForbidden, "LICENSE_INVALID"
	case errors.Is(err, ErrUpstreamUnavailable):
		return http.StatusServiceUnavailable, "UPSTREAM_UNAVAILABLE"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR"
	}
}

// writeAuthError maps authentication errors to HTTP status codes and
// writes a JSON error response.
func writeAuthError(w http.ResponseWriter, err error) {
	status, code := authErrorStatus(err)

	// A failure's message stays with the server: it may name upstream
	// details.
	message := err.Error()
	if status >= http.StatusInternalServerError {
		message = http.StatusText(status)
	}

	body, marshalErr := json.Marshal(errorBody{Error: errorDetail{Code: code, Message: message}})
	if marshalErr != nil {
		http.Error(w, http.StatusText(status), status)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_, _ = w.Write(body)
}
