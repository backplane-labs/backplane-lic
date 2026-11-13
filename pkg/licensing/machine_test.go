package licensing_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	licensing "github.com/backplane-labs/backplane-lic/pkg/licensing"
	"github.com/backplane-labs/backplane-lic/pkg/licensing/usage"
)

const usageMachineID = "d3a1d6e0-1f2e-4c9d-8a7b-3f5e9c2b1a01"

// recorded is one request seen by the usage double.
type recorded struct {
	method      string
	rawPath     string
	query       string
	contentType string
	body        []byte
}

// usageDouble answers machine, report-usage and usage calls and records
// every request. Responses are unsigned: the client has no PublicKey.
type usageDouble struct {
	mu   sync.Mutex
	seen []recorded
}

func (d *usageDouble) last(t *testing.T) recorded {
	t.Helper()

	d.mu.Lock()
	defer d.mu.Unlock()

	if len(d.seen) == 0 {
		t.Fatal("no request recorded")
	}

	return d.seen[len(d.seen)-1]
}

func (d *usageDouble) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return len(d.seen)
}

func (d *usageDouble) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	d.mu.Lock()
	d.seen = append(d.seen, recorded{
		method:      r.Method,
		rawPath:     r.URL.EscapedPath(),
		query:       r.URL.RawQuery,
		contentType: r.Header.Get("Content-Type"),
		body:        body,
	})
	d.mu.Unlock()

	w.Header().Set("Content-Type", "application/vnd.api+json")

	switch {
	case strings.HasSuffix(r.URL.Path, "/actions/report-usage"):
		_, _ = io.WriteString(w, `{"meta":{"ack":{"epoch":7,"seq":2,"hash":"abcd"}}}`)
	case strings.HasSuffix(r.URL.Path, "/usage"):
		_, _ = io.WriteString(w, `{"meta":{"epoch":7,"events":[{"seq":3,"event":"ZXY=","hash":"ff"}],`+
			`"head":{"seq":3,"hash":"ff"},"acks":[{"epoch":7,"seq":2,"hash":"abcd"}]}}`)
	default:
		_, _ = io.WriteString(w, `{"data":{"id":"`+usageMachineID+`","type":"machines","attributes":{"fingerprint":"fp"},`+
			`"relationships":{"license":{"data":{"type":"licenses","id":"lic-1"}}}}}`)
	}
}

// newUsageMachine returns a bound machine served by the double.
func newUsageMachine(t *testing.T) (*licensing.Machine, *usageDouble) {
	t.Helper()

	double := &usageDouble{}
	server := httptest.NewServer(double)
	t.Cleanup(server.Close)

	client, err := licensing.New(licensing.Config{APIURL: server.URL, Account: "acct", Token: "tok"})
	if err != nil {
		t.Fatal(err)
	}

	machine, err := client.Machine(context.Background(), usageMachineID)
	if err != nil {
		t.Fatal(err)
	}

	return machine, double
}

func TestMachine_ReportUsage_RequestShape(t *testing.T) {
	t.Parallel()

	machine, double := newUsageMachine(t)

	report, err := usage.NewReport("d/v1", "fp").Epoch(7).Commits(1, 1, [][]byte{[]byte("e")}).Build()
	if err != nil {
		t.Fatal(err)
	}

	ack, err := machine.ReportUsage(context.Background(), report)
	if err != nil {
		t.Fatal(err)
	}

	if ack.Epoch != 7 || ack.Seq != 2 || ack.Hash != "abcd" {
		t.Errorf("ack = %+v", ack)
	}

	got := double.last(t)

	if got.method != http.MethodPost {
		t.Errorf("method = %s", got.method)
	}

	if want := "/v1/machines/" + usageMachineID + "/actions/report-usage"; got.rawPath != want {
		t.Errorf("path = %s, want %s", got.rawPath, want)
	}

	if got.contentType != "application/vnd.api+json" {
		t.Errorf("content type = %q", got.contentType)
	}

	var members map[string]json.RawMessage

	if err := json.Unmarshal(got.body, &members); err != nil {
		t.Fatalf("body %s: %v", got.body, err)
	}

	if _, hasData := members["data"]; hasData {
		t.Errorf("meta-only document carries data: %s", got.body)
	}

	var wire struct {
		Meta struct {
			Report usage.Report `json:"report"`
		} `json:"meta"`
	}

	if err := json.Unmarshal(got.body, &wire); err != nil {
		t.Fatal(err)
	}

	if wire.Meta.Report.Fingerprint != "fp" || wire.Meta.Report.Epoch != 7 || wire.Meta.Report.SeqTo != 1 {
		t.Errorf("report on the wire = %+v", wire.Meta.Report)
	}
}

func TestMachine_Usage_RequestShape(t *testing.T) {
	t.Parallel()

	machine, double := newUsageMachine(t)

	resp, err := machine.Usage(context.Background(), 2, 50)
	if err != nil {
		t.Fatal(err)
	}

	if resp.Epoch != 7 || len(resp.Events) != 1 || resp.Events[0].Seq != 3 || resp.Head.Seq != 3 || len(resp.Acks) != 1 {
		t.Errorf("response = %+v", resp)
	}

	got := double.last(t)

	if got.method != http.MethodGet {
		t.Errorf("method = %s", got.method)
	}

	if want := "/v1/machines/" + usageMachineID + "/usage"; got.rawPath != want {
		t.Errorf("path = %s, want %s", got.rawPath, want)
	}

	if got.query != "after=2&limit=50" {
		t.Errorf("query = %q", got.query)
	}

	if len(got.body) != 0 {
		t.Errorf("GET carries a body: %s", got.body)
	}
}

func TestClient_Machine_RejectsBadIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		ok   bool
	}{
		{"uuid", usageMachineID, true},
		{"fingerprint", strings.Repeat("ab", 32), true},
		{"empty", "", false},
		{"traversal", "..", false},
		{"slash", "a/b", false},
		{"query", "a?x=1", false},
		{"space", "a b", false},
		{"percent", "a%2fb", false},
		{"newline", "a\nb", false},
		{"too long", strings.Repeat("a", 256), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			double := &usageDouble{}
			server := httptest.NewServer(double)
			t.Cleanup(server.Close)

			client, err := licensing.New(licensing.Config{APIURL: server.URL, Account: "acct", Token: "tok"})
			if err != nil {
				t.Fatal(err)
			}

			_, err = client.Machine(context.Background(), tc.id)

			if tc.ok {
				if err != nil {
					t.Fatalf("Machine(%q): %v", tc.id, err)
				}

				if want := "/v1/machines/" + tc.id; double.last(t).rawPath != want {
					t.Errorf("path = %s, want %s", double.last(t).rawPath, want)
				}

				return
			}

			if !errors.Is(err, licensing.ErrInvalidIdentifier) {
				t.Fatalf("Machine(%q) error = %v, want ErrInvalidIdentifier", tc.id, err)
			}

			if double.count() != 0 {
				t.Errorf("invalid id reached the server (%d requests)", double.count())
			}
		})
	}
}
