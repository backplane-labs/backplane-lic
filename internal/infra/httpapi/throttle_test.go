package httpapi //nolint:testpackage // directly test fixed-window clocks and the trusted-proxy boundary

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemoryThrottleFixedWindow(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_059, 0)
	limiter := NewMemoryThrottler()
	limiter.clock = func() time.Time { return now }

	for i := int64(1); i <= 6; i++ {
		got, err := limiter.Inc(t.Context(), "identity", time.Minute)
		if err != nil || got != i {
			t.Fatalf("increment: %d %v; want %d", got, err, i)
		}
	}

	now = now.Add(time.Second)

	got, err := limiter.Inc(t.Context(), "identity", time.Minute)
	if err != nil || got != 1 {
		t.Fatalf("fixed-window boundary: %d %v", got, err)
	}
}

func TestTrustedProxyClientIP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, remote, forwarded, want string
		trusted                       []string
	}{
		{"untrusted", "192.0.2.1:8080", "198.51.100.1", "192.0.2.1", nil},
		{"trusted chain", "192.0.2.1:8080", "203.0.113.1, 198.51.100.2, 192.0.2.2", "198.51.100.2", []string{"192.0.2.0/24"}},
		{"trusted host", "[::1]:8080", "198.51.100.3", "198.51.100.3", []string{"::1"}},
		{"empty forwarded", "192.0.2.1:8080", "", "192.0.2.1", []string{"192.0.2.0/24"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := pipeline{trustedProxies: parseCIDRs(tc.trusted)}
			r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", tc.forwarded)

			if got := p.clientIP(r); got != tc.want {
				t.Errorf("client IP %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMFAThrottleIdentityAndResponse(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_001, 0)
	limiter := NewMemoryThrottler()
	limiter.clock = func() time.Time { return now }
	p := pipeline{spec: Spec{ID: uuid.New()}, throttle: limiter, clock: func() time.Time { return now }}
	user := uuid.New().String()
	path := "/v1/accounts/" + p.spec.ID.String() + "/users/" + user + "/second-factors"

	for attempt := int64(1); attempt <= 7; attempt++ {
		method, route := http.MethodPatch, path+"/"+uuid.New().String()
		if attempt == 1 {
			method, route = http.MethodPost, path
		}

		r := httptest.NewRequest(method, route, http.NoBody)
		r.RemoteAddr = "192.0.2.1:8080"
		r.Header.Set("X-Forwarded-For", "198.51.100."+strconv.FormatInt(attempt, 10))
		name, count, window := p.limited(r, route)

		if attempt <= 5 {
			if name != "" {
				t.Fatalf("attempt %d prematurely throttled", attempt)
			}

			continue
		}

		if name != "mutmfa" || count != attempt || window != 10*time.Minute {
			t.Fatalf("MFA throttle: %q %d %s", name, count, window)
		}

		rec := httptest.NewRecorder()
		p.throttleError(rec, name, count, window, now)

		limitCount := rec.Header()["X-RateLimit-Count"][0] //nolint:staticcheck // intentionally match reference spelling
		if rec.Code != http.StatusTooManyRequests || limitCount != strconv.FormatInt(attempt, 10) {
			t.Fatalf("429 response: %d %v", rec.Code, rec.Header())
		}

		if rec.Header().Get("Retry-After") != "599" || strings.Contains(rec.Body.String(), `"meta"`) {
			t.Fatalf("429 window/body: %v %s", rec.Header(), rec.Body.String())
		}
	}

	// Authentication and MFA counters are separate even on the same account/IP.
	r := httptest.NewRequest(http.MethodPost, "/v1/accounts/example-com/tokens", http.NoBody)
	r.RemoteAddr = "192.0.2.1:8080"
	r.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("user@example.com:password")))

	if name, _, _ := p.limited(r, r.URL.Path); name != "" {
		t.Fatal("MFA requests spent the password authentication budget")
	}

	other := strings.Replace(path, user, uuid.New().String(), 1)
	r = httptest.NewRequest(http.MethodPost, other, http.NoBody)
	r.RemoteAddr = "192.0.2.1:8080"

	if name, _, _ := p.limited(r, other); name != "" {
		t.Fatal("one user's MFA requests spent another user's budget")
	}
}
