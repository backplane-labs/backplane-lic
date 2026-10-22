package domain

import "time"

// LicenseNotificationState persists the reference's per-event daily throttle.
type LicenseNotificationState struct {
	Expiration     *time.Time
	ExpiringSoon   *time.Time
	CheckInOverdue *time.Time
	CheckInSoon    *time.Time
}

// LicenseNotifications is the event selection of the reference's expiration
// and overdue-check-in workers. Expired/overdue notices stop after twelve
// hours; advance warnings cover the next three days. Throttles are per kind.
func LicenseNotifications(license License, state LicenseNotificationState, now time.Time) []string {
	const (
		day     = 24 * time.Hour
		warning = 3 * day
		grace   = 12 * time.Hour
	)

	due := func(last *time.Time) bool { return last == nil || !last.After(now.Add(-day)) }

	var events []string

	if license.Expiry != nil {
		switch {
		case license.Expired(now):
			if !license.Expiry.Before(now.Add(-grace)) && due(state.Expiration) {
				events = append(events, "license.expired")
			}
		case license.Expiry.Before(now.Add(warning)) && due(state.ExpiringSoon):
			events = append(events, "license.expiring-soon")
		}
	}

	if license.Expired(now) {
		return events
	}

	if next := license.NextCheckIn(); next != nil {
		switch {
		case license.CheckInOverdue(now):
			if !next.Before(now.Add(-grace)) && due(state.CheckInOverdue) {
				events = append(events, "license.check-in-overdue")
			}
		case next.Before(now.Add(warning)) && due(state.CheckInSoon):
			events = append(events, "license.check-in-required-soon")
		}
	}

	return events
}
