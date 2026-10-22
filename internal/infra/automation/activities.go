package automation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/backplane-labs/backplane/pkg/backplane/activity"
	"github.com/backplane-labs/backplane/pkg/backplane/deps"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	app "github.com/backplane-labs/backplane-lic/internal/app/automation"
	"github.com/backplane-labs/backplane-lic/internal/app/port"
)

var errNotStarted = errors.New("lic: activities have not started")

// ActivityServer declares handlers at construction and receives the started
// application from the composition root. It never captures an unopened store.
type ActivityServer struct {
	app atomic.Pointer[port.LicenseActivities]
}

func NewActivityServer(scope deps.Scope) *ActivityServer {
	server := &ActivityServer{}
	handle(scope, server, "CreateLicense", port.LicenseActivities.CreateLicense)
	handle(scope, server, "GetLicense", port.LicenseActivities.GetLicense)
	handle(scope, server, "SuspendLicense", port.LicenseActivities.SuspendLicense)
	handle(scope, server, "RenewLicense", port.LicenseActivities.RenewLicense)
	handle(scope, server, "ValidateKey", port.LicenseActivities.ValidateKey)
	handle(scope, server, "IncrementUsage", port.LicenseActivities.IncrementUsage)

	return server
}

func (s *ActivityServer) Set(handler port.LicenseActivities) { s.app.Store(&handler) }

func handle[In, Out any](scope deps.Scope, server *ActivityServer, name string,
	call func(port.LicenseActivities, context.Context, In, string) (Out, error),
) {
	activity.Handle(scope, name, func(ctx context.Context, in In) (Out, error) {
		var zero Out

		handler := server.app.Load()
		if handler == nil {
			return zero, errNotStarted
		}

		info, _ := activity.InfoOf(ctx)

		out, err := call(*handler, ctx, in, info.Key)
		if err != nil {
			return zero, activityError(err)
		}

		return out, nil
	}, activity.StartToClose(time.Minute),
		activity.Describe("Admin licensing operation with durable activity-key idempotency"))
}

func activityError(err error) error {
	var application *apperr.T
	if errors.Is(err, app.ErrMissingKey) || errors.Is(err, app.ErrKeyConflict) ||
		errors.As(err, &application) && application.Status < http.StatusInternalServerError &&
			application.Status != http.StatusTooManyRequests {
		return activity.NonRetryable(err) //nolint:wrapcheck // preserve SDK retry classification
	}

	return fmt.Errorf("lic activity: %w", err)
}
