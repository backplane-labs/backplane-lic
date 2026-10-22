// Package conformance provides a differential test harness that replays HTTP
// scenarios against a Keygen CE-compatible server, records normalised golden
// transcripts from the live Keygen CE reference, and diffs live runs against
// them.
package conformance

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The reference deployment and our server are configured with the same
// single account and admin credentials.
const (
	AccountID   = "2c1d4f2e-9a3b-4c5d-8e7f-0a1b2c3d4e5f"
	AccountSlug = "example-com"
	AdminEmail  = "admin@example.com"
	AdminPass   = "adminadmin123"
)

// DefaultReferenceURL is where the Keygen CE reference server listens.
const DefaultReferenceURL = "http://localhost:3900"

// referenceHost routes the reference server (behind a TLS-terminating proxy).
const referenceHost = "api.keygen.test"

// requestTimeout bounds a single step's HTTP exchange.
const requestTimeout = 30 * time.Second

// adminTokenVar is the reserved capture name caching the run's admin token.
const adminTokenVar = "_admin_token"

// Target executes HTTP requests against one Keygen-compatible server. The
// two implementations cover a live server (reference or ours over HTTP) and,
// for later in-process boots, a plain http.Handler.
type Target interface {
	// NewRequest builds a request for a server-relative path, query included.
	NewRequest(ctx context.Context, method string, path string, body io.Reader) (*http.Request, error)
	// Do sends the request and returns the unprocessed response.
	Do(req *http.Request) (*http.Response, error)
}

// HTTPTarget targets a running server over HTTP, optionally overriding
// request headers (the reference needs Host and X-Forwarded-Proto).
type HTTPTarget struct {
	BaseURL string
	Header  http.Header
	Client  *http.Client

	mu         sync.Mutex
	adminToken string
}

// NewTarget returns a target for a bare server URL.
func NewTarget(baseURL string) *HTTPTarget {
	return &HTTPTarget{
		BaseURL: strings.TrimSuffix(baseURL, "/"),
		Header:  http.Header{},
		Client:  &http.Client{Timeout: requestTimeout},
	}
}

// NewReferenceTarget returns the Keygen CE reference target: the server
// routes by Host header and expects TLS to have terminated in front of it.
func NewReferenceTarget(baseURL string) *HTTPTarget {
	target := NewTarget(baseURL)
	target.Header.Set("Host", referenceHost)
	target.Header.Set("X-Forwarded-Proto", "https")

	return target
}

// NewRequest implements Target.
func (tgt *HTTPTarget) NewRequest(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
) (*http.Request, error) {
	url := tgt.BaseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("conformance: build %s %s: %w", method, path, err)
	}

	tgt.applyHeaders(req)

	return req, nil
}

// Do implements Target.
func (tgt *HTTPTarget) Do(req *http.Request) (*http.Response, error) {
	resp, err := tgt.Client.Do(req) //nolint:gosec // conformance harness calls local, operator-configured servers
	if err != nil {
		return nil, fmt.Errorf("conformance: %s %s: %w", req.Method, req.URL.Path, err)
	}

	return resp, nil
}

func (tgt *HTTPTarget) applyHeaders(req *http.Request) {
	for name, values := range tgt.Header {
		if name == "Host" {
			req.Host = values[0]

			continue
		}

		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
}

// HandlerTarget targets an in-process http.Handler, so the server under test
// can be exercised without a socket.
type HandlerTarget struct {
	Handler http.Handler

	mu         sync.Mutex
	adminToken string
}

// cachedAdminToken mints the admin token at most once per target: password
// attempts are throttled by the reference server.
func (h *HandlerTarget) cachedAdminToken(tb testing.TB) string {
	tb.Helper()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.adminToken == "" {
		h.adminToken = mintAdminToken(tb, h)
	}

	return h.adminToken
}

// NewRequest implements Target.
func (h *HandlerTarget) NewRequest(
	ctx context.Context,
	method string,
	path string,
	body io.Reader,
) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://lic.test"+path, body)
	if err != nil {
		return nil, fmt.Errorf("conformance: build %s %s: %w", method, path, err)
	}

	return req, nil
}

// Do implements Target.
func (h *HandlerTarget) Do(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	h.Handler.ServeHTTP(rec, req)

	return rec.Result(), nil
}

// AuthKind selects the authentication scheme of a step.
type AuthKind uint8

const (
	// AuthNone sends no credentials; custom headers go via Step.Headers.
	AuthNone AuthKind = iota
	// AuthAdmin sends a Bearer admin token minted (and cached) per run.
	AuthAdmin
	// AuthTokenVar sends Bearer with a token captured in a variable.
	AuthTokenVar
	// AuthLicenseVar sends License with a key captured in a variable.
	AuthLicenseVar
	// AuthBasic sends HTTP Basic credentials verbatim.
	AuthBasic
)

// Auth describes how a step authenticates.
type Auth struct {
	Kind      AuthKind
	Var       string // variable name for AuthTokenVar / AuthLicenseVar
	BasicUser string
	BasicPass string
}

