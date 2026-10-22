// Package entitlements is the entitlement scenario: the account-level
// CRUD of the features policies and licenses attach.
package entitlements

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// ErrCodeTaken is the unique-index violation of (account, lower(code)).
var ErrCodeTaken = errors.New("entitlements: code taken")

// Store is the scenario's consumer-side storage port.
type Store interface {
	// Do runs fn in one transaction; the writes and the webhook
	// events of one action commit together.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
	// EntitlementByID loads an entitlement; ErrNotFound when none.
	EntitlementByID(ctx context.Context, accountID, id uuid.UUID) (domain.Entitlement, error)
	// EntitlementByCode loads an entitlement by downcased code.
	EntitlementByCode(ctx context.Context, accountID uuid.UUID, code string) (domain.Entitlement, error)
	// InsertEntitlement writes a new row; ErrCodeTaken on a collision.
	InsertEntitlement(ctx context.Context, entitlement domain.Entitlement) error
	// UpdateEntitlement overwrites the mutable columns.
	UpdateEntitlement(ctx context.Context, entitlement domain.Entitlement) error
	// DeleteEntitlement destroys the row; false when gone.
	DeleteEntitlement(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	// ListEntitlements pages the account's entitlements.
	ListEntitlements(ctx context.Context, accountID uuid.UUID, page paging.Page) ([]domain.Entitlement, error)
	// CountEntitlements counts the account's entitlements.
	CountEntitlements(ctx context.Context, accountID uuid.UUID) (int, error)
	// UserEntitlementIDs answers the entitlements of the user's licenses.
	UserEntitlementIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// LicenseEntitlementIDs answers the entitlements of one license.
	LicenseEntitlementIDs(ctx context.Context, accountID, licenseID uuid.UUID) ([]uuid.UUID, error)
}

// Input is the request body of create and update (already decoded and
// shape-validated by the handler): the Set flags distinguish an absent
// attribute from an explicit null.
type Input struct {
	NameSet    bool
	Name       *string
	CodeSet    bool
	Code       *string
	Metadata   map[string]any
	MetadataOn bool
}

// Scenario runs the entitlement reads and writes.
type Scenario struct {
	store  Store
	events events.Publisher
	clock  func() time.Time
}

// New builds the scenario.
func New(store Store, publisher events.Publisher, clock func() time.Time) Scenario {
	return Scenario{store: store, events: publisher, clock: clock}
}

// List is one index page.
type List struct {
	Items []domain.Entitlement
	Count int
	Next  uuid.NullUUID
}

// Index answers the entitlements page the principal may see (the
// user/license post-check narrows it to their own).
func (s Scenario) Index(ctx context.Context, account domain.Account, params paging.Params) (List, error) {
	if err := access.Require(ctx, "entitlement.read"); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	owned := map[uuid.UUID]struct{}{}
	switch { //nolint:wsl_v5 // dense by design
	case principal.Role == domain.RoleUser:
		ids, err := s.store.UserEntitlementIDs(ctx, account.ID, principal.BearerID)
		if err != nil {
			return List{}, fmt.Errorf("entitlements: index: %w", err)
		}

		owned = idSet(ids)
	case principal.Role == domain.RoleLicense && principal.License != nil:
		ids, err := s.store.LicenseEntitlementIDs(ctx, account.ID, principal.License.ID)
		if err != nil {
			return List{}, fmt.Errorf("entitlements: index: %w", err)
		}

		owned = idSet(ids)
	}

	page := params.Window()

	items, err := s.store.ListEntitlements(ctx, account.ID, page)
	if err != nil {
		return List{}, fmt.Errorf("entitlements: index: %w", err)
	}

	for _, item := range items {
		if err := access.EntitlementShow(principal, item.ID, owned); err != nil {
			return List{}, err //nolint:wrapcheck // the authorization's own error
		}
	}

	out := List{Items: items, Count: len(items)}

	switch params.Mode { //nolint:exhaustive // the default branch covers the rest
	case paging.ModeOffset:
		count, err := s.store.CountEntitlements(ctx, account.ID)
		if err != nil {
			return List{}, fmt.Errorf("entitlements: index count: %w", err)
		}

		out.Count = count
	case paging.ModeCursor:
		if len(items) > int(page.Limit) {
			out.Items = items[:page.Limit]
			out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].ID, Valid: true}
		}
	}

	return out, nil
}

