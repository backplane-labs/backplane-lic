package domain

import (
	"context"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The endpoint's signing algorithms (webhook_endpoints.signature_algorithm).
const (
	SignatureEd25519    = "ed25519"
	SignatureRSAPSS     = "rsa-pss-sha256"
	SignatureRSAPKCS1v5 = "rsa-sha256"
	SignatureECDSAP256  = "ecdsa-p256"
)

// SignatureAlgorithms is the closed set the endpoint accepts.
var SignatureAlgorithms = []string{ //nolint:gochecknoglobals // reference data
	SignatureEd25519, SignatureRSAPSS, SignatureRSAPKCS1v5, SignatureECDSAP256,
}

// WebhookEventStatus is the delivery lifecycle of a webhook event.
type WebhookEventStatus string

// The statuses (webhook_events.status).
const (
	EventDelivering WebhookEventStatus = "DELIVERING"
	EventDelivered  WebhookEventStatus = "DELIVERED"
	EventFailing    WebhookEventStatus = "FAILING"
	EventFailed     WebhookEventStatus = "FAILED"
)

// WebhookEndpoint is a registered HTTPS receiver of webhook events,
// optionally scoped to one product (it then receives only the events
// that product may read).
type WebhookEndpoint struct {
	ID            uuid.UUID
	AccountID     uuid.UUID
	ProductID     uuid.NullUUID
	URL           string
	Subscriptions []string
	// SignatureAlgorithm is never empty (ed25519 by default).
	SignatureAlgorithm string
	APIVersion         string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// Subscribed answers whether the endpoint receives the event ("*" or
// the exact name — disabled endpoints hold an empty list).
func (e WebhookEndpoint) Subscribed(event string) bool {
	for _, sub := range e.Subscriptions {
		if sub == "*" || sub == event {
			return true
		}
	}

	return false
}

// WebhookEvent is one queued delivery: the outbox row the domain writes
// in the same transaction as the change it describes.
type WebhookEvent struct {
	ID               uuid.UUID
	AccountID        uuid.UUID
	EndpointID       uuid.NullUUID
	Endpoint         string
	Event            string
	Payload          string
	APIVersion       string
	IdempotencyToken string
	Status           WebhookEventStatus
	Attempts         int
	NextAttemptAt    *time.Time
	LastResponseCode *int
	LastResponseBody *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// URLViolation is one rule the endpoint URL broke. The validator
// accumulates them in the reference's order (protocol, host, address).
type URLViolation struct {
	Code   string
	Detail string
}

// The URL violation codes, with the reference's exact details.
const (
	urlInvalidCode  = "URL_INVALID"
	urlProtocolCode = "URL_PROTOCOL_INVALID"
	urlHostCode     = "URL_HOST_INVALID"
	urlAddressCode  = "URL_ADDRESS_INVALID"
)

// The details of each URL violation.
const (
	urlInvalidDetail  = "must be a valid URL"
	urlProtocolDetail = "must be a valid URL using one of the following protocols: https"
	urlHostDetail     = "must be a URL with a valid host"
	urlAddressDetail  = "must resolve to a valid address"
)

// MaxWebhookURLLength is the URL column's 4 KB bound.
const MaxWebhookURLLength = 4 * 1024

// blacklistedHosts are the hosts (and their subdomains) the reference
// refuses: its own domains and localhost.
var blacklistedHosts = []string{ //nolint:gochecknoglobals // reference data
	"keygen.sh", "api.keygen.sh", "app.keygen.sh", "dist.keygen.sh",
	"dashboard.keygen.sh", "portal.keygen.sh", "status.keygen.sh",
	"stats.keygen.sh", "localhost",
}

// Resolver answers the addresses a host resolves to (DNS). An interface
// so tests can supply deterministic answers.
type Resolver interface {
	LookupHost(host string) ([]string, error)
}

// NetResolver resolves through the host's resolver (the default DNS
// path); tests inject deterministic answers instead.
type NetResolver struct{}

// LookupHost implements Resolver.
func (NetResolver) LookupHost(host string) ([]string, error) {
	addrs, err := net.DefaultResolver.LookupHost(context.Background(), host)

	return addrs, err //nolint:wrapcheck // the resolver's own error
}

// ValidateWebhookURL applies the reference's URL validator to a
// webhook endpoint URL: https only.
func ValidateWebhookURL(raw string, resolver Resolver, allowed []*net.IPNet) []URLViolation {
	return ValidateURL(raw, []string{"https"}, resolver, allowed)
}

// ValidateURL is the reference's shared URL validator (the endpoint and
// the product both use it, with different protocol sets): the protocol
// allowlist, a dotted public-suffix-style host that is not blacklisted,
// and DNS resolution where every address is public (or explicitly
// allowed — the self-hosted escape hatch for internal receivers).
// Violations accumulate in the validator's order.
func ValidateURL(
	raw string, protocols []string, resolver Resolver, allowed []*net.IPNet,
) []URLViolation {
	uri, err := parseWebhookURL(raw)
	if err != nil {
		return []URLViolation{{Code: urlInvalidCode, Detail: urlInvalidDetail}}
	}

	var violations []URLViolation

	if !slices.Contains(protocols, uri.Scheme) {
		detail := "must be a valid URL using one of the following protocols: " +
			strings.Join(protocols, ", ")

		violations = append(violations, URLViolation{urlProtocolCode, detail})
	}

	host := uri.Hostname()
	if !validWebhookHost(host) {
		violations = append(violations, URLViolation{urlHostCode, urlHostDetail})
	}

	if host == "" {
		// Every later rule needs a host; the reference reports the host
		// violation alone when the URI carries none.
		return violations
	}

	if !resolvesToAllowedAddress(host, resolver, allowed) {
		violations = append(violations, URLViolation{urlAddressCode, urlAddressDetail})
	}

	return violations
}

// parseWebhookURL parses the URL the way the reference's URI.parse does:
// an unparseable URI is the plain URL_INVALID violation.
func parseWebhookURL(raw string) (*url.URL, error) {
	uri, err := url.Parse(raw)
	if err != nil || uri == nil {
		return nil, err //nolint:wrapcheck // the sentinel is built above
	}

	// A URI without a host (e.g. "https:///path") behaves like an
	// invalid host in the reference.
	return uri, nil
}

// hostSuffix is the reference's host shape: a dot and 2+ letters (a
// public suffix) at the end of the host.
var hostSuffix = regexp.MustCompile(`\.[a-zA-Z]{2,}$`)

// validWebhookHost answers the host shape and blacklist rules.
func validWebhookHost(host string) bool {
	if host == "" || !hostSuffix.MatchString(host) {
		return false
	}

	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, blacklisted := range blacklistedHosts {
		if host == blacklisted || strings.HasSuffix(host, "."+blacklisted) {
			return false
		}
	}

	return true
}

// PublicAddress answers whether the IP is a routable public address
// under the reference's blacklist (loopback, private, link-local,
// mapped/compat and the reserved ranges are all refused) — or inside
// one of the allowed private ranges.
func PublicAddress(address net.IP, allowed []*net.IPNet) bool {
	for _, cidr := range allowed {
		if cidr.Contains(address) {
			return true
		}
	}

	if address.IsUnspecified() || address.IsLoopback() || address.IsPrivate() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() ||
		address.IsInterfaceLocalMulticast() || isIPv4Mapped(address) {
		return false
	}

	if v4 := address.To4(); v4 != nil {
		return !blacklistedIPv4.Contains(v4)
	}

	// A NAT64 address embeds an IPv4 address the reference blacklists
	// as itself.
	if nat64Prefixes.Contains(address) {
		return !blacklistedIPv4.Contains(net.IPv4(address[12], address[13], address[14], address[15]))
	}

	return !blacklistedIPv6.Contains(address)
}

// isIPv4Mapped answers whether the IPv6 address is an IPv4-mapped or
// -compatible form (the reference refuses both).
func isIPv4Mapped(address net.IP) bool {
	// A plain IPv4 address arrives in its 4-in-6 encoding; only the
	// true IPv6 mapped/compat forms are refused.
	if address.To4() != nil {
		return false
	}

	if len(address) != net.IPv6len {
		return false
	}

	for i := range 10 {
		if address[i] != 0 {
			return false
		}
	}

	return address[10] == 0xff || address[10] == 0x00
}

// ipSet is a closed set of CIDRs.
type ipSet struct {
	cidrs []*net.IPNet
}

// Contains answers whether any member covers the address.
func (s ipSet) Contains(ip net.IP) bool {
	for _, cidr := range s.cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}

	return false
}

func mustCIDRs(cidrs ...string) ipSet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, netIP, err := net.ParseCIDR(cidr)
		if err != nil {
			panic("webhook: bad reference CIDR " + cidr + ": " + err.Error())
		}

		out = append(out, netIP)
	}

	return ipSet{cidrs: out}
}

