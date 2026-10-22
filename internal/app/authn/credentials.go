// Package authn is the authentication scenario: every Authorization
// scheme and the ?token=/?auth= query form, resolved to a principal.
package authn

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// CredentialKind discriminates the raw credential carried by the request.
type CredentialKind int

// The raw kinds: how the credential arrived, before the mode resolves
// what a Basic username means.
const (
	CredentialNone CredentialKind = iota
	CredentialToken
	CredentialLicense
	CredentialBasic
)

// Credentials is the raw authentication material of one request.
type Credentials struct {
	Kind CredentialKind
	// Scheme is the Authorization scheme as sent ("Bearer", "Token",
	// "License", "Basic"); empty when the credential came from the query.
	Scheme string
	// Value is the bearer/token value (Bearer, Token, License) or the
	// Basic payload's username (legacy Basic token auth).
	Value string
	// Secret is the Basic payload's password (the license key or token
	// value for the "license:"/"token:" prefixed forms, or the user's
	// password for the email form).
	Secret string
	// PasswordPresent distinguishes a Basic username from username: (empty password).
	PasswordPresent bool
}

// The recognized Authorization schemes (case-insensitive, like the
// reference's downcase).
const (
	schemeBearer  = "bearer"
	schemeToken   = "token"
	schemeBasic   = "basic"
	schemeLicense = "license"
)

// emailRe approximates the reference's EMAIL_RE (also the users table's
// email CHECK).
var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Parse extracts the raw credentials: the Authorization header first,
// then the ?token=/?auth= query value. Basic values are base64-decoded
// here; what the username means is resolved per-mode in Resolve.
func Parse(authorization, queryValue string) Credentials {
	scheme, value := splitAuthorization(authorization)

	switch strings.ToLower(scheme) {
	case schemeBearer, schemeToken:
		return Credentials{Kind: CredentialToken, Scheme: canonicalScheme(scheme), Value: value}
	case schemeLicense:
		return Credentials{Kind: CredentialLicense, Scheme: canonicalScheme(scheme), Value: value}
	case schemeBasic:
		user, pass, present, ok := decodeBasic(value)
		if !ok {
			return Credentials{Kind: CredentialNone}
		}

		return Credentials{
			Kind: CredentialBasic, Scheme: canonicalScheme(scheme), Value: user, Secret: pass, PasswordPresent: present,
		}
	case "":
		if queryValue == "" {
			return Credentials{Kind: CredentialNone}
		}

		user, pass, hasPass := strings.Cut(queryValue, ":")
		if hasPass {
			if user == "license" {
				return Credentials{Kind: CredentialLicense, Value: pass}
			}

			if user == "token" {
				return Credentials{Kind: CredentialToken, Value: pass}
			}
		}

		// Legacy: the bare query value is a token.
		return Credentials{Kind: CredentialToken, Value: queryValue}
	default:
		// Unknown scheme: no usable credential.
		return Credentials{Kind: CredentialNone}
	}
}

// Mode selects the endpoint's authentication entry point.
type Mode int

// The entry points: authenticate_with_token! (everything) and
// authenticate_with_password_or_token! (POST /tokens).
const (
	ModeToken Mode = iota
	ModePasswordOrToken
)

// Resolved is the mode-resolved credential: exactly one flow to run.
type Resolved struct {
	// Flow is the authentication flow to execute.
	Flow Flow
	// TokenValue is the token secret (FlowToken).
	TokenValue string
	// LicenseKey is the license key (FlowLicense).
	LicenseKey string
	// Email and Password carry the Basic email:password pair
	// (FlowPassword).
	Email    string
	Password string
}

// Flow is an authentication flow.
type Flow int

// The flows.
const (
	FlowNone Flow = iota
	FlowToken
	FlowLicense
	FlowPassword
)

// Resolve interprets the credentials for the endpoint's mode: the
// license:/token: Basic prefixes always map to their flows, an
// email:password Basic pair is a password flow only where the endpoint
// honors passwords, and anything else is (legacy) token auth with the
// username as the value.
func (c Credentials) Resolve(mode Mode) Resolved {
	switch c.Kind {
	case CredentialToken:
		return Resolved{Flow: FlowToken, TokenValue: c.Value}
	case CredentialLicense:
		return Resolved{Flow: FlowLicense, LicenseKey: c.Value}
	case CredentialBasic:
		switch c.Value {
		case "license":
			return Resolved{Flow: FlowLicense, LicenseKey: c.Secret}
		case "token":
			return Resolved{Flow: FlowToken, TokenValue: c.Secret}
		}

		if mode == ModePasswordOrToken && strings.Contains(c.Value, "@") && c.PasswordPresent {
			return Resolved{Flow: FlowPassword, Email: c.Value, Password: c.Secret}
		}

		// Legacy: Basic base64(<token>:).
		return Resolved{Flow: FlowToken, TokenValue: c.Value}
	default:
		return Resolved{Flow: FlowNone}
	}
}

// ChallengeScheme answers the WWW-Authenticate scheme for a failure: the
// Basic realm for the password flow, Bearer otherwise.
func (r Resolved) ChallengeScheme() string {
	if r.Flow == FlowPassword {
		return "Basic"
	}

	return "Bearer"
}

func splitAuthorization(authorization string) (string, string) {
	scheme, value, ok := strings.Cut(strings.TrimSpace(authorization), " ")
	if !ok {
		return "", ""
	}

	return scheme, strings.TrimSpace(value)
}

func canonicalScheme(scheme string) string {
	if scheme == "" {
		return ""
	}

	return strings.ToUpper(scheme[:1]) + strings.ToLower(scheme[1:])
}

func decodeBasic(value string) (string, string, bool, bool) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", "", false, false
	}

	user, pass, present := strings.Cut(string(raw), ":")

	return user, pass, present, true
}
