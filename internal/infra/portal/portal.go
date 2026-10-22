// Package portal serves the standalone customer portal, outside the public
// JSON:API middleware. It adds no authenticated or private API endpoints.
package portal

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/backplane-labs/backplane-lic/internal/domain"
)

//go:embed all:dist
var assets embed.FS

var (
	errBasePath = errors.New("portal base path must be an absolute, clean path outside /v1")
	errIndex    = errors.New("portal index must contain <head>")
	hashedAsset = regexp.MustCompile(`-[a-zA-Z0-9_-]{8,}\.[a-zA-Z0-9.]+$`)
)

// Config controls the customer portal; disabled unless explicitly enabled.
type Config struct {
	Enabled bool `json:"enabled,omitempty" schemapb:"description=Serve the customer portal"`
	// BasePath defaults to /portal; configuration uses camelCase.
	BasePath string `json:"basePath,omitempty" schemapb:"description=Portal path"` //nolint:tagliatelle // camelCase config
}

type clientConfig struct {
	Brand    domain.Brand `json:"brand"`
	BasePath string       `json:"basePath"` //nolint:tagliatelle // frontend configuration uses camelCase
	Account  string       `json:"account"`
}

// Handler is an immutable portal mount with the public API as its fallback.
type Handler struct {
	basePath string
	index    []byte
	csp      string
	files    fs.FS
	next     http.Handler
}

// New embeds runtime configuration into the index. Like the templates console
// bundle, a committed placeholder keeps a clean checkout buildable; make portal
// replaces it with the actual Vite build before compiling a release.
func New(cfg Config, brand domain.Brand, account string, next http.Handler) (Handler, error) {
	handler := Handler{next: next}
	if !cfg.Enabled {
		return handler, nil
	}

	basePath := strings.TrimSuffix(cfg.BasePath, "/")
	if cfg.BasePath == "/" {
		return Handler{}, errBasePath
	}

	if basePath == "" {
		basePath = "/portal"
	}

	if !validBasePath(basePath) {
		return Handler{}, errBasePath
	}

	files, err := fs.Sub(assets, "dist")
	if err != nil {
		return Handler{}, fmt.Errorf("portal assets: %w", err)
	}

	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		return Handler{}, fmt.Errorf("portal index: %w", err)
	}

	if !bytes.Contains(index, []byte("<head>")) {
		return Handler{}, errIndex
	}

	config, err := json.Marshal(clientConfig{Brand: brand, BasePath: basePath, Account: account})
	if err != nil {
		return Handler{}, fmt.Errorf("portal config: %w", err)
	}

	script := "window.__PORTAL_CONFIG__=" + string(config) + ";"
	handler.basePath = basePath
	handler.index = bytes.Replace(index, []byte("<head>"), []byte(head(basePath, brand, script)), 1)
	handler.csp = policy(brand, script)
	handler.files = files

	return handler, nil
}

// policy allows the inline config by its digest and the brand's image
// origins; everything else is same-origin.
func policy(brand domain.Brand, script string) string {
	digest := sha256.Sum256([]byte(script))
	images := strings.Join(append([]string{"'self'", "data:"}, brand.Origins()...), " ")

	return "default-src 'self'; script-src 'self' 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) +
		"'; style-src 'self' 'unsafe-inline'; img-src " + images + "; connect-src 'self'; " +
		"object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'"
}

// head opens the index with the mount, the brand's title and favicon,
// and the runtime config.
func head(basePath string, brand domain.Brand, script string) string {
	out := `<head><base href="` + html.EscapeString(basePath) + `/"><title>` + html.EscapeString(brand.Name) + `</title>`
	if brand.FaviconURL != "" {
		out += `<link rel="icon" href="` + html.EscapeString(brand.FaviconURL) + `">`
	}

	return out + `<script>` + script + `</script>`
}

func validBasePath(basePath string) bool {
	return strings.HasPrefix(basePath, "/") && !strings.HasPrefix(basePath, "//") &&
		path.Clean(basePath) == basePath && basePath != "/v1" && !strings.HasPrefix(basePath, "/v1/") &&
		!strings.ContainsAny(basePath, "?#%\\\"<> \t\r\n")
}

// ServeHTTP serves only the configured mount. Unknown assets are 404, while
// extensionless client routes receive the SPA index with no-store.
func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.basePath == "" || (r.URL.Path != h.basePath && !strings.HasPrefix(r.URL.Path, h.basePath+"/")) {
		h.next.ServeHTTP(w, r)
		return
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", h.csp)
	w.Header().Set("Cache-Control", "no-store")

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)

		return
	}

	name := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, h.basePath), "/")
	if name != "" && !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}

	if name == "" || name == "index.html" {
		h.serveIndex(w, r)
		return
	}

	h.servePath(w, r, name)
}

func (h Handler) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(h.index))
}

func (h Handler) servePath(w http.ResponseWriter, r *http.Request, name string) {
	body, err := fs.ReadFile(h.files, name)
	if err != nil {
		if path.Ext(name) == "" && !strings.HasPrefix(name, "assets/") {
			h.serveIndex(w, r)
			return
		}

		http.NotFound(w, r)

		return
	}

	if strings.HasPrefix(name, "assets/") && hashedAsset.MatchString(path.Base(name)) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}

	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(body))
}
