// Package domain holds the pure rules of the licensing model: entities,
// enums, sentinels, key schemes and the expiry/status/heartbeat math. No
// infrastructure.
package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// Status of a CE singleplayer account on the wire: Keygen CE reports the
// account as subscribed (the billing state an installation has without
// billing).
const AccountStatusSubscribed = "SUBSCRIBED"

// Account is the single account of the installation. It exists implicitly:
// the row is created at startup from configuration when missing.
type Account struct {
	ID         uuid.UUID
	Name       string
	Slug       string
	APIVersion string
	Protected  bool
	Keys       crypto.AccountKeys
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
