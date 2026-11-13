// The checkout payload: the JSON:API dataset embedded in a license
// or machine file certificate ({data, included, meta}).
package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/licenses"
	"github.com/backplane-labs/backplane-lic/internal/app/machines"
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

	return payload(data.Encode, checkoutMeta{Issued: checkout.IssuedAt, Expiry: checkout.Expiry, TTL: checkout.TTL},
		func(enc *jx.Encoder) {
			for _, name := range checkout.Includes {
				encodeLicenseInclude(enc, accountID, checkout, name)
			}
		})
}

// MachineCheckoutPayload renders the machine-file dataset: the machine,
// its requested includes (the license's own included as license.*).
func MachineCheckoutPayload(accountID uuid.UUID, checkout machines.Checkout, now time.Time) ([]byte, error) {
	data := Machine(accountID, checkout.Machine)

	return payload(data.Encode, checkoutMeta{Issued: checkout.IssuedAt, Expiry: checkout.Expiry, TTL: checkout.TTL},
		func(enc *jx.Encoder) {
			for _, name := range checkout.Includes {
				encodeMachineInclude(enc, accountID, checkout, name, now)
			}
		})
}

func payload(data func(*jx.Encoder), meta checkoutMeta, included func(*jx.Encoder)) ([]byte, error) {
	rawMeta, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("render: checkout meta: %w", err)
	}

	enc := jx.Encoder{}

	enc.ObjStart()
	enc.FieldStart("data")
	data(&enc)
	enc.FieldStart("included")
	enc.ArrStart()
	included(&enc)
	enc.ArrEnd()
	enc.FieldStart("meta")
	enc.Raw(rawMeta)
	enc.ObjEnd()

	return enc.Bytes(), nil
}

func encodeMachineInclude(
	enc *jx.Encoder, accountID uuid.UUID, checkout machines.Checkout, name string, now time.Time,
) {
	switch name {
	case "license":
		license := License(accountID, checkout.License, now, nil)
		license.Encode(enc)
	case "owner":
		if checkout.Owner != nil {
			owner := User(accountID, *checkout.Owner, checkout.OwnerStatus)
			owner.Encode(enc)
		}
	case "group":
		if checkout.Group != nil {
			group := Group(*checkout.Group)
			group.Encode(enc)
		}
	case "components":
		for i := range checkout.Components {
			c := checkout.Components[i]
			c.ProductID, c.LicenseID = checkout.Machine.ProductID, checkout.Machine.LicenseID
			component := Component(accountID, c)
			component.Encode(enc)
		}
	default:
		if rest, ok := strings.CutPrefix(name, "license."); ok {
			encodeLicenseInclude(enc, accountID, checkout.Included, rest)
		}
	}
}

// encodeLicenseInclude renders one requested include of a license.
func encodeLicenseInclude(enc *jx.Encoder, accountID uuid.UUID, checkout licenses.Checkout, name string) {
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
