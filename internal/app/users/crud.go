package users

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
)

// emailRe mirrors the users table's email CHECK (and the reference's
// EMAIL_RE).
var emailRe = regexp.MustCompile(`^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$`)

// The field length limits of the reference's user model.
const (
	maxEmailLen    = 255
	maxNameLen     = 255
	minPasswordLen = 6
	maxPasswordLen = 72
)

// Attrs is the decoded write payload of POST/PATCH /users (the handler
// has already enforced the typed-params shape, including who may send
// role/password/metadata/group at all).
type Attrs struct {
	FirstName *string
	LastName  *string
	Email     *string
	// Password is nil when absent (a passwordless user); an empty
	// string clears it.
	Password *string
	Metadata map[string]any
	// Role is the requested role (wire form); empty keeps the current
	// one (or "user" on create).
	Role string
	// GroupSet reports whether the group relationship was addressed;
	// Group carries its value (zero = remove).
	GroupSet bool
	Group    uuid.NullUUID
}

// Create validates and writes a new user, answering the row with her
// computed status.
func (s Scenario) Create(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	attrs Attrs,
) (Listed, error) {
	role, err := requestedRole(domain.RoleUser, attrs.Role)
	if err != nil {
		return Listed{}, err
	}

	if err := access.UserCreate(principal, account.Protected, role); err != nil {
		return Listed{}, err //nolint:wrapcheck // the authorization's own error
	}

	now := s.clock()

	user := domain.User{
		ID:        uuid.New(),
		AccountID: account.ID,
		Role:      role,
		GroupID:   attrs.Group,
		Metadata:  attrs.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	applyAttrs(&user, attrs)

	digest, err := s.passwordDigest(attrs.Password)
	if err != nil {
		return Listed{}, err
	}

	user.PasswordDigest = digest

	if err := s.validateNew(ctx, account.ID, user, attrs.GroupSet); err != nil {
		return Listed{}, err
	}

	if err := s.store.InsertUser(ctx, user); err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return Listed{}, unprocessable("EMAIL_TAKEN", "has already been taken", "attributes/email")
		}

		return Listed{}, fmt.Errorf("users: create: %w", err)
	}

	if err := s.events.Publish(ctx, "user.created", user); err != nil {
		return Listed{}, fmt.Errorf("users: create: %w", err)
	}

	return Listed{User: user, Status: user.Status(false, s.clock())}, nil
}

// Show loads the user by id-or-email alias, enforcing the reference's
// scoped lookup (an invisible user is a 404).
func (s Scenario) Show(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias string,
) (Listed, error) {
	if err := access.Require(ctx, "user.read"); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return Listed{}, err
	}

	status, err := s.status(ctx, user)
	if err != nil {
		return Listed{}, err
	}

	return Listed{User: user, Status: status}, nil
}

// Update applies the write payload to the user.
func (s Scenario) Update(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias string,
	attrs Attrs,
) (Listed, error) {
	if err := access.Require(ctx, "user.update"); err != nil {
		return Listed{}, err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return Listed{}, err
	}

	original := user

	role, err := s.prepareUpdate(ctx, account, principal, &user, attrs)
	if err != nil {
		return Listed{}, err
	}

	applyAttrs(&user, attrs)

	if attrs.GroupSet {
		user.GroupID = attrs.Group
	}

	digest, err := s.passwordDigest(attrs.Password)
	if err != nil {
		return Listed{}, err
	}

	if attrs.Password != nil {
		user.PasswordDigest = digest
	}

	groupChanged := attrs.GroupSet && user.GroupID != original.GroupID
	roleChanged := role != original.Role

	if err := s.validateUpdate(ctx, account.ID, user, original, groupChanged, role); err != nil {
		return Listed{}, err
	}

	if err := s.store.UpdateUser(ctx, user); err != nil {
		if errors.Is(err, ErrEmailTaken) {
			return Listed{}, unprocessable("EMAIL_TAKEN", "has already been taken", "attributes/email")
		}

		return Listed{}, fmt.Errorf("users: update: %w", err)
	}

	// A role change revokes the user's tokens (all but the acting one).
	if roleChanged {
		if err := s.revokeTokens(ctx, account.ID, user.ID, principal); err != nil {
			return Listed{}, err
		}
	}

	if err := s.events.Publish(ctx, "user.updated", user); err != nil {
		return Listed{}, fmt.Errorf("users: update: %w", err)
	}

	user.Role = role

	status, err := s.status(ctx, user)
	if err != nil {
		return Listed{}, err
	}

	return Listed{User: user, Status: status}, nil
}

