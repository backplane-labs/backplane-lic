package portal_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane-lic/internal/infra/portal"
)

func TestServing(t *testing.T) {
	t.Parallel()

	fallback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })

	handler, err := portal.New(portal.Config{Enabled: true, Title: "Vendor </script>", BasePath: "/customers"}, "account-id", fallback)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/customers", "/customers/", "/customers/licenses/123", "/customers/reset?token=secret"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, http.NoBody))

			if res.Code != http.StatusOK {
				t.Fatalf("status = %d", res.Code)
			}

			if res.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(res.Header())
			}

			body := res.Body.String()
			if !strings.Contains(body, `<base href="/customers/">`) || !strings.Contains(body, `"account":"account-id"`) {
				t.Fatal(body)
			}

			if strings.Contains(body, `Vendor </script>`) {
				t.Fatal("config was not HTML escaped")
			}

			if !strings.Contains(res.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal(res.Header())
			}

			if res.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Fatal(res.Header())
			}
		})
	}

	for _, path := range []string{"/v1/ping", "/customers-other", "/"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		if res.Code != http.StatusTeapot {
			t.Fatalf("%s: status %d", path, res.Code)
		}
	}

	for _, path := range []string{"/customers/assets/missing-12345678.js", "/customers/does-not-exist.css", "/customers/../secret", "/customers/%2e%2e/secret"} {
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		if res.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d", path, res.Code)
		}
	}

	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodHead, "/customers/reset", http.NoBody))

	if res.Code != http.StatusOK || res.Body.Len() != 0 {
		t.Fatal(res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/customers", http.NoBody))

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatal(res.Code)
	}
}

func TestConfig(t *testing.T) {
	t.Parallel()

	fallback := http.NotFoundHandler()

	disabled, err := portal.New(portal.Config{}, "account", fallback)
	if err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	disabled.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/portal", http.NoBody))

	if res.Code != http.StatusNotFound {
		t.Fatal(res.Code)
	}

	for _, path := range []string{"/", "relative", "/v1", "/v1/portal", "/portal/../oops", "/portal?x=1", "/portal#fragment", "/portal%20x", "/portal\\x", "//host/path"} {
		_, err := portal.New(portal.Config{Enabled: true, BasePath: path}, "account", fallback)
		if err == nil {
			t.Fatalf("accepted base path %q", path)
		}
	}
}
