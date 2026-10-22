package console_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/backplane-labs/backplane/pkg/backplane/backplanetest"
	"github.com/backplane-labs/backplane/pkg/backplane/config"
	sdkpg "github.com/backplane-labs/backplane/pkg/backplane/infra/postgres"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/gopherex/xlog"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/infra/console"
	"github.com/backplane-labs/backplane-lic/internal/infra/postgres"
	"github.com/backplane-labs/backplane-lic/internal/service"
	licconsolev1 "github.com/backplane-labs/backplane-lic/proto/lic/console/v1"
)

func TestRequiresOperator(t *testing.T) {
	t.Parallel()

	srv := console.New(nil, nil, uuid.New(), "test", slog.New(slog.DiscardHandler))
	if _, err := srv.Call(t.Context(), &licconsolev1.CallRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Call without session: %v", err)
	}

	if _, err := srv.Info(t.Context(), &licconsolev1.InfoRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("Info without session: %v", err)
	}
}

func TestCallRejectsInvalidRequests(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]*licconsolev1.CallRequest{
		"nil":        nil,
		"origin":     {Method: "GET", Path: "https://elsewhere/v1/users"},
		"authority":  {Method: "GET", Path: "//elsewhere/v1/users"},
		"query":      {Method: "GET", Path: "/v1/users?token=secret"},
		"fragment":   {Method: "GET", Path: "/v1/users#secret"},
		"non-api":    {Method: "GET", Path: "/internal/secret"},
		"method":     {Method: "CONNECT", Path: "/v1/users"},
		"large body": {Method: "POST", Path: "/v1/users", Body: make([]byte, (1<<20)+1)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := console.New(nil, nil, uuid.New(), "test", slog.New(slog.DiscardHandler))

			ctx := backplanetest.Operator(t.Context(), uuid.New())
			if _, err := srv.Call(ctx, in); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("Call: %v", err)
			}
		})
	}
}

func TestCallPreservesHTTPResponse(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page[number]") != "3" || r.Header.Get("Authorization") != "" {
			t.Errorf("request: %s %v", r.URL, r.Header)
		}

		w.Header().Set("Content-Disposition", `attachment; filename="license.lic"`)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte{0, 255, 42})
	})
	srv := console.New(handler, nil, uuid.New(), "test", slog.New(slog.NewJSONHandler(&logs, nil)))
	session := uuid.New()
	ctx := backplanetest.Operator(t.Context(), session)

	out, err := srv.Call(ctx, &licconsolev1.CallRequest{
		Method: "POST", Path: "/v1/licenses/secret-license-key",
		Query: map[string]string{"page[number]": "3", "key": "secret-query"},
		Body:  []byte(`{"secret":"secret-body"}`),
	})
	if err != nil || out.GetStatus() != http.StatusUnprocessableEntity ||
		!bytes.Equal(out.GetBody(), []byte{0, 255, 42}) || out.GetHeaders()["Content-Disposition"] == "" {
		t.Fatalf("response: %v %v", out, err)
	}

	var audit map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &audit); err != nil {
		t.Fatal(err)
	}

	if audit[backplane.AuditLabel] != true || audit[backplane.AuditActor] != "console:"+session.String() ||
		audit[backplane.AuditOutcome] != "failed" || audit[backplane.AuditID] == "" {
		t.Fatalf("audit: %v", audit)
	}

	if strings.Contains(logs.String(), "secret-query") || strings.Contains(logs.String(), "secret-body") ||
		strings.Contains(logs.String(), "secret-license-key") {
		t.Fatalf("secret in audit: %s", logs.String())
	}
}

