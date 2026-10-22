// The checkout payload: the JSON:API dataset embedded in a license
// file certificate ({data, included, meta}).
package render

import (
	"encoding/json"
	"time"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
)

// checkoutMeta is the file's {issued, expiry, ttl} meta.
type checkoutMeta struct {
	Issued time.Time  `json:"issued"`
	Expiry *time.Time `json:"expiry"`
	TTL    *int32     `json:"ttl"`
}

// CheckoutPayload renders the license-file dataset of a checkout. It
// encodes through the generated jx encoders (encoding/json would
// marshal the optional relationship wrappers' zero values).
func CheckoutPayload(
	accountID uuid.UUID, checkout licenses.Checkout, now time.Time,
) ([]byte, error) {
	data := License(accountID, checkout.License, now, nil)

	meta, err := json.Marshal(checkoutMeta{
		Issued: checkout.IssuedAt, Expiry: checkout.Expiry, TTL: checkout.TTL,
	})
	if err != nil {
		return nil, err //nolint:wrapcheck // the renderer's own error
	}

	enc := jx.Encoder{}

	enc.ObjStart()
	enc.FieldStart("data")
	data.Encode(&enc)
	enc.FieldStart("included")
	encodeIncludes(&enc, accountID, checkout)
	enc.FieldStart("meta")
	enc.Raw(meta)
	enc.ObjEnd()

	return enc.Bytes(), nil
}

// encodeIncludes renders the requested included documents.
func encodeIncludes(enc *jx.Encoder, accountID uuid.UUID, checkout licenses.Checkout) {
	enc.ArrStart()

	for _, name := range checkout.Includes {
		switch name {
		case "product":
			if checkout.Product != nil {
				product := ProductResource(accountID, *checkout.Product)
				product.Encode(enc)
			}
		case "policy":
			if checkout.Policy != nil {
				policy := PolicyResource(accountID, *checkout.Policy)
				policy.Encode(enc)
			}
		case "owner":
			if checkout.Owner != nil {
				owner := User(accountID, *checkout.Owner, checkout.OwnerStatus)
				owner.Encode(enc)
			}
		case "users":
			for index := range checkout.Users {
				status := ""

				if index < len(checkout.UserStatuses) {
					status = checkout.UserStatuses[index]
				}

				user := User(accountID, checkout.Users[index], status)
				user.Encode(enc)
			}
		case "group":
			if checkout.Group != nil {
				group := Group(*checkout.Group)
				group.Encode(enc)
			}
		case "entitlements":
			for i := range checkout.Entitlements {
				entitlement := EntitlementResource(accountID, checkout.Entitlements[i])
				entitlement.Encode(enc)
			}
		}
	}

	enc.ArrEnd()
}
