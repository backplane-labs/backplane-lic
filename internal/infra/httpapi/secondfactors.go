package httpapi

import (
	"context"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/render"
	"github.com/backplane-labs/backplane-lic/internal/oas"
)

// userIDsOf resolves the {userId} path param of the user sub-routes
// (UUID or email alias) to the user row.
func (a *API) subUser(ctx context.Context, req *request, alias string) (domain.User, error) {
	user, err := a.users.Show(ctx, req.account, req.principal, alias)
	if err != nil {
		return domain.User{}, err //nolint:wrapcheck // the scenario's own error
	}

	return user.User, nil
}

// ListUserSecondFactors implements GET /users/{userId}/second-factors.
func (a *API) ListUserSecondFactors(
	ctx context.Context, params oas.ListUserSecondFactorsParams,
) (*oas.SecondFactorCollectionDocument, error) {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return nil, err
	}

	factors, err := a.secondFactors.List(ctx, req.account, req.principal, user.ID, limitOf(req))
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	doc := &oas.SecondFactorCollectionDocument{Data: make([]oas.SecondFactorResource, 0, len(factors))}
	for i := range factors {
		factor := factors[i]
		doc.Data = append(doc.Data, render.SecondFactor(req.account.ID, factor, user.Email))
	}

	return doc, nil
}

// CreateUserSecondFactor implements POST /users/{userId}/second-factors.
func (a *API) CreateUserSecondFactor(
	ctx context.Context, _ oas.CreateUserSecondFactorReq, params oas.CreateUserSecondFactorParams,
) (*oas.SecondFactorDocumentHeaders, error) {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return nil, err
	}

	password, otp, err := validateSecondFactorCreate(req)
	if err != nil {
		return nil, err
	}

	factor, err := a.secondFactors.Create(ctx, req.account, req.principal, user.ID, password, otp)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.SecondFactorDocumentHeaders{
		Location: oas.NewOptString(req.location("/v1/accounts/" +
			req.account.ID.String() + "/users/" + user.ID.String() +
			"/second-factors/" + factor.ID.String())),
		Response: oas.SecondFactorDocument{
			Data: render.SecondFactor(req.account.ID, factor, user.Email),
		},
	}, nil
}

// ShowUserSecondFactor implements GET /users/{userId}/second-factors/{id}.
func (a *API) ShowUserSecondFactor(
	ctx context.Context, params oas.ShowUserSecondFactorParams,
) (*oas.SecondFactorDocument, error) {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return nil, err
	}

	factorID, err := uuid.Parse(params.SecondFactorId)
	if err != nil {
		return nil, apperr.NotFound("second factor", params.SecondFactorId)
	}

	factor, err := a.secondFactors.Show(ctx, req.account, req.principal, user.ID, factorID)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.SecondFactorDocument{
		Data: render.SecondFactor(req.account.ID, factor, user.Email),
	}, nil
}

// UpdateUserSecondFactor implements PATCH /users/{userId}/second-factors/{id}.
func (a *API) UpdateUserSecondFactor(
	ctx context.Context, _ oas.UpdateUserSecondFactorReq, params oas.UpdateUserSecondFactorParams,
) (*oas.SecondFactorDocument, error) {
	return a.updateUserSecondFactor(ctx, params)
}

// ReplaceUserSecondFactor is the reference's PUT alias of PATCH.
func (a *API) ReplaceUserSecondFactor(
	ctx context.Context, _ oas.ReplaceUserSecondFactorReq, params oas.ReplaceUserSecondFactorParams,
) (*oas.SecondFactorDocument, error) {
	return a.updateUserSecondFactor(ctx, oas.UpdateUserSecondFactorParams(params))
}

func (a *API) updateUserSecondFactor(
	ctx context.Context, params oas.UpdateUserSecondFactorParams,
) (*oas.SecondFactorDocument, error) {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return nil, err
	}

	factorID, err := uuid.Parse(params.SecondFactorId)
	if err != nil {
		return nil, apperr.NotFound("second factor", params.SecondFactorId)
	}

	enabled, otp, err := validateSecondFactorUpdate(req)
	if err != nil {
		return nil, err
	}

	factor, err := a.secondFactors.Update(
		ctx, req.account, req.principal, user.ID, factorID, enabled, otp,
	)
	if err != nil {
		return nil, err //nolint:wrapcheck // the scenario's own error
	}

	return &oas.SecondFactorDocument{
		Data: render.SecondFactor(req.account.ID, factor, user.Email),
	}, nil
}

// DeleteUserSecondFactor implements DELETE /users/{userId}/second-factors/{id}.
func (a *API) DeleteUserSecondFactor(
	ctx context.Context, _ oas.DeleteUserSecondFactorReq, params oas.DeleteUserSecondFactorParams,
) error {
	req := requestOf(ctx)

	user, err := a.subUser(ctx, req, params.UserId)
	if err != nil {
		return err
	}

	factorID, err := uuid.Parse(params.SecondFactorId)
	if err != nil {
		return apperr.NotFound("second factor", params.SecondFactorId)
	}

	otp := secondFactorDeleteOTP(req)

	err = a.secondFactors.Delete(ctx, req.account, req.principal, user.ID, factorID, otp)

	return err //nolint:wrapcheck // the scenario's own error
}