// Delete removes the user (the last admin is undeletable).
func (s Scenario) Delete(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	alias string,
) error {
	if err := access.Require(ctx, "user.delete"); err != nil {
		return err //nolint:wrapcheck // the port's own error
	}

	user, err := s.visible(ctx, account, principal, alias)
	if err != nil {
		return err
	}

	if err := access.UserDestroy(principal, user); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if user.Role == domain.RoleAdmin {
		admins, err := s.store.CountAdmins(ctx, account.ID)
		if err != nil {
			return err //nolint:wrapcheck // the port's own error
		}

		if admins <= 1 {
			return unprocessable(
				"ACCOUNT_ADMINS_REQUIRED",
				"account must have at least 1 admin user",
				"relationships/account",
			)
		}
	}

	if err := s.store.DeleteUser(ctx, account.ID, user.ID); err != nil {
		return fmt.Errorf("users: delete: %w", err)
	}

	if err := s.events.Publish(ctx, "user.deleted", user); err != nil {
		return fmt.Errorf("users: delete: %w", err)
	}

	return nil
}

// List answers the filtered, paginated page of the principal's scope.
func (s Scenario) List(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	f Filter,
	p paging.Params,
) (Page, error) {
	if err := access.Require(ctx, "user.read"); err != nil {
		return Page{}, err //nolint:wrapcheck // the port's own error
	}

	scope := access.UserScopeKind(principal)

	rows, err := s.store.ListUsers(ctx, account.ID, f, scope, principal.BearerID, p)
	if err != nil {
		return Page{}, fmt.Errorf("users: list: %w", err)
	}

	total := len(rows)
	if p.Mode == paging.ModeOffset {
		total, err = s.store.CountUsers(ctx, account.ID, f, scope, principal.BearerID)
		if err != nil {
			return Page{}, fmt.Errorf("users: count: %w", err)
		}
	}

	return Page{Rows: rows, Total: total}, nil
}

// visible resolves the id-or-email alias within the principal's
// authorized scope (the reference's set_user).
func (s Scenario) visible(
	ctx context.Context, account domain.Account, principal access.Principal, alias string,
) (domain.User, error) {
	user, err := s.byAlias(ctx, account.ID, alias)
	if errors.Is(err, ErrNotFound) {
		return domain.User{}, apperr.NotFound("user", alias)
	}

	if err != nil {
		return domain.User{}, err
	}

	if !s.inScope(ctx, account, principal, user) {
		return domain.User{}, apperr.NotFound("user", alias)
	}

	return user, nil
}

// byAlias resolves a UUID or email alias.
func (s Scenario) byAlias(ctx context.Context, accountID uuid.UUID, alias string) (domain.User, error) {
	if id, err := uuid.Parse(strings.ToLower(alias)); err == nil {
		return s.store.UserByID(ctx, accountID, id) //nolint:wrapcheck // the caller maps the sentinel
	}

	return s.store.UserByEmail(ctx, accountID, strings.ToLower(alias)) //nolint:wrapcheck // ditto
}

// inScope answers the authorized_scope visibility of the record
// (User#accessible_by).
func (s Scenario) inScope(
	ctx context.Context, account domain.Account, principal access.Principal, user domain.User,
) bool {
	switch access.UserScopeKind(principal) {
	case "":
		return true
	case "product":
		return user.Role == domain.RoleUser
	case "user":
		if user.ID == principal.BearerID {
			return true
		}

		teammate, err := s.store.UserIsTeammate(ctx, account.ID, principal.BearerID, user.ID)

		return err == nil && teammate
	default:
		return false
	}
}

// prepareUpdate resolves the target role and authorizes the mutation
// (the caller keeps the original record for the change checks).
func (s Scenario) prepareUpdate(
	ctx context.Context,
	account domain.Account,
	principal access.Principal,
	user *domain.User,
	attrs Attrs,
) (domain.Role, error) {
	_ = ctx
	_ = account

	role, err := requestedRole(user.Role, attrs.Role)
	if err != nil {
		return role, err
	}

	if err := access.UserUpdate(principal, *user, role); err != nil {
		return role, err //nolint:wrapcheck // the authorization's own error
	}

	return role, nil
}

// requestedRole parses the wire role (empty keeps the current one).
func requestedRole(current domain.Role, wire string) (domain.Role, error) {
	if wire == "" {
		return current, nil
	}

	role, ok := domain.ParseRole(wire)
	if !ok {
		return role, apperr.BadRequestSource("is invalid", "/data/attributes/role")
	}

	return role, nil
}

// applyAttrs copies the simple attributes onto the record.
func applyAttrs(user *domain.User, attrs Attrs) {
	if attrs.FirstName != nil {
		user.FirstName = *attrs.FirstName
	}

	if attrs.LastName != nil {
		user.LastName = *attrs.LastName
	}

	if attrs.Email != nil {
		user.Email = strings.ToLower(strings.TrimSpace(*attrs.Email))
	}

	if attrs.Metadata != nil {
		user.Metadata = attrs.Metadata
	}
}

// passwordDigest validates the password length and answers its bcrypt
// digest (nil password → passwordless, empty string → cleared).
func (s Scenario) passwordDigest(password *string) (string, error) {
	if password == nil {
		return "", nil
	}

	n := utf8.RuneCountInString(*password)
	switch {
	case n < minPasswordLen:
		return "", unprocessable(
			"PASSWORD_TOO_SHORT",
			"is too short (minimum is 6 characters)",
			"attributes/password",
		)
	case n > maxPasswordLen:
		return "", unprocessable(
			"PASSWORD_TOO_LONG",
			"is too long (maximum is 72 characters)",
			"attributes/password",
		)
	}

	digest, err := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("users: password: %w", err)
	}

	return string(digest), nil
}

