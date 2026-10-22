// The licenses index filters (the reference's has_scope declarations).
package httpapi

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
)

// The scope-window calendar factors (the reference's ActiveSupport
// durations).
const (
	hoursPerDay  = 24
	daysPerWeek  = 7
	daysPerMonth = 30
	daysPerYear  = 365
)

// licenseFilter builds the index filter from the query.
func licenseFilter(query url.Values) licenses.Filter {
	f := licenses.Filter{
		ProductID: queryUUID(query.Get("product")),
		PolicyID:  queryUUID(query.Get("policy")),
		MachineID: queryUUID(query.Get("machine")),
		GroupID:   queryUUID(query.Get("group")),
		Status:    licenseStatusFilter(query.Get("status")),
		Suspended: queryBool(query, "suspended"),
		Expiring:  queryBool(query, "expiring"),
		Assigned:  queryBool(query, "assigned"),
		Activated: queryBool(query, "activated"),
		Metadata:  metadataFilter(query),
	}

	f.Unassigned = queryBool(query, "unassigned")

	// owner and user accept a UUID or an email term (exact then ILIKE).
	f.OwnerID = queryUUID(query.Get("owner"))
	f.UserID = queryUUID(query.Get("user"))

	if term := query.Get("owner"); term != "" && !f.OwnerID.Valid {
		f.OwnerEmail, f.OwnerEmailLike = term, term
	}

	if term := query.Get("user"); term != "" && !f.UserID.Valid {
		f.UserEmail, f.UserEmailLike = term, term
	}

	// expired: a boolean or the expired[within|in|before|after|on] hash.
	if hasHashKey(query, "expired") { //nolint:nestif // the expired window forms
		f.ExpiryGte1, f.ExpiryLte = expiredWindow(query, "expired")
		f.Expired = boolPtr(true)
	} else if raw := query.Get("expired"); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			if value {
				f.Expired = boolPtr(true)
			} else {
				f.Unexpired = boolPtr(true)
			}
		} else {
			// An unparsable value matches nothing.
			f.Expired, f.Unexpired = boolPtr(true), boolPtr(true)
		}
	}

	// expires: the future-window analog.
	if hasHashKey(query, "expires") {
		gte, lte := expiresWindow(query)
		f.ExpiryGte2, f.ExpiryLte = gte, lte
		f.Expired = boolPtr(false)
		f.Unexpired = boolPtr(true)
	}

	// activity: inside/outside durations, before/after timestamps.
	switch {
	case hasHashKey(query, "activity"):
		f.ActivityActiveSince, f.ActivityInactiveSince = activityWindow(query)
	case query.Get("activity") != "":
		// An unparsable activity matches nothing.
		f.ActivityActiveSince, f.ActivityInactiveSince = pastPtr(), pastPtr()
	}

	// activations: the machines_count comparison.
	f.ActivationsEq = queryInt32(query, "activations[eq]")
	f.ActivationsGt = queryInt32(query, "activations[gt]")
	f.ActivationsGte = queryInt32(query, "activations[gte]")
	f.ActivationsLt = queryInt32(query, "activations[lt]")
	f.ActivationsLte = queryInt32(query, "activations[lte]")

	return f
}

// machineFilter builds the machine relationship filter.
func machineFilter(req *request) licenses.MachineFilter {
	return licenses.MachineFilter{
		Fingerprint: req.query.Get("fingerprint"),
		ProductID:   queryUUID(req.query.Get("product")),
		UserID:      queryUUID(req.query.Get("user")),
	}
}

func licenseStatusFilter(value string) string {
	switch strings.ToUpper(value) {
	case "ACTIVE", "INACTIVE", "EXPIRING", "EXPIRED", "SUSPENDED", "BANNED":
		return strings.ToUpper(value)
	case "":
		return ""
	default:
		return licenses.StatusNoMatch
	}
}

func hasHashKey(query url.Values, name string) bool {
	for key := range query {
		if strings.HasPrefix(key, name+"[") && strings.HasSuffix(key, "]") {
			return true
		}
	}

	return false
}

// expiredWindow resolves expired[within|in|before|after|on] to the
// expiry bounds.
func expiredWindow(query url.Values, name string) (*time.Time, *time.Time) {
	switch {
	case hashValue(query, name, "within") != "" || hashValue(query, name, "in") != "":
		d, ok := parseScopeDuration(firstNonEmpty(hashValue(query, name, "within"), hashValue(query, name, "in")))
		if !ok {
			return pastPtr(), pastPtr()
		}

		since := time.Now().Add(-d)

		return &since, nil
	case hashValue(query, name, "before") != "":
		t, ok := parseScopeTime(hashValue(query, name, "before"))
		if !ok {
			return pastPtr(), pastPtr()
		}

		return nil, &t
	case hashValue(query, name, "after") != "":
		t, ok := parseScopeTime(hashValue(query, name, "after"))
		if !ok {
			return pastPtr(), pastPtr()
		}

		return &t, nil
	case hashValue(query, name, "on") != "":
		start, end, ok := parseScopeDay(hashValue(query, name, "on"))
		if !ok {
			return pastPtr(), pastPtr()
		}

		return &start, &end
	default:
		return pastPtr(), pastPtr()
	}
}