// validateSecondFactorCreate decodes the create body: optional data with
// the type, meta{password?, otp?}.
func validateSecondFactorCreate(req *request) (string, string, error) {
	body := secondFactorBody(req)

	if err := unpermitted(body, "/", "data", "meta"); err != nil {
		return "", "", err
	}

	if dataRaw, ok := body["data"]; ok {
		data, err := hash(dataRaw, "/data")
		if err != nil {
			return "", "", err
		}

		if err := unpermitted(data, "/data/", "type"); err != nil {
			return "", "", err
		}

		typeStr, err := str(data["type"], pointerOf("data", "type"))
		if err != nil {
			return "", "", err
		}

		if !secondFactorType(typeStr) {
			return "", "", apperr.BadRequestSource("is invalid", pointerOf("data", "type"))
		}
	}

	password, otp, err := secondFactorMeta(body, "otp", "password")
	if err != nil {
		return "", "", err
	}

	return password, otp, nil
}

// validateSecondFactorUpdate decodes the PATCH body: attributes.enabled
// (required boolean) and meta.otp (required).
func validateSecondFactorUpdate(req *request) (bool, string, error) {
	body := secondFactorBody(req)

	if err := unpermitted(body, "/", "data", "meta"); err != nil {
		return false, "", err
	}

	dataRaw, ok := body["data"]
	if !ok {
		return false, "", apperr.BadRequestSource("is missing", "/data")
	}

	data, err := hash(dataRaw, "/data")
	if err != nil {
		return false, "", err
	}

	if err := unpermitted(data, "/data/", "type", "id", "attributes"); err != nil {
		return false, "", err
	}

	typeStr, err := str(data["type"], pointerOf("data", "type"))
	if err != nil {
		return false, "", err
	}

	if !secondFactorType(typeStr) {
		return false, "", apperr.BadRequestSource("is invalid", pointerOf("data", "type"))
	}

	attrsRaw, ok := data["attributes"]
	if !ok {
		return false, "", apperr.BadRequestSource("is missing", pointerOf("data", "attributes"))
	}

	attrs, err := hash(attrsRaw, pointerOf("data", "attributes"))
	if err != nil {
		return false, "", err
	}

	if err := unpermitted(attrs, "/data/attributes/", "enabled"); err != nil {
		return false, "", err
	}

	enabledRaw, ok := attrs["enabled"]
	if !ok {
		return false, "", apperr.BadRequestSource("is missing", pointerOf("data", "attributes", "enabled"))
	}

	enabled, ok := enabledRaw.(bool)
	if !ok {
		return false, "", apperr.BadRequestSource(
			"type mismatch (received "+jsonKind(enabledRaw)+" expected boolean)",
			pointerOf("data", "attributes", "enabled"),
		)
	}

	_, otp, err := secondFactorMeta(body, "otp", "")
	if err != nil {
		return false, "", err
	}

	if _, present := secondFactorMetaRaw(body)["otp"]; !present {
		return false, "", apperr.BadRequestSource("is missing", pointerOf("meta", "otp"))
	}

	return enabled, otp, nil
}

// secondFactorDeleteOTP reads the optional meta.otp of the delete body
// (an absent code simply fails verification, like the reference's
// verify(nil)).
func secondFactorDeleteOTP(req *request) string {
	body := secondFactorBody(req)

	meta, _ := body["meta"].(map[string]any)
	if meta == nil {
		return ""
	}

	otp, _ := meta["otp"].(string)

	return otp
}

func secondFactorBody(req *request) map[string]any {
	if req.body == nil {
		return map[string]any{}
	}

	return req.body
}

func secondFactorMetaRaw(body map[string]any) map[string]any {
	meta, _ := body["meta"].(map[string]any)
	if meta == nil {
		meta = map[string]any{}
	}

	return meta
}

// secondFactorMeta validates meta's permitted keys and answers the
// password and otp values.
func secondFactorMeta(body map[string]any, otpKey, passwordKey string) (string, string, error) {
	metaRaw, ok := body["meta"]
	if !ok {
		return "", "", apperr.BadRequestSource("is missing", "/meta")
	}

	meta, err := hash(metaRaw, "/meta")
	if err != nil {
		return "", "", err
	}

	if err := unpermitted(meta, "/meta/", "password", "otp"); err != nil {
		return "", "", err
	}

	password := ""

	if raw, present := meta[passwordKey]; present {
		password, err = str(raw, pointerOf("meta", passwordKey))
		if err != nil {
			return "", "", err
		}
	}

	otp := ""

	if raw, present := meta[otpKey]; present {
		otp, err = str(raw, pointerOf("meta", otpKey))
		if err != nil {
			return "", "", err
		}
	}

	return password, otp, nil
}

func secondFactorType(t string) bool {
	switch t {
	case "second-factor", "second-factors", "secondFactor", "secondFactors",
		"second_factor", "second_factors":
		return true
	default:
		return false
	}
}
