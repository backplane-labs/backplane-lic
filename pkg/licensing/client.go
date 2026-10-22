package licensing

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"

	"github.com/backplane-labs/backplane-lic/pkg/licensing/internal/jsonapi"
)

// sdkVersion is the client's version, reported in the User-Agent.
const sdkVersion = "0.1.0"

// Client is a Keygen licensing API client. Construct it with New; it is
// safe for concurrent use. Every response is verified against the
// account's public key when Config.PublicKey is set.
type Client struct {
	cfg      Config
	http     *http.Client
	log      *slog.Logger
	verifier Verifier
	skew     SkewGuard
}

// New validates cfg, resolves defaults, and returns a ready Client. It
// performs no I/O.
func New(cfg Config) (*Client, error) {
	normalized, err := cfg.normalize()
	if err != nil {
		return nil, err
	}

	client := &Client{
		cfg:  normalized,
		http: normalized.HTTPClient,
		log:  normalized.Logger,
		skew: SkewGuardFor(normalized.ClockSkew),
	}

	if client.http == nil {
		client.http = &http.Client{Timeout: httpClientTimeout}
	}

	if client.log == nil {
		client.log = slog.New(slog.DiscardHandler)
	}

	if normalized.PublicKey != "" {
		verifier, err := NewVerifier(normalized.PublicKey)
		if err != nil {
			return nil, err
		}

		client.verifier = verifier
	}

	return client, nil
}

// httpClientTimeout bounds a single API request.
const httpClientTimeout = 30 * time.Second

// userAgent is the default User-Agent prefix.
func (c Client) userAgent() string {
	base := "backplane-licensing/" + sdkVersion +
		" keygen/" + c.cfg.APIVersion +
		" go/" + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH

	if c.cfg.UserAgent != "" {
		return base + " " + c.cfg.UserAgent
	}

	return base
}

// urlFor builds the absolute URL of a path relative to the API prefix.
func (c Client) urlFor(path string, query url.Values) string {
	full := c.cfg.APIURL + c.cfg.APIPrefix
	if c.cfg.cloudScoped() {
		full += "/accounts/" + c.cfg.Account
	}

	full += "/" + path

	if enc := query.Encode(); enc != "" {
		full += "?" + enc
	}

	return full
}

// response captures what a call needs from an HTTP exchange.
type response struct {
	body   []byte
	status int
	header http.Header
	id     string
	req    *http.Request
}

// do performs one API call: builds the request, sends it, checks rate
// limits and the response signature, and decodes the JSON:API document.
// When body is non-nil it is encoded as the document's data (plus meta
// when set). decode, when non-nil, receives the parsed document.
func (c *Client) do(
	ctx context.Context,
	method, path string,
	query url.Values,
	body jsonapi.Body,
	decode func(jsonapi.Document) error,
) error {
	req, size, err := c.buildRequest(ctx, method, path, query, body)
	if err != nil {
		return err
	}

	c.log.Debug("request", "method", method, "url", req.URL.String(), "size", size)

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("licensing: request: %w", err)
	}

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		_ = res.Body.Close()

		return fmt.Errorf("licensing: read response: %w", err)
	}

	if err := res.Body.Close(); err != nil {
		return fmt.Errorf("licensing: close response: %w", err)
	}

	out := response{
		body:   bodyBytes,
		status: res.StatusCode,
		header: res.Header,
		id:     res.Header.Get("X-Request-Id"),
		req:    req,
	}

	c.log.Debug("response", "id", out.id, "status", out.status, "size", len(out.body))

	if err := c.checkResponse(out); err != nil {
		return err
	}

	if decode == nil || out.status == http.StatusNoContent || len(out.body) == 0 {
		return nil
	}

	doc, err := jsonapi.Parse(out.body)
	if err != nil {
		return fmt.Errorf("licensing: id=%s: %w", out.id, err)
	}

	return decode(doc)
}

// buildRequest encodes the body (when present) and assembles the request
// with the client's default and configured headers. It returns the ready
// request and the encoded body's size.
func (c *Client) buildRequest(
	ctx context.Context,
	method, path string,
	query url.Values,
	body jsonapi.Body,
) (*http.Request, int, error) {
	var payload []byte

	if body.Type != "" {
		encoded, err := body.Encode()
		if err != nil {
			return nil, 0, fmt.Errorf("licensing: encode request: %w", err)
		}

		payload = encoded
	}

	req, err := http.NewRequestWithContext(ctx, method, c.urlFor(path, query), http.NoBody)
	if err != nil {
		return nil, 0, fmt.Errorf("licensing: build request: %w", err)
	}

	if len(payload) > 0 {
		req.Body = io.NopCloser(strings.NewReader(string(payload)))
		req.ContentLength = int64(len(payload))
		req.Header.Set("Content-Type", jsonapi.ContentType)
	}

	req.Header.Set("Accept", jsonapi.ContentType)
	req.Header.Set("Keygen-Version", c.cfg.APIVersion)
	req.Header.Set("User-Agent", c.userAgent())

	switch {
	case c.cfg.LicenseKey != "":
		req.Header.Set("Authorization", "License "+c.cfg.LicenseKey)
	case c.cfg.Token != "":
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	for key, value := range c.cfg.Headers {
		if strings.EqualFold(key, "Host") {
			req.Host = value

			continue
		}

		req.Header.Set(key, value)
	}

	return req, len(payload), nil
}

// checkResponse maps non-2xx statuses and verifies the signature of
// everything else.
func (c *Client) checkResponse(res response) error {
	switch {
	case res.status == http.StatusTooManyRequests:
		return parseRateLimit(res.header)
	case res.status >= http.StatusInternalServerError:
		return &APIError{
			Status:    res.status,
			Title:     "Internal server error",
			Detail:    tldr(res.body),
			Code:      "INTERNAL_ERROR",
			RequestID: res.id,
		}
	case res.status >= http.StatusBadRequest:
		doc, err := jsonapi.Parse(res.body)
		if err != nil || len(doc.Errors) == 0 {
			return &APIError{Status: res.status, Code: "UNKNOWN", Detail: tldr(res.body), RequestID: res.id}
		}

		first := doc.Errors[0]

		return &APIError{
			Status:    res.status,
			Title:     first.Title,
			Detail:    first.Detail,
			Code:      first.Code,
			Source:    first.Source.Pointer + first.Source.Parameter + first.Source.Header,
			RequestID: res.id,
		}
	}

	if c.verifier.configured() {
		if err := c.verifier.VerifyResponse(res.req, res.header, res.body, c.skew); err != nil {
			return fmt.Errorf("licensing: id=%s: verify response: %w", res.id, err)
		}
	}

	return nil
}

// tldrMax bounds the body excerpt in error messages.
const tldrMax = 500

// tldr renders a body excerpt for error messages.
func tldr(body []byte) string {
	excerpt := string(body)
	if len(excerpt) > tldrMax {
		excerpt = excerpt[:tldrMax] + "..."
	}

	return strings.ReplaceAll(excerpt, "\n", "\\n")
}
