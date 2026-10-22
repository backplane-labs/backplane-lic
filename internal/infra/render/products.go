// Package render projects domain values onto the generated oas wire
// types. It is shared by the HTTP handlers and the future webhook
// publisher: one renderer, one wire truth.
package render

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// accountLink is the account to-one linkage every resource carries.
func accountLink(accountID uuid.UUID) oas.ToOneRelationship {
	return oas.ToOneRelationship{
		Links: oas.RelationshipLinks{Related: oas.NewNilString("/v1/accounts/" + accountID.String())},
		Data: oas.NilResourceIdentifier{
			Value: oas.ResourceIdentifier{Type: "accounts", ID: accountID.String()},
		},
	}
}

// related is a has-many relationship's related link.
func related(base, path string) oas.ToManyRelationship {
	return oas.ToManyRelationship{
		Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)},
	}
}

// toOne is a belongs-to relationship: related link plus linkage.
func toOne(base, path, typ string, id uuid.UUID) oas.ToOneRelationship {
	return oas.ToOneRelationship{
		Links: oas.RelationshipLinks{Related: oas.NewNilString(base + path)},
		Data: oas.NilResourceIdentifier{
			Value: oas.ResourceIdentifier{Type: typ, ID: id.String()},
		},
	}
}

func timestamp(t time.Time) oas.Timestamp { return oas.Timestamp(jsonapi.Timestamp(t)) }

func nilString(s string) oas.NilString {
	if s == "" {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(s)
}

func nilInt(i *int32) oas.NilInt {
	if i == nil {
		return oas.NilInt{Null: true}
	}

	return oas.NewNilInt(int(*i))
}

func nilInt64(i *int64) oas.NilInt {
	if i == nil {
		return oas.NilInt{Null: true}
	}

	return oas.NewNilInt(int(*i))
}

func nilBool(b *bool) oas.NilBool {
	if b == nil {
		return oas.NilBool{Null: true}
	}

	return oas.NewNilBool(*b)
}

// metadata renders the stored metadata camelCased ({} when empty).
func metadata(stored map[string]any) oas.Metadata {
	camel, _ := jsonapi.CamelizeDeep(stored).(map[string]any)
	if camel == nil {
		camel = map[string]any{}
	}

	out := oas.Metadata{}

	for k, v := range camel {
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}

		out[k] = raw
	}

	return out
}

// Links builds the collection links document (nil Doc answers no
// links at all).
func Links(doc *paging.Doc) oas.OptListLinks {
	if doc == nil {
		return oas.OptListLinks{}
	}

	links := oas.ListLinks{Self: doc.Self}

	if doc.First != nil {
		links.First = oas.NewOptString(*doc.First)
	}

	if doc.Last != nil {
		links.Last = oas.NewOptString(*doc.Last)
	}

	// The offset mode emits prev/next (null at the ends); the keyset
	// mode emits next only (null when the page ran out) and no prev.
	if doc.Meta != nil {
		if doc.Prev != nil {
			links.Prev = oas.NewOptNilString(*doc.Prev)
		} else {
			links.Prev.SetToNull()
		}
	}

	if doc.Next != nil {
		links.Next = oas.NewOptNilString(*doc.Next)
	} else {
		links.Next.SetToNull()
	}

	if doc.Meta != nil {
		links.Meta = oas.NewOptPaginationMeta(oas.PaginationMeta{
			Pages: doc.Meta.Pages, Count: doc.Meta.Count,
		})
	}

	return oas.NewOptListLinks(links)
}

// ProductResource renders a product exactly as the reference's
// ProductSerializer does.
func ProductResource(accountID uuid.UUID, p domain.Product) oas.ProductResource {
	base := "/v1/accounts/" + accountID.String() + "/products/" + p.ID.String()

	return oas.ProductResource{
		Type: oas.ProductResourceTypeProducts,
		ID:   p.ID.String(),
		Attributes: oas.ProductAttributes{
			Name:                 p.Name,
			Code:                 nilString(p.Code),
			DistributionStrategy: nilString(p.DistributionStrategy),
			URL:                  nilString(p.URL),
			Platforms:            p.Platforms,
			Metadata:             metadata(p.Metadata),
			Created:              timestamp(p.CreatedAt),
			Updated:              timestamp(p.UpdatedAt),
		},
		Relationships: oas.ProductRelationships{
			Account:   accountLink(accountID),
			Policies:  related(base, "/policies"),
			Licenses:  related(base, "/licenses"),
			Machines:  related(base, "/machines"),
			Users:     related(base, "/users"),
			Tokens:    related(base, "/tokens"),
			Platforms: related(base, "/platforms"),
			Channels:  related(base, "/channels"),
			Releases:  related(base, "/releases"),
			Artifacts: related(base, "/artifacts"),
		},
		Links: oas.ResourceLinks{Self: base},
	}
}

// ProductToken renders a product token document (raw only on
// creation).
func ProductToken(accountID uuid.UUID, token domain.Token, raw string) oas.TokenDocument {
	base := "/v1/accounts/" + accountID.String()

	// The token attribute exists only on the creation response (the
	// reference omits it, not nulls it, everywhere else).
	secret := oas.OptNilString{}
	if raw != "" {
		secret = oas.NewOptNilString(raw)
	}

	return oas.TokenDocument{
		Data: oas.TokenResource{
			Type: oas.TokenResourceTypeTokens,
			ID:   token.ID.String(),
			Attributes: oas.TokenAttributes{
				Kind:    token.Kind,
				Token:   secret,
				Expiry:  optNilTimeString(token.Expiry),
				Name:    nilPtrString(token.Name),
				Created: timestamp(token.CreatedAt),
				Updated: timestamp(token.UpdatedAt),
			},
			Relationships: oas.TokenRelationships{
				Account: accountLink(accountID),
				Bearer:  toOne(base, "/products/"+token.BearerID.String(), "products", token.BearerID),
			},
			Links: oas.ResourceLinks{Self: base + "/tokens/" + token.ID.String()},
		},
	}
}

func optNilTimeString(t *time.Time) oas.NilString {
	if t == nil {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(jsonapi.Timestamp(*t))
}

func nilPtrString(s *string) oas.NilString {
	if s == nil {
		return oas.NilString{Null: true}
	}

	return oas.NewNilString(*s)
}
