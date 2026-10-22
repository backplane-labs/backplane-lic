-- name: LicenseNotificationCandidates :many
SELECT l.id, l.last_expiration_event_sent_at, l.last_expiring_soon_event_sent_at,
       l.last_check_in_event_sent_at, l.last_check_in_soon_event_sent_at
FROM lic.licenses l
JOIN lic.policies p ON p.id = l.policy_id
WHERE l.account_id = @account_id
  AND (
    l.expiry BETWEEN @now::timestamptz - interval '3 days' AND @now::timestamptz + interval '3 days'
    OR (
      p.require_check_in
      AND p.check_in_interval_count IS NOT NULL
      AND p.check_in_interval IS NOT NULL
      AND l.last_check_in_at + (p.check_in_interval_count || ' ' || p.check_in_interval)::interval
          BETWEEN @now::timestamptz - interval '3 days' AND @now::timestamptz + interval '3 days'
    )
  )
ORDER BY l.id;

-- name: StampLicenseNotification :exec
UPDATE lic.licenses SET
  last_expiration_event_sent_at = CASE WHEN @event::text = 'license.expired'
    THEN @now::timestamptz ELSE last_expiration_event_sent_at END,
  last_expiring_soon_event_sent_at = CASE WHEN @event::text = 'license.expiring-soon'
    THEN @now::timestamptz ELSE last_expiring_soon_event_sent_at END,
  last_check_in_event_sent_at = CASE WHEN @event::text = 'license.check-in-overdue'
    THEN @now::timestamptz ELSE last_check_in_event_sent_at END,
  last_check_in_soon_event_sent_at = CASE WHEN @event::text = 'license.check-in-required-soon'
    THEN @now::timestamptz ELSE last_check_in_soon_event_sent_at END,
  updated_at = @now::timestamptz
WHERE account_id = @account_id AND id = @id;
