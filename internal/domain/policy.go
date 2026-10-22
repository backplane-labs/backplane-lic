package domain

import (
	"time"

	"github.com/google/uuid"
)

// Policy is a product's licensing rule set. Every strategy column is a
// nullable enum; the zero-value strings mean null on the wire.
type Policy struct {
	ID        uuid.UUID
	AccountID uuid.UUID
	ProductID uuid.UUID
	Name      string

	Duration     *int32
	Strict       bool
	Floating     bool
	UsePool      bool
	MaxMachines  *int32
	MaxProcesses *int32
	MaxUsers     *int32
	MaxCores     *int32
	MaxMemory    *int64
	MaxDisk      *int64
	MaxUses      *int32

	MachineUniquenessStrategy   string
	MachineMatchingStrategy     string
	ComponentUniquenessStrategy string
	ComponentMatchingStrategy   string
	ExpirationStrategy          string
	ExpirationBasis             string
	RenewalBasis                string
	TransferStrategy            string
	AuthenticationStrategy      string
	MachineLeasingStrategy      string
	ProcessLeasingStrategy      string
	OverageStrategy             string
	Scheme                      string
	Encrypted                   bool
	// Protected is nil until created (it then inherits the account's
	// protected flag); PATCH may set it explicitly.
	Protected *bool

	RequireProductScope           bool
	RequirePolicyScope            bool
	RequireMachineScope           bool
	RequireFingerprintScope       bool
	RequireComponentsScope        bool
	RequireUserScope              bool
	RequireChecksumScope          bool
	RequireVersionScope           bool
	RequireCheckIn                bool
	CheckInInterval               string
	CheckInIntervalCount          *int32
	HeartbeatDuration             *int32
	HeartbeatCullStrategy         string
	HeartbeatResurrectionStrategy string
	HeartbeatBasis                string
	RequireHeartbeat              bool

	Metadata             map[string]any
	CreatedAt, UpdatedAt time.Time
}

// Policy crypto schemes (Policy::CRYPTO_SCHEMES).
func PolicyCryptoSchemes() []string {
	return []string{
		"LEGACY_ENCRYPT", "RSA_2048_PKCS1_ENCRYPT", "RSA_2048_PKCS1_SIGN",
		"RSA_2048_PKCS1_PSS_SIGN", "RSA_2048_JWT_RS256", "RSA_2048_PKCS1_SIGN_V2",
		"RSA_2048_PKCS1_PSS_SIGN_V2", "ED25519_SIGN", "ECDSA_P256_SIGN",
	}
}

// Strategy enum sets, transcribed from Policy's SCOPES/constants.
func MachineUniquenessStrategies() []string {
	return []string{"UNIQUE_PER_ACCOUNT", "UNIQUE_PER_PRODUCT", "UNIQUE_PER_POLICY", "UNIQUE_PER_LICENSE"}
}

func ComponentUniquenessStrategies() []string {
	return []string{
		"UNIQUE_PER_ACCOUNT", "UNIQUE_PER_PRODUCT", "UNIQUE_PER_POLICY",
		"UNIQUE_PER_LICENSE", "UNIQUE_PER_MACHINE",
	}
}

func MatchingStrategies() []string {
	return []string{"MATCH_ANY", "MATCH_TWO", "MATCH_MOST", "MATCH_ALL"}
}

func ExpirationStrategies() []string {
	return []string{"RESTRICT_ACCESS", "REVOKE_ACCESS", "MAINTAIN_ACCESS", "ALLOW_ACCESS"}
}

func ExpirationBases() []string {
	return []string{
		"FROM_CREATION", "FROM_FIRST_VALIDATION", "FROM_FIRST_ACTIVATION",
		"FROM_FIRST_DOWNLOAD", "FROM_FIRST_USE",
	}
}

func RenewalBases() []string {
	return []string{"FROM_EXPIRY", "FROM_NOW", "FROM_NOW_IF_EXPIRED"}
}

func TransferStrategies() []string {
	return []string{"RESET_EXPIRY", "KEEP_EXPIRY"}
}

func AuthenticationStrategies() []string {
	return []string{"TOKEN", "LICENSE", "SESSION", "MIXED", "NONE"}
}

func MachineLeasingStrategies() []string {
	return []string{"PER_LICENSE", "PER_USER"}
}

func ProcessLeasingStrategies() []string {
	return []string{"PER_LICENSE", "PER_MACHINE", "PER_USER"}
}

func OverageStrategies() []string {
	return []string{
		"ALWAYS_ALLOW_OVERAGE", "ALLOW_1_25X_OVERAGE", "ALLOW_1_5X_OVERAGE",
		"ALLOW_2X_OVERAGE", "NO_OVERAGE",
	}
}

func HeartbeatCullStrategies() []string { return []string{"DEACTIVATE_DEAD", "KEEP_DEAD"} }

func HeartbeatResurrectionStrategies() []string {
	return []string{
		"ALWAYS_REVIVE", "15_MINUTE_REVIVE", "10_MINUTE_REVIVE", "5_MINUTE_REVIVE",
		"2_MINUTE_REVIVE", "1_MINUTE_REVIVE", "NO_REVIVE",
	}
}

func HeartbeatBases() []string { return []string{"FROM_CREATION", "FROM_FIRST_PING"} }

func CheckInIntervals() []string { return []string{"day", "week", "month", "year"} }

// The policy create defaults (v1.7): every nil strategy defaults, and a
// node-locked policy with no explicit maxMachines gets exactly one.
func PolicyCreateDefaults() Policy {
	return Policy{
		Strict:                        false,
		MachineUniquenessStrategy:     "UNIQUE_PER_LICENSE",
		MachineMatchingStrategy:       "MATCH_ANY",
		ComponentUniquenessStrategy:   "UNIQUE_PER_MACHINE",
		ComponentMatchingStrategy:     "MATCH_ANY",
		ExpirationStrategy:            "RESTRICT_ACCESS",
		ExpirationBasis:               "FROM_CREATION",
		RenewalBasis:                  "FROM_EXPIRY",
		TransferStrategy:              "KEEP_EXPIRY",
		AuthenticationStrategy:        "TOKEN",
		MachineLeasingStrategy:        "PER_LICENSE",
		ProcessLeasingStrategy:        "PER_MACHINE",
		OverageStrategy:               "NO_OVERAGE",
		HeartbeatCullStrategy:         "DEACTIVATE_DEAD",
		HeartbeatResurrectionStrategy: "NO_REVIVE",
		HeartbeatBasis:                "FROM_FIRST_PING",
	}
}
