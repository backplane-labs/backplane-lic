package portal //nolint:testpackage // inject an in-memory bundle to test assets without a frontend build

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestAssetHeaders(t *testing.T) {
	t.Parallel()

	handler := Handler{basePath: "/portal", next: http.NotFoundHandler(), files: fstest.MapFS{
		"assets/index-123aB_89.js": &fstest.MapFile{Data: []byte("export {}")},
		"favicon.svg":              &fstest.MapFile{Data: []byte("<svg></svg>")},
	}}

	for _, tc := range []struct{ path, cache, contentType string }{
		{"/portal/assets/index-123aB_89.js", "public, max-age=31536000, immutable", "javascript"},
		{"/portal/favicon.svg", "no-cache", "image/svg+xml"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, tc.path, http.NoBody))

			if res.Code != http.StatusOK || res.Header().Get("Cache-Control") != tc.cache {
				t.Fatal(res.Code, res.Header())
			}

			if !strings.Contains(res.Header().Get("Content-Type"), tc.contentType) {
				t.Fatal(res.Header())
			}
		})
	}
}