// expiresWindow resolves expires[within|in|before|after|on].
func expiresWindow(query url.Values) (*time.Time, *time.Time) {
	switch {
	case hashValue(query, "expires", "within") != "" || hashValue(query, "expires", "in") != "":
		d, ok := parseScopeDuration(firstNonEmpty(hashValue(query, "expires", "within"), hashValue(query, "expires", "in")))
		if !ok {
			return pastPtr(), pastPtr()
		}

		until := time.Now().Add(d)

		return nil, &until
	case hashValue(query, "expires", "before") != "":
		t, ok := parseScopeTime(hashValue(query, "expires", "before"))
		if !ok {
			return pastPtr(), pastPtr()
		}

		return nil, &t
	case hashValue(query, "expires", "after") != "":
		t, ok := parseScopeTime(hashValue(query, "expires", "after"))
		if !ok {
			return pastPtr(), pastPtr()
		}

		return &t, nil
	case hashValue(query, "expires", "on") != "":
		start, end, ok := parseScopeDay(hashValue(query, "expires", "on"))
		if !ok {
			return pastPtr(), pastPtr()
		}

		return &start, &end
	default:
		return pastPtr(), pastPtr()
	}
}

// activityWindow resolves activity[inside|outside|before|after].
func activityWindow(query url.Values) (*time.Time, *time.Time) {
	switch {
	case hashValue(query, "activity", "inside") != "":
		if d, ok := parseScopeDuration(hashValue(query, "activity", "inside")); ok {
			return timePtrOf(time.Now().Add(-d)), nil
		}
	case hashValue(query, "activity", "outside") != "":
		if d, ok := parseScopeDuration(hashValue(query, "activity", "outside")); ok {
			return nil, timePtrOf(time.Now().Add(-d))
		}
	case hashValue(query, "activity", "before") != "":
		if t, ok := parseScopeTime(hashValue(query, "activity", "before")); ok {
			return nil, &t
		}
	case hashValue(query, "activity", "after") != "":
		if t, ok := parseScopeTime(hashValue(query, "activity", "after")); ok {
			return &t, nil
		}
	}

	return pastPtr(), pastPtr()
}

func hashValue(query url.Values, name, key string) string {
	return query.Get(name + "[" + key + "]")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func pastPtr() *time.Time {
	t := time.Unix(0, 0)

	return &t
}

func timePtrOf(t time.Time) *time.Time { return &t }

func boolPtr(v bool) *bool { return &v }

func queryInt32(query url.Values, key string) *int32 {
	raw := query.Get(key)
	if raw == "" {
		return nil
	}

	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		invalid := int32(-1)
		// An unparsable comparison matches nothing for gt/gte and
		// everything for lt/lte; the simplest faithful no-match is a
		// negative bound on the equality key.
		if strings.HasSuffix(key, "eq") {
			return &invalid
		}

		return nil
	}

	out := int32(value)

	return &out
}

// parseScopeDuration parses an ISO8601 duration (P1W, PT3600S) or a
// plain second count.
func parseScopeDuration(raw string) (time.Duration, bool) { //nolint:cyclop,funlen // the duration forms
	if raw == "" {
		return 0, false
	}

	if seconds, err := strconv.Atoi(raw); err == nil {
		return time.Duration(seconds) * time.Second, true
	}

	if !strings.HasPrefix(raw, "P") {
		return 0, false
	}

	var (
		total time.Duration
		num   strings.Builder
		unit  rune
	)

	timePart := false

	apply := func() bool {
		value, err := strconv.ParseInt(num.String(), 10, 64)
		if err != nil {
			return false
		}

		switch unit {
		case 'Y':
			total += time.Duration(value) * daysPerYear * hoursPerDay
		case 'M':
			if timePart {
				total += time.Duration(value) * time.Minute
			} else {
				total += time.Duration(value) * daysPerMonth * hoursPerDay
			}
		case 'W':
			total += time.Duration(value) * daysPerWeek * hoursPerDay
		case 'D':
			total += time.Duration(value) * hoursPerDay
		case 'H':
			total += time.Duration(value) * time.Hour
		case 'S':
			total += time.Duration(value) * time.Second
		default:
			return false
		}

		num.Reset()

		return true
	}

	for _, c := range raw {
		switch {
		case c == 'P':
		case c == 'T':
			timePart = true
		case c >= '0' && c <= '9' || c == '.':
			num.WriteRune(c)
		default:
			unit = c

			if !apply() {
				return 0, false
			}
		}
	}

	return total, true
}

// parseScopeTime parses a unix second count or an ISO timestamp.
func parseScopeTime(raw string) (time.Time, bool) {
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Unix(seconds, 0), true
	}

	if t, parsed := parseTime(raw); parsed {
		return t, true
	}

	return time.Time{}, false
}

// parseScopeDay parses a date into its day bounds.
func parseScopeDay(raw string) (time.Time, time.Time, bool) {
	if t, parsed := parseTime(raw); parsed {
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)

		return start, start.Add(hoursPerDay), true
	}

	return time.Time{}, time.Time{}, false
}
