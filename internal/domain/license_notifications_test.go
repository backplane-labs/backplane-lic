package domain_test

import (
	"slices"
	"testing"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

func TestLicenseNotificationWindows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour)
	dayAgo := now.Add(-24 * time.Hour)

	for _, test := range []struct {
		name   string
		expiry time.Duration
		state  domain.LicenseNotificationState
		want   []string
	}{
		{"expired", -time.Hour, domain.LicenseNotificationState{}, []string{"license.expired"}},
		{"expired boundary", -12 * time.Hour, domain.LicenseNotificationState{}, []string{"license.expired"}},
		{"expired outside grace", -12*time.Hour - time.Nanosecond, domain.LicenseNotificationState{}, nil},
		{"expiring", time.Hour, domain.LicenseNotificationState{}, []string{"license.expiring-soon"}},
		{"warning boundary", 72 * time.Hour, domain.LicenseNotificationState{}, nil},
		{"expired throttled", -time.Hour, domain.LicenseNotificationState{Expiration: &recent}, nil},
		{"warning throttled", time.Hour, domain.LicenseNotificationState{ExpiringSoon: &recent}, nil},
		{"daily boundary", time.Hour, domain.LicenseNotificationState{ExpiringSoon: &dayAgo}, []string{"license.expiring-soon"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			expiry := now.Add(test.expiry)

			license := domain.License{Expiry: &expiry}
			if got := domain.LicenseNotifications(license, test.state, now); !slices.Equal(got, test.want) {
				t.Fatalf("events = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLicenseCheckInNotifications(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	count := int32(1)
	checkedIn := now.Add(-25 * time.Hour)

	license := domain.License{
		LastCheckInAt: &checkedIn,
		Policy:        domain.Policy{RequireCheckIn: true, CheckInInterval: "day", CheckInIntervalCount: &count},
	}
	if got := domain.LicenseNotifications(license, domain.LicenseNotificationState{}, now); !slices.Equal(got, []string{"license.check-in-overdue"}) {
		t.Fatalf("overdue events = %v", got)
	}

	checkedIn = now
	if got := domain.LicenseNotifications(license, domain.LicenseNotificationState{}, now); !slices.Equal(got, []string{"license.check-in-required-soon"}) {
		t.Fatalf("soon events = %v", got)
	}

	expiry := now.Add(-time.Hour)

	license.Expiry = &expiry
	if got := domain.LicenseNotifications(license, domain.LicenseNotificationState{}, now); !slices.Equal(got, []string{"license.expired"}) {
		t.Fatalf("expired licenses must not receive check-in notifications: %v", got)
	}
}