// Show answers one entitlement by id or code alias.
// Resolve loads the entitlement (the before_action's alias find; a
// miss is the 404 that precedes the typed-params decode and the
// authorization).
func (s Scenario) Resolve(ctx context.Context, account domain.Account, alias string) (domain.Entitlement, error) {
	return s.load(ctx, account.ID, alias)
}

func (s Scenario) Show(ctx context.Context, account domain.Account, alias string) (domain.Entitlement, error) {
	entitlement, err := s.load(ctx, account.ID, alias)
	if err != nil {
		return domain.Entitlement{}, err
	}

	if err := access.Require(ctx, "entitlement.read"); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	owned := map[uuid.UUID]struct{}{}
	switch { //nolint:wsl_v5 // dense by design
	case principal.Role == domain.RoleUser:
		ids, err := s.store.UserEntitlementIDs(ctx, account.ID, principal.BearerID)
		if err != nil {
			return domain.Entitlement{}, fmt.Errorf("entitlements: show: %w", err)
		}

		owned = idSet(ids)
	case principal.Role == domain.RoleLicense && principal.License != nil:
		ids, err := s.store.LicenseEntitlementIDs(ctx, account.ID, principal.License.ID)
		if err != nil {
			return domain.Entitlement{}, fmt.Errorf("entitlements: show: %w", err)
		}

		owned = idSet(ids)
	}

	if err := access.EntitlementShow(principal, entitlement.ID, owned); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	return entitlement, nil
}

// Create writes a new entitlement.
func (s Scenario) Create(ctx context.Context, account domain.Account, in Input) (domain.Entitlement, error) {
	if err := access.Require(ctx, "entitlement.create"); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.EntitlementWrite(access.PrincipalOf(ctx)); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	now := s.clock()

	entitlement := domain.Entitlement{
		ID:        uuid.New(),
		AccountID: account.ID,
		Name:      deref(in.Name),
		Code:      deref(in.Code),
		Metadata:  metadataOf(in),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.validate(ctx, account.ID, entitlement, true); err != nil {
		return domain.Entitlement{}, err
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.InsertEntitlement(ctx, entitlement); err != nil {
			return mapWrite(err)
		}

		s.publish(ctx, "entitlement.created", entitlement)

		return nil
	}); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the transaction's own error
	}

	return entitlement, nil
}

// Update merges the input over the entitlement and writes it.
func (s Scenario) Update(
	ctx context.Context, account domain.Account, alias string, in Input,
) (domain.Entitlement, error) {
	entitlement, err := s.load(ctx, account.ID, alias)
	if err != nil {
		return domain.Entitlement{}, err
	}

	if err := access.Require(ctx, "entitlement.update"); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.EntitlementWrite(access.PrincipalOf(ctx)); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the authorization's own error
	}

	if in.NameSet {
		entitlement.Name = deref(in.Name)
	}

	if in.CodeSet {
		entitlement.Code = deref(in.Code)
	}

	if in.MetadataOn {
		entitlement.Metadata = metadataOf(in)
	}

	entitlement.UpdatedAt = s.clock()

	if err := s.validate(ctx, account.ID, entitlement, false); err != nil {
		return domain.Entitlement{}, err
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateEntitlement(ctx, entitlement); err != nil {
			return mapWrite(err)
		}

		s.publish(ctx, "entitlement.updated", entitlement)

		return nil
	}); err != nil {
		return domain.Entitlement{}, err //nolint:wrapcheck // the transaction's own error
	}

	return entitlement, nil
}

