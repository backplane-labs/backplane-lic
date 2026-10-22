package domain_test

import (
	"errors"
	"net"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// stubResolver answers fixed addresses.
type stubResolver struct {
	addrs []string
	err   error
}

func (r stubResolver) LookupHost(string) ([]string, error) { return r.addrs, r.err }

var errResolve = errors.New("no such host")

func TestValidateWebhookURL(t *testing.T) {
	t.Parallel()

	public := "93.184.216.34"

	tests := []struct {
		name    string
		url     string
		resolve stubResolver
		allow   string // an extra allowed private CIDR
		want    []string
	}{
		{
			name: "valid https url",
			url:  "https://example.com/hook?x=1",
			want: nil,
		},
		{
			name: "unparseable",
			url:  "://",
			want: []string{"URL_INVALID"},
		},
		{
			name: "http scheme",
			url:  "http://example.com/hook",
			want: []string{"URL_PROTOCOL_INVALID"},
		},
		{
			name:    "localhost is blacklisted and private",
			url:     "https://localhost/hook",
			resolve: stubResolver{addrs: []string{"127.0.0.1"}},
			want:    []string{"URL_HOST_INVALID", "URL_ADDRESS_INVALID"},
		},
		{
			name: "keygen.sh is blacklisted",
			url:  "https://api.keygen.sh/hook",
			want: []string{"URL_HOST_INVALID"},
		},
		{
			name: "subdomain of blacklisted host",
			url:  "https://eu.api.keygen.sh/hook",
			want: []string{"URL_HOST_INVALID"},
		},
		{
			name:    "host without a dotted suffix",
			url:     "https://intranet/hook",
			resolve: stubResolver{err: errResolve},
			want:    []string{"URL_HOST_INVALID", "URL_ADDRESS_INVALID"},
		},
		{
			name:    "numeric tld",
			url:     "https://example.c0m/hook",
			resolve: stubResolver{err: errResolve},
			want:    []string{"URL_HOST_INVALID", "URL_ADDRESS_INVALID"},
		},
		{
			name:    "unresolvable host",
			url:     "https://gone.example.com/hook",
			resolve: stubResolver{err: errResolve},
			want:    []string{"URL_ADDRESS_INVALID"},
		},
		{
			name:    "private address",
			url:     "https://internal.example.com/hook",
			resolve: stubResolver{addrs: []string{"10.1.2.3"}},
			want:    []string{"URL_ADDRESS_INVALID"},
		},
		{
			name:    "private address allowed by the allowlist",
			url:     "https://internal.example.com/hook",
			resolve: stubResolver{addrs: []string{"10.1.2.3"}},
			allow:   "10.0.0.0/8",
			want:    nil,
		},
		{
			name:    "one private among public addresses",
			url:     "https://mixed.example.com/hook",
			resolve: stubResolver{addrs: []string{public, "192.168.0.1"}},
			want:    []string{"URL_ADDRESS_INVALID"},
		},
		{
			name:    "ipv6 loopback",
			url:     "https://v6.example.com/hook",
			resolve: stubResolver{addrs: []string{"::1"}},
			want:    []string{"URL_ADDRESS_INVALID"},
		},
		{
			name:    "ipv6 public",
			url:     "https://v6.example.com/hook",
			resolve: stubResolver{addrs: []string{"2606:2800:220:1:248:1893:25c8:1946"}},
			want:    nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolver := tc.resolve

			if resolver.addrs == nil && resolver.err == nil {
				resolver = stubResolver{addrs: []string{public}}
			}

			var allowed []*net.IPNet

			if tc.allow != "" {
				if _, network, err := net.ParseCIDR(tc.allow); err == nil {
					allowed = append(allowed, network)
				}
			}

			got := domain.ValidateWebhookURL(tc.url, resolver, allowed)

			if len(got) != len(tc.want) {
				t.Fatalf("ValidateWebhookURL(%q) = %v, want codes %v", tc.url, got, tc.want)
			}

			for i, violation := range got {
				if violation.Code != tc.want[i] {
					t.Errorf("violation %d = %s (%s), want %s", i, violation.Code, violation.Detail, tc.want[i])
				}
			}
		})
	}
}