// This test uses the actual HTTP pipeline, scenarios, sqld queries, and
// database, with no seeded admin/password/token. Public HTTP still rejects
// the same unauthenticated request; only the operator bridge is privileged.
func TestConsoleAdminPassthroughAndInfo(t *testing.T) {
	t.Parallel()

	store, id := openStore(t)

	handler, err := service.Handler(store, accounts.Spec{ID: id, Slug: "console-" + id.String()}, service.HTTP{}, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer

	srv := console.New(handler, store, id, "v-test", slog.New(slog.NewJSONHandler(&logs, nil)))
	ctx := backplanetest.Operator(t.Context(), uuid.New())
	path := "/v1/accounts/" + id.String() + "/products"
	body := []byte(`{"data":{"type":"products","attributes":{"name":"Console product"}}}`)

	public := httptest.NewRecorder()
	handler.ServeHTTP(public, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, bytes.NewReader(body)))

	if public.Code != http.StatusUnauthorized {
		t.Fatalf("public request: %d %s", public.Code, public.Body.String())
	}

	out, err := srv.Call(ctx, &licconsolev1.CallRequest{Method: "POST", Path: path, Body: body})
	if err != nil || out.GetStatus() != http.StatusCreated {
		t.Fatalf("create product: %v %v", out, err)
	}

	info, err := srv.Info(ctx, &licconsolev1.InfoRequest{})
	if err != nil || info.GetAccountId() != id.String() || info.GetAccountSlug() != "console-"+id.String() ||
		info.GetVersion() != "v-test" || info.GetCounts()["products"] != 1 || len(info.GetPublicKeys()) != 3 {
		t.Fatalf("Info: %v %v", info, err)
	}

	account, err := store.Account(ctx, id)
	if err != nil {
		t.Fatal(err)
	}

	wire, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	for _, secret := range []string{account.Keys.SecretKey, account.Keys.RSAPrivate, account.Keys.Ed25519Private} {
		if strings.Contains(string(wire), secret) {
			t.Fatal("Info leaked private material")
		}
	}

	if !strings.Contains(logs.String(), `"backplane.audit.outcome":"succeeded"`) {
		t.Fatalf("missing successful audit: %s", logs.String())
	}

	testAdminToken(ctx, t, srv, id)
}

func testAdminToken(ctx context.Context, t *testing.T, srv *console.Server, id uuid.UUID) {
	t.Helper()

	base := "/v1/accounts/" + id.String()

	out, err := srv.Call(ctx, &licconsolev1.CallRequest{
		Method: "POST", Path: base + "/users",
		Body: []byte(`{"data":{"type":"users","attributes":{"email":"console-admin@example.com","role":"admin"}}}`),
	})
	if err != nil || out.GetStatus() != http.StatusCreated {
		t.Fatalf("admin create: %v %v", out, err)
	}

	var doc struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(out.GetBody(), &doc); err != nil {
		t.Fatal(err)
	}

	out, err = srv.Call(ctx, &licconsolev1.CallRequest{Method: "POST", Path: base + "/users/" + doc.Data.ID + "/tokens"})
	if err != nil || out.GetStatus() != http.StatusOK || !bytes.Contains(out.GetBody(), []byte("admin-")) {
		t.Fatalf("admin mint: %v %v", out, err)
	}

	var minted struct {
		Data struct {
			Attributes struct {
				Token string `json:"token"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.GetBody(), &minted); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(minted.Data.Attributes.Token, "admin-") {
		t.Fatal("missing minted admin token secret")
	}

	out, err = srv.Call(ctx, &licconsolev1.CallRequest{Method: "GET", Path: base + "/tokens"})
	if err != nil || out.GetStatus() != http.StatusOK || bytes.Contains(out.GetBody(), []byte(minted.Data.Attributes.Token)) {
		t.Fatalf("token list must not repeat secret: %v %v", out, err)
	}
}

func openStore(t *testing.T) (*postgres.Store, uuid.UUID) {
	t.Helper()

	dsn := os.Getenv("LIC_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LIC_TEST_DATABASE_URL not set")
	}

	store, err := postgres.Open(t.Context(), xlog.NewJSON(xlog.WithWriter(io.Discard)), sdkpg.Config{DSN: config.Secret(dsn)})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(store.Close)

	id := uuid.New()

	if _, err := accounts.Bootstrap(t.Context(), store.Accounts, accounts.Spec{ID: id, Name: "Console test", Slug: "console-" + id.String()}); err != nil {
		t.Fatal(err)
	}

	return store, id
}