// Destroy removes the entitlement.
func (s Scenario) Destroy(ctx context.Context, account domain.Account, alias string) error {
	entitlement, err := s.load(ctx, account.ID, alias)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "entitlement.delete"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.EntitlementWrite(access.PrincipalOf(ctx)); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if _, err := s.store.DeleteEntitlement(ctx, account.ID, entitlement.ID); err != nil {
			return fmt.Errorf("entitlements: destroy: %w", err)
		}

		s.publish(ctx, "entitlement.deleted", entitlement)

		return nil
	}); err != nil {
		return err //nolint:wrapcheck // the transaction's own error
	}

	return nil
}

// load resolves the id-or-code alias.
func (s Scenario) load(ctx context.Context, accountID uuid.UUID, alias string) (domain.Entitlement, error) {
	if id, err := uuid.Parse(alias); err == nil {
		entitlement, err := s.store.EntitlementByID(ctx, accountID, id)
		if errors.Is(err, ErrNotFound) {
			return domain.Entitlement{}, apperr.NotFound("entitlement", alias)
		}

		if err != nil {
			return domain.Entitlement{}, fmt.Errorf("entitlements: load: %w", err)
		}

		return entitlement, nil
	}

	entitlement, err := s.store.EntitlementByCode(ctx, accountID, alias)
	if errors.Is(err, ErrNotFound) {
		return domain.Entitlement{}, apperr.NotFound("entitlement", alias)
	}

	if err != nil {
		return domain.Entitlement{}, fmt.Errorf("entitlements: load: %w", err)
	}

	return entitlement, nil
}

// validate runs the model validations (Entitlement's rules).
func (s Scenario) validate(
	_ context.Context, _ uuid.UUID, e domain.Entitlement, create bool,
) error { // mirrors the reference's shape
	var errs []*apperr.T

	add := func(code, detail, attr string) {
		errs = append(errs, apperr.Validation(code, detail,
			"/data/attributes/"+attr, "entitlements/#entitlements-object-attrs-"+attr))
	}

	if strings.TrimSpace(e.Name) == "" {
		add("NAME_MISSING", "can't be blank", "name")
	} else if len(e.Name) > 255 { //nolint:mnd // the reference's literal bound
		add("NAME_TOO_LONG", "is too long (maximum is 255 characters)", "name")
	}

	switch {
	case e.Code == "":
		add("CODE_MISSING", "can't be blank", "code")
	case len(e.Code) > 255: //nolint:mnd // the reference's literal bound
		add("CODE_TOO_LONG", "is too long (maximum is 255 characters)", "code")
	case isUUID(e.Code):
		add("CODE_INVALID", "is invalid", "code")
	case reserved(domain.ReservedEntitlementCodes, e.Code):
		add("CODE_NOT_ALLOWED", "is reserved", "code")
	}

	if err := apperr.MultiOf(errs...); err != nil {
		return err //nolint:wrapcheck // the document itself
	}

	_ = create // the unique index answers the taken check on both writes

	return nil
}

// mapWrite renders the write's index violation as the CODE_TAKEN 422.
func mapWrite(err error) error {
	if errors.Is(err, ErrCodeTaken) {
		return apperr.Validation("CODE_TAKEN", "has already been taken",
			"/data/attributes/code", "entitlements/#entitlements-object-attrs-code")
	}

	return fmt.Errorf("entitlements: write: %w", err)
}

func (s Scenario) publish(ctx context.Context, event string, entitlement domain.Entitlement) {
	_ = s.events.Publish(ctx, event, entitlement)
}

func metadataOf(in Input) map[string]any {
	if !in.MetadataOn || in.Metadata == nil {
		return map[string]any{}
	}

	return in.Metadata
}

func idSet(ids []uuid.UUID) map[uuid.UUID]struct{} {
	set := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}

	return set
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func reserved(list []string, value string) bool {
	for _, r := range list {
		if strings.EqualFold(r, value) {
			return true
		}
	}

	return false
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)

	return err == nil
}
