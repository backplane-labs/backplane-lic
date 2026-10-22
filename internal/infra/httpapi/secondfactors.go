package httpapi

import (
	"context"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// The second-factor routes are retired with end-user authentication
// (bearers are admin, product and license only): every one answers the
// 501.

// ListUserSecondFactors implements GET /v1/accounts/{account}/users/{userId}/second-factors.
func (a *API) ListUserSecondFactors(
	context.Context, oas.ListUserSecondFactorsParams,
) (*oas.SecondFactorCollectionDocument, error) {
	return nil, apperr.NotImplemented()
}

// CreateUserSecondFactor implements POST /v1/accounts/{account}/users/{userId}/second-factors.
func (a *API) CreateUserSecondFactor(
	context.Context, oas.CreateUserSecondFactorReq, oas.CreateUserSecondFactorParams,
) (*oas.SecondFactorDocumentHeaders, error) {
	return nil, apperr.NotImplemented()
}

// ShowUserSecondFactor implements GET /v1/accounts/{account}/users/{userId}/second-factors/{secondFactorId}.
func (a *API) ShowUserSecondFactor(
	context.Context, oas.ShowUserSecondFactorParams,
) (*oas.SecondFactorDocument, error) {
	return nil, apperr.NotImplemented()
}

// UpdateUserSecondFactor implements the enable/disable actions.
func (a *API) UpdateUserSecondFactor(
	context.Context, oas.UpdateUserSecondFactorReq, oas.UpdateUserSecondFactorParams,
) (*oas.SecondFactorDocument, error) {
	return nil, apperr.NotImplemented()
}

// DeleteUserSecondFactor implements DELETE /v1/accounts/{account}/users/{userId}/second-factors/{secondFactorId}.
func (a *API) DeleteUserSecondFactor(
	context.Context, oas.DeleteUserSecondFactorReq, oas.DeleteUserSecondFactorParams,
) error {
	return apperr.NotImplemented()
}