// Step is one HTTP exchange within a scenario. Path, body and header values
// are {{var}}-interpolated from captures and the built-in "run" suffix.
type Step struct {
	Name    string
	Method  string
	Path    string
	Auth    Auth
	Body    string            // raw JSON body; empty means no body
	Headers map[string]string // extra or overriding request headers
	Capture map[string]string // variable name -> JSONPath-lite path into the body
}

// Scenario is a named, self-contained sequence of steps: it must create
// everything it needs and never depend on global resource counts.
type Scenario struct {
	Name  string
	Steps []Step
}

// StepTranscript records the observable outcome of one step.
type StepTranscript struct {
	Name    string            `json:"name"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Present []string          `json:"present,omitempty"`
	Body    any               `json:"body"`
}

// Transcript records a whole scenario run.
type Transcript struct {
	Scenario string           `json:"scenario"`
	Steps    []StepTranscript `json:"steps"`

	// Run is the per-run random suffix embedded in generated names. It is
	// not part of the golden output but is needed to normalise one.
	Run string `json:"-"`
}

// recordedHeaderNames lists the response headers whose values are compared.
func recordedHeaderNames() []string {
	return []string{
		"Content-Type",
		"Keygen-Version",
		"Keygen-Edition",
		"Keygen-Mode",
		"WWW-Authenticate",
	}
}

// presenceHeaderNames lists the response headers whose mere presence is
// compared (their values are timestamps or per-response signatures).
func presenceHeaderNames() []string {
	return []string{
		"Keygen-Signature",
		"Keygen-Digest",
		"Digest",
		"Keygen-Date",
		"Date",
		"X-Request-Id",
	}
}

// Run executes the scenario's steps sequentially against target and returns
// the transcript. When signer is non-nil, every response carrying a
// Keygen-Signature header is verified with it.
func Run(tb testing.TB, target Target, scenario Scenario, signer ed25519.PublicKey) Transcript {
	tb.Helper()

	vars := map[string]string{"run": runSuffix()}
	transcript := Transcript{
		Scenario: scenario.Name,
		Run:      vars["run"],
		Steps:    make([]StepTranscript, 0, len(scenario.Steps)),
	}

	for index := range scenario.Steps {
		step := scenario.Steps[index]

		transcript.Steps = append(transcript.Steps, runStep(tb, target, step, vars, signer))
	}

	return transcript
}

func runStep(
	tb testing.TB,
	target Target,
	step Step,
	vars map[string]string,
	signer ed25519.PublicKey,
) StepTranscript {
	tb.Helper()

	path := interpolate(tb, step.Path, vars)

	var body io.Reader
	if step.Body != "" {
		body = strings.NewReader(interpolate(tb, step.Body, vars))
	}

	req, err := target.NewRequest(tb.Context(), step.Method, path, body)
	if err != nil {
		tb.Fatalf("conformance: step %q: %v", step.Name, err)
	}

	applyRequestHeaders(tb, req, step, vars)
	applyAuth(tb, target, req, step.Auth, vars)

	resp, err := target.Do(req)
	if err != nil {
		tb.Fatalf("conformance: step %q: %v", step.Name, err)
	}

	raw, err := io.ReadAll(resp.Body)

	closeErr := resp.Body.Close()

	if err != nil || closeErr != nil {
		tb.Fatalf("conformance: step %q: read response: %v", step.Name, err)
	}

	if signer != nil {
		verifySignature(tb, step.Method, path, requestHost(req), resp.Header, raw, signer)
	}

	return newStepTranscript(tb, step, resp, raw, vars)
}

func newStepTranscript(
	tb testing.TB,
	step Step,
	resp *http.Response,
	raw []byte,
	vars map[string]string,
) StepTranscript {
	tb.Helper()

	transcript := StepTranscript{Name: step.Name, Status: resp.StatusCode, Headers: map[string]string{}}

	for _, name := range recordedHeaderNames() {
		if value := resp.Header.Get(name); value != "" {
			transcript.Headers[name] = value
		}
	}

	for _, name := range presenceHeaderNames() {
		if resp.Header.Get(name) != "" {
			transcript.Present = append(transcript.Present, name)
		}
	}

	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		return transcript
	}

	var body any
	if err := json.Unmarshal(raw, &body); err != nil {
		tb.Fatalf("conformance: step %q: decode JSON body %q: %v", step.Name, raw, err)
	}

	transcript.Body = body

	for name, jsonPath := range step.Capture {
		value, found := lookup(body, jsonPath)
		if !found {
			tb.Fatalf("conformance: step %q: capture %q: %q not found in %s", step.Name, name, jsonPath, raw)
		}

		vars[name] = fmt.Sprint(value)
	}

	return transcript
}

func applyRequestHeaders(tb testing.TB, req *http.Request, step Step, vars map[string]string) {
	tb.Helper()

	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/vnd.api+json")
	}

	if step.Body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/vnd.api+json")
	}

	for name, template := range step.Headers {
		req.Header.Set(name, interpolate(tb, template, vars))
	}
}

func applyAuth(tb testing.TB, target Target, req *http.Request, auth Auth, vars map[string]string) {
	tb.Helper()

	switch auth.Kind {
	case AuthNone:
	case AuthAdmin:
		if vars[adminTokenVar] == "" {
			vars[adminTokenVar] = AdminToken(tb, target)
		}

		req.Header.Set("Authorization", "Bearer "+vars[adminTokenVar])
	case AuthTokenVar:
		req.Header.Set("Authorization", "Bearer "+varValue(tb, auth.Var, vars))
	case AuthLicenseVar:
		req.Header.Set("Authorization", "License "+varValue(tb, auth.Var, vars))
	case AuthBasic:
		user := interpolate(tb, auth.BasicUser, vars)
		pass := interpolate(tb, auth.BasicPass, vars)
		req.Header.Set("Authorization", basicAuth(user, pass))
	}
}

// cachedAdminToken mints the admin token at most once per target: password
// attempts are throttled by the reference server.
func (tgt *HTTPTarget) cachedAdminToken(tb testing.TB) string {
	tb.Helper()

	tgt.mu.Lock()
	defer tgt.mu.Unlock()

	if tgt.adminToken == "" {
		tgt.adminToken = mintAdminToken(tb, tgt)
	}

	return tgt.adminToken
}

// AdminToken returns an admin token for the target via password
// authentication; targets that support it mint one at most once per run.
func AdminToken(tb testing.TB, target Target) string {
	tb.Helper()

	switch tgt := target.(type) {
	case *HTTPTarget:
		return tgt.cachedAdminToken(tb)
	case *HandlerTarget:
		return tgt.cachedAdminToken(tb)
	default:
		return mintAdminToken(tb, target)
	}
}

func mintAdminToken(tb testing.TB, target Target) string {
	tb.Helper()

	status, raw := call(tb, target, http.MethodPost, "/v1/accounts/"+AccountSlug+"/tokens",
		`{"data":{"type":"tokens"}}`, map[string]string{
			"Authorization": basicAuth(AdminEmail, AdminPass),
		})
	if status != http.StatusCreated {
		tb.Fatalf("conformance: admin token: unexpected status %d: %s", status, raw)
	}

	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		tb.Fatalf("conformance: admin token: decode body %q: %v", raw, err)
	}

	token, found := lookup(doc, "data.attributes.token")
	if !found {
		tb.Fatalf("conformance: admin token: no data.attributes.token in %s", raw)
	}

	return fmt.Sprint(token)
}

// call performs a harness-internal request and returns status and raw body.
func call(
	tb testing.TB,
	target Target,
	method string,
	path string,
	body string,
	headers map[string]string,
) (int, []byte) {
	tb.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req, err := target.NewRequest(tb.Context(), method, path, reader)
	if err != nil {
		tb.Fatalf("conformance: %s %s: %v", method, path, err)
	}

	req.Header.Set("Accept", "application/vnd.api+json")

	if body != "" {
		req.Header.Set("Content-Type", "application/vnd.api+json")
	}

	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := target.Do(req)
	if err != nil {
		tb.Fatalf("conformance: %s %s: %v", method, path, err)
	}

	raw, err := io.ReadAll(resp.Body)

	closeErr := resp.Body.Close()

	if err != nil || closeErr != nil {
		tb.Fatalf("conformance: %s %s: read response: %v", method, path, err)
	}

	return resp.StatusCode, raw
}

func basicAuth(user, pass string) string {
	credentials := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))

	return "Basic " + credentials
}

func varValue(tb testing.TB, name string, vars map[string]string) string {
	tb.Helper()

	value, found := vars[name]
	if !found {
		tb.Fatalf("conformance: auth: variable %q was not captured", name)
	}

	return value
}

// interpolate replaces {{name}} placeholders with captured values and fails
// the test when a placeholder is left unresolved.
func interpolate(tb testing.TB, template string, vars map[string]string) string {
	tb.Helper()

	for name, value := range vars {
		template = strings.ReplaceAll(template, "{{"+name+"}}", value)
	}

	if index := strings.Index(template, "{{"); index >= 0 {
		tb.Fatalf("conformance: unresolved variable in %q at %d", template, index)
	}

	return template
}

// lookup resolves a JSONPath-lite path ("data.attributes.id"; array indexes
// are numeric segments) against a decoded JSON value.
func lookup(value any, path string) (any, bool) {
	current := value

	for _, segment := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]any:
			next, found := node[segment]
			if !found {
				return nil, false
			}

			current = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}

			current = node[index]
		default:
			return nil, false
		}
	}

	return current, true
}

func runSuffix() string {
	buf := make([]byte, suffixBytes)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("conformance: read random suffix: %v", err))
	}

	return hex.EncodeToString(buf)
}

// suffixBytes of entropy keep generated names unique across runs.
const suffixBytes = 4

// requestHost is the host the server saw (for signature reconstruction),
// without the port.
func requestHost(req *http.Request) string {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	if withoutPort, _, err := net.SplitHostPort(host); err == nil {
		return withoutPort
	}

	return host
}