// validateNew runs the model validations of a create (group existence
// and limits included).
func (s Scenario) validateNew(ctx context.Context, accountID uuid.UUID, user domain.User, groupSet bool) error {
	if err := validateUser(user); err != nil {
		return err
	}

	if groupSet && user.GroupID.Valid {
		return s.checkGroupJoin(ctx, accountID, user, domain.User{})
	}

	return nil
}

// validateUpdate runs the model validations of an update, including the
// last-admin guard on demotion.
func (s Scenario) validateUpdate(
	ctx context.Context, accountID uuid.UUID, user, original domain.User,
	groupChanged bool, role domain.Role,
) error {
	if err := validateUser(user); err != nil {
		return err
	}

	if original.Role == domain.RoleAdmin && role != domain.RoleAdmin {
		admins, err := s.store.CountAdmins(ctx, accountID)
		if err != nil {
			return err //nolint:wrapcheck // the port's own error
		}

		if admins <= 1 {
			return unprocessable(
				"ACCOUNT_ADMINS_REQUIRED",
				"account must have at least 1 admin user",
				"relationships/account",
			)
		}
	}

	if groupChanged && user.GroupID.Valid {
		return s.checkGroupJoin(ctx, accountID, user, original)
	}

	return nil
}

// validateUser checks the row-level field validations, in the model's
// attribute order.
func validateUser(user domain.User) error {
	switch {
	case len(user.FirstName) > maxNameLen:
		return unprocessable("FIRST_NAME_TOO_LONG", "is too long (maximum is 255 characters)", "attributes/firstName")
	case len(user.LastName) > maxNameLen:
		return unprocessable("LAST_NAME_TOO_LONG", "is too long (maximum is 255 characters)", "attributes/lastName")
	case user.Email == "":
		return unprocessable("EMAIL_MISSING", "can't be blank", "attributes/email")
	case len(user.Email) > maxEmailLen || !emailRe.MatchString(user.Email):
		return unprocessable("EMAIL_INVALID", "is invalid", "attributes/email")
	}

	if detail := domain.MetadataViolation(user.Metadata); detail != "" {
		return unprocessable("METADATA_TOO_LONG", detail, "attributes/metadata")
	}

	return nil
}

// checkGroupJoin verifies the target group exists and has room.
func (s Scenario) checkGroupJoin(
	ctx context.Context, accountID uuid.UUID, user, original domain.User,
) error {
	group, err := s.store.GroupByID(ctx, accountID, user.GroupID.UUID)
	if errors.Is(err, ErrNotFound) {
		return unprocessable("GROUP_NOT_FOUND", "must exist", "relationships/group")
	}

	if err != nil {
		return fmt.Errorf("users: group: %w", err)
	}

	if group.MaxUsers != nil {
		count, err := s.store.CountGroupUsers(ctx, accountID, group.ID)
		if err != nil {
			return err //nolint:wrapcheck // the port's own error
		}

		// Staying in the same group does not add a member.
		if original.GroupID.Valid && original.GroupID.UUID == group.ID {
			count--
		}

		if count >= *group.MaxUsers {
			return unprocessable(
				"GROUP_USER_LIMIT_EXCEEDED",
				fmt.Sprintf("user count has exceeded maximum allowed by current group (%d)", *group.MaxUsers),
				"relationships/group",
			)
		}
	}

	return nil
}

// revokeTokens revokes the user's tokens, keeping the acting one when
// the request is token-authenticated.
func (s Scenario) revokeTokens(
	ctx context.Context, accountID, userID uuid.UUID, principal access.Principal,
) error {
	if principal.TokenID != uuid.Nil {
		err := s.store.DeleteUserTokensExcept(ctx, accountID, userID, principal.TokenID)

		return err //nolint:wrapcheck // the port's own error
	}

	return s.store.DeleteUserTokens(ctx, accountID, userID) //nolint:wrapcheck // the port's own error
}

// docsOrigin is the reference's hardcoded docs origin of validation
// error links (they never follow the request host).
const docsOrigin = "https://keygen.sh/docs/api"

// unprocessable builds a 422 with the validation docs link (the pointer
// is relative to /data).
func unprocessable(code, detail, attrPath string) *apperr.T {
	t := apperr.Unprocessable(code, detail, "/data/"+attrPath)

	attr := strings.TrimPrefix(strings.TrimPrefix(attrPath, "attributes/"), "relationships/")
	if strings.HasPrefix(attrPath, "relationships/") {
		t.About = docsOrigin + "/users/#users-object-relationships-" + attr

		return t
	}

	t.About = docsOrigin + "/users/#users-object-attrs-" + strings.ReplaceAll(attr, "/", "-")

	return t
}
