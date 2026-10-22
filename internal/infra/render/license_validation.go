package render

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/jsonapi"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// LicenseValidationMeta is shared by API responses and validation webhooks.
func LicenseValidationMeta(event domain.LicenseValidationEvent) oas.LicenseValidateMeta {
	meta := oas.LicenseValidateMeta{
		Ts: Timestamp(event.Time), Valid: event.Valid, Detail: event.Detail, Code: event.Code,
	}
	if event.Nonce != nil {
		meta.Nonce = oas.NewOptInt(int(*event.Nonce))
	}

	if event.Scope != nil {
		scope := oas.LicenseValidateMetaScope{}

		for key, value := range event.Scope {
			raw, err := json.Marshal(value)
			if err != nil {
				continue // request metadata has already been validated as JSON
			}

			scope[jsonapi.CamelizeKey(key)] = raw
		}

		meta.Scope = oas.NewOptLicenseValidateMetaScope(scope)
	}

	return meta
}

// LicenseValidation renders the event's complete JSON:API document.
func LicenseValidation(accountID uuid.UUID, event domain.LicenseValidationEvent) oas.LicenseValidateDocument {
	return oas.LicenseValidateDocument{
		Data: oas.NilLicenseResource{Value: License(accountID, event.License, event.Time, nil)},
		Meta: LicenseValidationMeta(event),
	}
}