// blacklistedIPv4 is the reference's IPv4 blacklist (unroutable,
// reserved, documentation, CGNAT, multicast and benchmarking ranges).
var blacklistedIPv4 = mustCIDRs( //nolint:gochecknoglobals // reference data
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
	"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
)

// blacklistedIPv6 is the reference's IPv6 blacklist (unspecified, NAT64,
// documentation, 6to4, ULA, site-local, multicast, link-local, loopback).
var blacklistedIPv6 = mustCIDRs( //nolint:gochecknoglobals // reference data
	"::/128", "100::/64", "2001::/32", "2001:2::/48", "2001:db8::/32", "2002::/16",
	"fec0::/10", "ff00::/8", "fc00::/7", "fe80::/10", "::1/128",
)

// nat64Prefixes are the NAT64 well-known prefixes; an IPv6 address
// inside one embeds an IPv4 address the reference blacklists too.
var nat64Prefixes = mustCIDRs("64:ff9b::/96", "64:ff9b:1::/48") //nolint:gochecknoglobals // reference data

// resolvesToAllowedAddress answers whether DNS resolves and every
// resolved address is acceptable.
func resolvesToAllowedAddress(host string, resolver Resolver, allowed []*net.IPNet) bool {
	addrs, err := resolver.LookupHost(host)
	if err != nil || len(addrs) == 0 {
		return false
	}

	for _, addr := range addrs {
		ip := net.ParseIP(addr)
		if ip == nil {
			return false
		}

		if !PublicAddress(ip, allowed) {
			return false
		}
	}

	return true
}
