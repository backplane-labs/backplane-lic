package conformance

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Normalise rewrites volatile values in a decoded JSON transcript — UUIDs,
// token secrets, timestamps, long hex/base64 blobs (license keys, account
// public keys, signatures), and the run's random name suffix — into stable
// placeholders numbered by first appearance, so a golden recorded from the
// reference and a live transcript of our server become comparable.
// Object key order is irrelevant (maps walk in sorted key order); array
// order is preserved as-is.
func Normalise(value any, runSuffix string) any {
	n := normaliser{
		run:          runSuffix,
		placeholders: map[string]string{},
		counters:     map[string]int{},
	}

	n.compile()

	return n.value(value)
}

// minBlobLen keeps short words that happen to be hex-ish (e.g. "deadbeef")
// out of the blob placeholders.
const minBlobLen = 40

type normaliser struct {
	run string

	placeholders map[string]string
	counters     map[string]int

	uuid     *regexp.Regexp
	uuidFind *regexp.Regexp
	ts       *regexp.Regexp
	token    *regexp.Regexp
	hex      *regexp.Regexp
	b64      *regexp.Regexp
	base32   *regexp.Regexp
	b32Find  *regexp.Regexp
}

func (n *normaliser) compile() {
	const uuidPattern = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`

	n.uuid = regexp.MustCompile(`^` + uuidPattern + `$`)
	n.uuidFind = regexp.MustCompile(uuidPattern)
	n.ts = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z$`)
	n.token = regexp.MustCompile(`^[a-z]+-[0-9a-f]{20,}v[123]$`)
	n.hex = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	n.b64 = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
	n.base32 = regexp.MustCompile(`^[A-Z2-7]{26,}$`)
	n.b32Find = regexp.MustCompile(`[A-Z2-7]{26,}`)
}

func (n *normaliser) value(value any) any {
	switch node := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))

		for _, key := range slices.Sorted(maps.Keys(node)) {
			out[key] = n.value(node[key])
		}

		return out
	case []any:
		out := make([]any, len(node))

		for index, item := range node {
			out[index] = n.value(item)
		}

		return out
	case string:
		return n.string(node)
	default:
		return value
	}
}

func (n *normaliser) string(value string) string {
	switch {
	case n.token.MatchString(value):
		return n.placeholder("token", value)
	case n.ts.MatchString(value):
		return "<ts>"
	}

	value = n.replaceUUIDs(value)
	if n.run != "" {
		value = strings.ReplaceAll(value, n.run, "<run>")
	}

	switch {
	case len(value) >= minBlobLen && n.hex.MatchString(value):
		return n.placeholder("hex", value)
	case len(value) >= minBlobLen && n.b64.MatchString(value):
		return n.placeholder("b64", value)
	case n.base32.MatchString(value):
		return n.placeholder("b32", value)
	}

	// Base32 runs embedded in strings (a TOTP provisioning uri carries
	// its secret) are masked in place.
	if n.b32Find.MatchString(value) {
		return n.b32Find.ReplaceAllStringFunc(value, func(run string) string {
			return n.placeholder("b32", run)
		})
	}

	return value
}

// replaceUUIDs masks UUIDs standing alone (the whole value) as well as those
// embedded in link paths, where they are just as volatile.
func (n *normaliser) replaceUUIDs(value string) string {
	if n.uuid.MatchString(value) {
		return n.placeholder("uuid", value)
	}

	matches := n.uuidFind.FindAllStringIndex(value, -1)
	if matches == nil {
		return value
	}

	var out strings.Builder

	last := 0

	for _, match := range matches {
		out.WriteString(value[last:match[0]])
		out.WriteString(n.placeholder("uuid", value[match[0]:match[1]]))
		last = match[1]
	}

	out.WriteString(value[last:])

	return out.String()
}

// placeholder numbers equal values by first appearance, so the same UUID
// used in data.id and links.self collapses to one placeholder on both sides.
func (n *normaliser) placeholder(kind, value string) string {
	if existing, found := n.placeholders[value]; found {
		return existing
	}

	n.counters[kind]++

	placeholder := fmt.Sprintf("<%s:%d>", kind, n.counters[kind])
	n.placeholders[value] = placeholder

	return placeholder
}
