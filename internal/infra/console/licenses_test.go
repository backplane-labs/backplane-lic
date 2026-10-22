package console_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/backplane-labs/backplane/pkg/backplane/backplanetest"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/accounts"
	"github.com/backplane-labs/backplane-lic/internal/infra/console"
	"github.com/backplane-labs/backplane-lic/internal/service"
	licconsolev1 "github.com/backplane-labs/backplane-lic/proto/lic/console/v1"
)

type consoleFixture struct {
	srv  *console.Server
	base string
}

func TestLicenseManagementThroughConsole(t *testing.T) {
	t.Parallel()

	store, id := openStore(t)

	handler, err := service.Handler(store, accounts.Spec{ID: id, Slug: "console-" + id.String()}, service.HTTP{}, "", "test")
	if err != nil {
		t.Fatal(err)
	}

	fixture := consoleFixture{
		srv:  console.New(handler, store, id, "test", slog.New(slog.DiscardHandler)),
		base: "/v1/accounts/" + id.String(),
	}
	ctx := backplanetest.Operator(t.Context(), uuid.New())
	product := fixture.create(ctx, t, "products", `{"name":"Console license product"}`, "")
	policyAttrs := `{"name":"Console policy","duration":86400,"strict":false,"maxUses":10,` +
		`"requireCheckIn":true,"checkInInterval":"day"}`
	policy := fixture.create(ctx, t, "policies", policyAttrs,
		`"product":{"data":{"type":"products","id":"`+product+`"}}`)
	user := fixture.create(ctx, t, "users", `{"email":"license-owner@example.com"}`, "")
	group := fixture.create(ctx, t, "groups", `{"name":"Console group"}`, "")
	license := fixture.create(ctx, t, "licenses", `{"name":"Console license"}`,
		`"policy":{"data":{"type":"policies","id":"`+policy+`"}},`+
			`"user":{"data":{"type":"users","id":"`+user+`"}},`+
			`"group":{"data":{"type":"groups","id":"`+group+`"}}`)
	path := "/licenses/" + license

	fixture.call(ctx, t, "GET", "/licenses", "", http.StatusOK)
	fixture.call(ctx, t, "GET", path, "", http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/validate", `{"meta":{"scope":{"product":"`+product+`"}}}`, http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/suspend", "", http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/reinstate", "", http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/renew", "", http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/check-in", "", http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/increment-usage", `{"meta":{"increment":3}}`, http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/decrement-usage", `{"meta":{"decrement":1}}`, http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/actions/reset-usage", "", http.StatusOK)
	fixture.call(ctx, t, "POST", path+"/tokens", "", http.StatusOK)
	fixture.call(ctx, t, "GET", path+"/tokens", "", http.StatusOK)
	fixture.call(ctx, t, "GET", path+"/policy", "", http.StatusOK)
	fixture.call(ctx, t, "GET", path+"/group", "", http.StatusOK)
	fixture.call(ctx, t, "GET", path+"/owner", "", http.StatusOK)

	entitlement := fixture.create(ctx, t, "entitlements", `{"name":"Console feature","code":"CONSOLE_FEATURE"}`, "")
	entitlements := `{"data":[{"type":"entitlements","id":"` + entitlement + `"}]}`
	fixture.call(ctx, t, "POST", path+"/entitlements", entitlements, http.StatusOK)
	fixture.call(ctx, t, "GET", path+"/entitlements", "", http.StatusOK)
	fixture.call(ctx, t, "DELETE", path+"/entitlements", entitlements, http.StatusNoContent)

	member := fixture.create(ctx, t, "users", `{"email":"license-member@example.com"}`, "")
	members := `{"data":[{"type":"users","id":"` + member + `"}]}`
	fixture.call(ctx, t, "POST", path+"/users", members, http.StatusOK)
	fixture.call(ctx, t, "GET", path+"/users", "", http.StatusOK)
	fixture.call(ctx, t, "DELETE", path+"/users", members, http.StatusNoContent)

	checkout := fixture.call(ctx, t, "GET", path+"/actions/check-out", "", http.StatusOK)
	if !strings.Contains(checkout.GetHeaders()["Content-Disposition"], license+".lic") ||
		!strings.Contains(string(checkout.GetBody()), "BEGIN LICENSE FILE") {
		t.Fatalf("checkout download: %v", checkout)
	}

	fixture.call(ctx, t, "DELETE", path+"/actions/revoke", "", http.StatusNoContent)
	fixture.call(ctx, t, "GET", path, "", http.StatusNotFound)
}

func (f consoleFixture) call(ctx context.Context, t *testing.T, method, path, body string, want int32) *licconsolev1.CallResponse {
	t.Helper()

	out, err := f.srv.Call(ctx, &licconsolev1.CallRequest{
		Method: method, Path: f.base + path, Body: []byte(body),
		Query: map[string]string{"page[number]": "1", "page[size]": "10"},
	})
	if err != nil || out.GetStatus() != want {
		t.Fatalf("%s %s: want %d, got %d: %s (%v)", method, path, want, out.GetStatus(), out.GetBody(), err)
	}

	return out
}

func (f consoleFixture) create(ctx context.Context, t *testing.T, resource, attributes, relationships string) string {
	t.Helper()

	body := `{"data":{"type":"` + resource + `","attributes":` + attributes
	if relationships != "" {
		body += `,"relationships":{` + relationships + `}`
	}

	body += `}}`
	out := f.call(ctx, t, "POST", "/"+resource, body, http.StatusCreated)

	var doc struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.Unmarshal(out.GetBody(), &doc); err != nil {
		t.Fatal(err)
	}

	if doc.Data.ID == "" {
		t.Fatalf("missing resource id: %s", out.GetBody())
	}

	return doc.Data.ID
}
