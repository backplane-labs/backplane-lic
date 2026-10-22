package search_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/search"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

type call struct {
	resource, attribute string
	term                search.Term
}

type fakeStore struct {
	calls   []call
	answers map[string][]uuid.UUID
}

func (f *fakeStore) SearchIDs(_ context.Context, _ uuid.UUID, resource, attribute string, term search.Term) ([]uuid.UUID, error) {
	f.calls = append(f.calls, call{resource, attribute, term})

	return f.answers[resource+"."+attribute], nil
}

func admin() context.Context {
	return access.With(context.Background(), access.Principal{Role: domain.RoleAdmin})
}

func request(t *testing.T, body string) search.Request {
	t.Helper()

	meta, err := search.ObjectFields(json.RawMessage(body))
	if err != nil {
		t.Fatal(err)
	}

	out := search.Request{HasMeta: true}

	for _, member := range meta {
		switch member.Key {
		case "type":
			out.HasType = json.Unmarshal(member.Value, &out.Type) == nil
		case "op":
			_ = json.Unmarshal(member.Value, &out.Op)
		case "query":
			query, err := search.ObjectFields(member.Value)
			if err != nil {
				t.Fatal(err)
			}

			out.HasQuery, out.Query = true, query
		}
	}

	return out
}

func detail(t *testing.T, err error) (string, string) {
	t.Helper()

	var failure *apperr.T
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want an API error", err)
	}

	return failure.Detail, failure.Source.Pointer
}

// Where the reference fails with a 500, the search answers a 400.
func TestReferenceCrashesAreBadRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, body, detail, pointer string
	}{
		{"unknown op", `{"type":"users","op":"XOR","query":{"email":"abc"}}`, "search operation 'XOR' is not supported", "/meta/op"},
		{"number for a text term", `{"type":"users","query":{"email":123}}`, "search query for 'email' must be a string", "/meta/query/email"},
		{"object term", `{"type":"licenses","query":{"owner":{"a":1}}}`, "search query for 'owner' must be a string", "/meta/query/owner"},
		{"request logs", `{"type":"request-logs","query":{"url":"abc"}}`, "search type 'requestLogs' is not supported", "/meta/type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := search.New(&fakeStore{}).Search(admin(), domain.Account{}, request(t, tc.body))

			gotDetail, gotPointer := detail(t, err)
			if gotDetail != tc.detail || gotPointer != tc.pointer {
				t.Fatalf("got %q at %q, want %q at %q", gotDetail, gotPointer, tc.detail, tc.pointer)
			}
		})
	}
}

func TestCombinesAndCasts(t *testing.T) {
	t.Parallel()

	a, b, c := uuid.New(), uuid.New(), uuid.New()
	store := &fakeStore{answers: map[string][]uuid.UUID{
		"licenses.name": {a, b}, "licenses.key": {b, c}, "licenses.metadata": {a},
	}}
	scenario := search.New(store)

	and, err := scenario.Search(admin(), domain.Account{}, request(t, `{"type":"license","query":{"name":"abc","key":"KEY-1"}}`))
	if err != nil || len(and.IDs) != 1 || and.IDs[0] != b || and.Resource != search.Licenses {
		t.Fatalf("AND = %+v, %v; want [b] of licenses", and, err)
	}

	or, err := scenario.Search(admin(), domain.Account{}, request(t, `{"type":"licenses","op":"or","query":{"name":"abc","key":"KEY-1"}}`))
	if err != nil || len(or.IDs) != 3 {
		t.Fatalf("OR = %+v, %v; want three ids", or, err)
	}

	_, err = scenario.Search(admin(), domain.Account{}, request(t, `{"type":"licenses","query":{"metadata":{"seatCount":"1042"}}}`))
	if err != nil {
		t.Fatal(err)
	}

	last := store.calls[len(store.calls)-1]
	if string(last.term.Raw) != `{"seat_count":"1042"}` || string(last.term.Cast) != `{"seat_count":1042}` {
		t.Fatalf("metadata documents = %s / %s", last.term.Raw, last.term.Cast)
	}
}

func TestTermForms(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	id := uuid.New()

	_, err := search.New(store).Search(admin(), domain.Account{},
		request(t, `{"type":"licenses","query":{"owner":"`+id.String()+`","name":"50%_off"}}`))
	if err != nil {
		t.Fatal(err)
	}

	if owner := store.calls[0].term; !owner.ID.Valid || owner.ID.UUID != id || !owner.Prefix {
		t.Fatalf("owner term = %+v", owner)
	}

	if name := store.calls[1].term; name.ID.Valid || name.Like != `50\%\_off` || name.Prefix {
		t.Fatalf("name term = %+v", name)
	}
}

func TestFullNameNeedsTwoWords(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}

	result, err := search.New(store).Search(admin(), domain.Account{}, request(t, `{"type":"users","query":{"name":"single"}}`))
	if err != nil || len(result.IDs) != 0 || len(store.calls) != 0 {
		t.Fatalf("one word = %+v, %v, %d calls; want nothing", result, err, len(store.calls))
	}
}

func TestOnlyStaffSearch(t *testing.T) {
	t.Parallel()

	ctx := access.With(context.Background(), access.Principal{Role: domain.RoleProduct})

	_, err := search.New(&fakeStore{}).Search(ctx, domain.Account{}, request(t, `{"type":"users","query":{"email":"abc"}}`))

	var failure *apperr.T
	if !errors.As(err, &failure) || failure.Status != 403 {
		t.Fatalf("product bearer = %v, want 403", err)
	}
}
