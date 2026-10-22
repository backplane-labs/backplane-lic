// Package products is the product scenario: CRUD, the product's
// relationship routes (policies, licenses, machines, tokens, users)
// and product-token generation.
package products

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/backplane-labs/backplane-lic/internal/app/access"
	"github.com/backplane-labs/backplane-lic/internal/app/apperr"
	"github.com/backplane-labs/backplane-lic/internal/app/events"
	"github.com/backplane-labs/backplane-lic/internal/app/paging"
	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/domain/crypto"
)

// ErrNotFound is the storage port's not-found sentinel.
var ErrNotFound = domain.ErrNotFound // one sentinel across the scenario ports

// ErrCodeTaken is the unique-index violation of (account, lower(code)).
var ErrCodeTaken = errors.New("products: code taken")

// Store is the scenario's consumer-side storage port.
type Store interface { //nolint:interfacebloat // the scenario's full storage port
	// Do runs fn in one transaction; the writes and the webhook
	// events of one action commit together.
	Do(ctx context.Context, fn func(ctx context.Context) error) error
	// ProductByID loads a product; ErrNotFound when none.
	ProductByID(ctx context.Context, accountID, id uuid.UUID) (domain.Product, error)
	// ProductByCode loads a product by downcased code.
	ProductByCode(ctx context.Context, accountID uuid.UUID, code string) (domain.Product, error)
	// InsertProduct writes a new row; ErrCodeTaken on a collision.
	InsertProduct(ctx context.Context, product domain.Product) error
	// UpdateProduct overwrites the mutable columns.
	UpdateProduct(ctx context.Context, product domain.Product) error
	// DeleteProduct destroys the row; false when gone.
	DeleteProduct(ctx context.Context, accountID, id uuid.UUID) (bool, error)
	// ListAllProducts pages the admin-side scope.
	ListAllProducts(ctx context.Context, accountID uuid.UUID, page paging.Page) ([]domain.Product, error)
	// CountAllProducts counts the admin-side scope.
	CountAllProducts(ctx context.Context, accountID uuid.UUID) (int, error)
	// ListUserProducts pages the user scope.
	ListUserProducts(ctx context.Context, accountID, userID uuid.UUID, page paging.Page) ([]domain.Product, error)
	// CountUserProducts counts the user scope.
	CountUserProducts(ctx context.Context, accountID, userID uuid.UUID) (int, error)
	// ListLicenseProducts pages the license scope.
	ListLicenseProducts(ctx context.Context, accountID, productID uuid.UUID, page paging.Page) ([]domain.Product, error)
	// CountLicenseProducts counts the license scope.
	CountLicenseProducts(ctx context.Context, accountID, productID uuid.UUID) (int, error)
	// ListOwnProducts pages the product-bearer scope.
	ListOwnProducts(ctx context.Context, accountID, bearerID uuid.UUID, page paging.Page) ([]domain.Product, error)
	// CountOwnProducts counts the product-bearer scope.
	CountOwnProducts(ctx context.Context, accountID, bearerID uuid.UUID) (int, error)
	// UserProductIDs answers the user's licensed products.
	UserProductIDs(ctx context.Context, accountID, userID uuid.UUID) ([]uuid.UUID, error)
	// GetLicenseProductID answers the license bearer's product id.
	GetLicenseProductID(ctx context.Context, accountID, licenseID uuid.UUID) (uuid.UUID, error)
	// ListProductTokens pages the product's tokens.
	ListProductTokens(ctx context.Context, accountID, productID uuid.UUID, page paging.Page) ([]domain.Token, error)
	// CountProductTokens counts the product's tokens.
	CountProductTokens(ctx context.Context, accountID, productID uuid.UUID) (int, error)
	// GetProductToken loads one of the product's tokens.
	GetProductToken(ctx context.Context, accountID, productID, tokenID uuid.UUID) (domain.Token, error)
	// ListProductUsers pages the product's users (via its licenses).
	ListProductUsers(ctx context.Context, accountID, productID uuid.UUID, roles []string, page paging.Page) ([]domain.User, error) //nolint:lll // the wire name is long
	// CountProductUsers counts the product's users.
	CountProductUsers(ctx context.Context, accountID, productID uuid.UUID, roles []string) (int, error)
	// GetProductUserByID loads one of the product's users by id.
	GetProductUserByID(ctx context.Context, accountID, productID uuid.UUID, roles []string, id uuid.UUID) (domain.User, error) //nolint:lll // the wire name is long
	// GetProductUserByEmail loads one of the product's users by email.
	GetProductUserByEmail(ctx context.Context, accountID, productID uuid.UUID, roles []string, email string) (domain.User, error) //nolint:lll // the wire name is long
}

// TokenStore is the token write seam (the tokens repository).
type TokenStore interface {
	// InsertToken writes a product-token row.
	InsertToken(ctx context.Context, token domain.Token) error
}

// UserStore is the user seam (the users repository).
type UserStore interface {
	// UserHasActiveLicense answers the user's status input.
	UserHasActiveLicense(ctx context.Context, accountID, id uuid.UUID) (bool, error)
}

// Input is the request body of create and update (decoded and
// shape-validated by the handler); the On flags distinguish an absent
// attribute from an explicit null.
type Input struct {
	NameOn         bool
	Name           *string
	CodeOn         bool
	Code           *string
	DistributionOn bool
	Distribution   *string
	URLOn          bool
	URL            *string
	PlatformsOn    bool
	Platforms      []string
	MetadataOn     bool
	Metadata       map[string]any
}

// TokenInput is the body of POST /products/{id}/tokens.
type TokenInput struct {
	Name   *string
	Expiry *time.Time
}

// Scenario runs the product reads and writes.
type Scenario struct {
	store    Store
	tokens   TokenStore
	users    UserStore
	events   events.Publisher
	resolver domain.Resolver
	allowed  []*net.IPNet
	rng      io.Reader
	clock    func() time.Time
}

// New builds the scenario over the products, tokens and users
// repositories.
func New(
	store Store, tokens TokenStore, users UserStore, publisher events.Publisher,
	resolver domain.Resolver, allowedPrivate []*net.IPNet,
	rng io.Reader, clock func() time.Time,
) Scenario {
	return Scenario{
		store: store, tokens: tokens, users: users, events: publisher,
		resolver: resolver, allowed: allowedPrivate, rng: rng, clock: clock,
	}
}

// List is one index page.
type List struct {
	Items []domain.Product
	Count int
	Next  uuid.NullUUID
}

// Index answers the products page within the principal's scope.
func (s Scenario) Index(ctx context.Context, account domain.Account, params paging.Params) (List, error) {
	if err := access.Require(ctx, "product.read"); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)
	page := params.Window()

	var (
		items []domain.Product
		err   error
	)

	switch principal.Role {
	case domain.RoleUser:
		items, err = s.store.ListUserProducts(ctx, account.ID, principal.BearerID, page)
	case domain.RoleLicense:
		productID, lidErr := s.licenseProductID(ctx, account.ID, principal)
		if lidErr != nil {
			return List{}, lidErr
		}

		items, err = s.store.ListLicenseProducts(ctx, account.ID, productID, page)
	case domain.RoleProduct:
		items, err = s.store.ListOwnProducts(ctx, account.ID, principal.BearerID, page)
	default:
		items, err = s.store.ListAllProducts(ctx, account.ID, page)
	}

	if err != nil {
		return List{}, fmt.Errorf("products: index: %w", err)
	}

	// The user scope may include a CLOSED product through an owned
	// license, but the policy still denies a page containing that product.
	if err := access.ProductIndex(principal, allOpenOrLicensed(items)); err != nil {
		return List{}, err //nolint:wrapcheck // the authorization's own error
	}

	out := List{Items: items, Count: len(items)}

	switch params.Mode { //nolint:exhaustive // the default branch covers the rest
	case paging.ModeOffset:
		count, err := s.countScope(ctx, account.ID, principal)
		if err != nil {
			return List{}, err
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

func allOpenOrLicensed(items []domain.Product) bool {
	for i := range items {
		if !items[i].Open() && !items[i].Licensed() {
			return false
		}
	}

	return true
}

// Show answers one product by id or code alias, within scope.
func (s Scenario) Show(ctx context.Context, account domain.Account, alias string) (domain.Product, error) {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return domain.Product{}, err
	}

	if err := access.Require(ctx, "product.read"); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	principal := access.PrincipalOf(ctx)

	licensed := false

	switch principal.Role { //nolint:exhaustive // the default branch covers the rest
	case domain.RoleUser:
		ids, err := s.store.UserProductIDs(ctx, account.ID, principal.BearerID)
		if err != nil {
			return domain.Product{}, fmt.Errorf("products: show: %w", err)
		}

		licensed = contains(ids, product.ID)
	case domain.RoleLicense:
		productID, err := s.licenseProductID(ctx, account.ID, principal)
		if err != nil {
			return domain.Product{}, err
		}

		licensed = productID == product.ID
	}

	if err := access.ProductShow(principal, product, licensed); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	return product, nil
}

// Create writes a new product.
func (s Scenario) Create(ctx context.Context, account domain.Account, in Input) (domain.Product, error) {
	if err := access.Require(ctx, "product.create"); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductWrite(access.PrincipalOf(ctx), domain.Product{}); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	now := s.clock()

	product := domain.Product{
		ID:                   uuid.New(),
		AccountID:            account.ID,
		Name:                 deref(in.Name),
		Code:                 deref(in.Code),
		DistributionStrategy: domain.DistributionLicensed,
		URL:                  deref(in.URL),
		Platforms:            in.Platforms,
		Metadata:             metadataOf(in),
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if in.DistributionOn && in.Distribution != nil {
		product.DistributionStrategy = *in.Distribution
	}

	if err := s.validate(product); err != nil {
		return domain.Product{}, err
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.InsertProduct(ctx, product); err != nil {
			return mapWrite(err)
		}

		s.publish(ctx, "product.created", product)

		return nil
	}); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the transaction's own error
	}

	return product, nil
}

// Resolve loads the product within the principal's scope (the
// before_action's scoped find; a miss is the 404 that precedes the
// typed-params decode and the authorization).
func (s Scenario) Resolve(ctx context.Context, account domain.Account, alias string) (domain.Product, error) {
	return s.loadScoped(ctx, account, alias)
}

// Update merges the input over the product and writes it.
func (s Scenario) Update(
	ctx context.Context, account domain.Account, alias string, in Input,
) (domain.Product, error) {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return domain.Product{}, err
	}

	if err := access.Require(ctx, "product.update"); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductWrite(access.PrincipalOf(ctx), product); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	if in.NameOn {
		product.Name = deref(in.Name)
	}

	if in.CodeOn {
		product.Code = deref(in.Code)
	}

	if in.DistributionOn {
		product.DistributionStrategy = deref(in.Distribution)
	}

	if in.URLOn {
		product.URL = deref(in.URL)
	}

	if in.PlatformsOn {
		product.Platforms = in.Platforms
	}

	if in.MetadataOn {
		product.Metadata = metadataOf(in)
	}

	if product.DistributionStrategy == "" {
		product.DistributionStrategy = domain.DistributionLicensed
	}

	product.UpdatedAt = s.clock()

	if err := s.validate(product); err != nil {
		return domain.Product{}, err
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.store.UpdateProduct(ctx, product); err != nil {
			return mapWrite(err)
		}

		s.publish(ctx, "product.updated", product)

		return nil
	}); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the transaction's own error
	}

	return product, nil
}

// Destroy removes the product.
func (s Scenario) Destroy(ctx context.Context, account domain.Account, alias string) error {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return err
	}

	if err := access.Require(ctx, "product.delete"); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductWrite(access.PrincipalOf(ctx), product); err != nil {
		return err //nolint:wrapcheck // the authorization's own error
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if _, err := s.store.DeleteProduct(ctx, account.ID, product.ID); err != nil {
			return fmt.Errorf("products: destroy: %w", err)
		}

		s.publish(ctx, "product.deleted", product)

		return nil
	}); err != nil {
		return err //nolint:wrapcheck // the transaction's own error
	}

	return nil
}

// GenerateProductToken mints a product token (200, raw secret once).
func (s Scenario) GenerateProductToken(
	ctx context.Context, account domain.Account, alias string, in TokenInput,
) (domain.Token, string, error) {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return domain.Token{}, "", err
	}

	if err := access.Require(ctx, "product.tokens.generate"); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductTokenCreate(access.PrincipalOf(ctx)); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the authorization's own error
	}

	raw, err := crypto.GenerateTokenSecret(s.rng, string(domain.RoleProduct))
	if err != nil {
		return domain.Token{}, "", fmt.Errorf("products: token secret: %w", err)
	}

	now := s.clock()

	token := domain.Token{
		ID:          uuid.New(),
		AccountID:   account.ID,
		Digest:      crypto.DigestV3(account.Keys.SecretKey, raw),
		BearerID:    product.ID,
		BearerType:  "Product",
		BearerRole:  domain.RoleProduct,
		Kind:        crypto.TokenKind(string(domain.RoleProduct)),
		Name:        in.Name,
		Expiry:      in.Expiry,
		Permissions: []string{"*"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.store.Do(ctx, func(ctx context.Context) error {
		if err := s.tokens.InsertToken(ctx, token); err != nil {
			return fmt.Errorf("products: token insert: %w", err)
		}

		_ = s.events.Publish(ctx, "token.generated", token)

		return nil
	}); err != nil {
		return domain.Token{}, "", err //nolint:wrapcheck // the transaction's own error
	}

	return token, raw, nil
}

// loadScoped resolves the id-or-code alias within the principal's
// product scope (a miss is the 404, like the reference's scoped find).
func (s Scenario) loadScoped( //nolint:gocognit,cyclop // one branch per bearer kind
	ctx context.Context, account domain.Account, alias string,
) (domain.Product, error) {
	principal := access.PrincipalOf(ctx)

	load := func() (domain.Product, error) {
		if id, err := uuid.Parse(alias); err == nil {
			product, err := s.store.ProductByID(ctx, account.ID, id)
			if errors.Is(err, ErrNotFound) {
				return domain.Product{}, apperr.NotFound("product", alias)
			}

			if err != nil {
				return domain.Product{}, fmt.Errorf("products: load: %w", err)
			}

			return product, nil
		}

		product, err := s.store.ProductByCode(ctx, account.ID, alias)
		if errors.Is(err, ErrNotFound) {
			return domain.Product{}, apperr.NotFound("product", alias)
		}

		if err != nil {
			return domain.Product{}, fmt.Errorf("products: load: %w", err)
		}

		return product, nil
	}

	product, err := load()
	if err != nil {
		return domain.Product{}, err
	}

	// The scoped load: anything outside the principal's product scope
	// answers the same 404 the reference's scoped find does.
	switch principal.Role { //nolint:exhaustive // the default branch covers the rest
	case domain.RoleProduct:
		if product.ID != principal.BearerID {
			return domain.Product{}, apperr.NotFound("product", alias)
		}
	case domain.RoleUser:
		if !product.Open() {
			ids, err := s.store.UserProductIDs(ctx, account.ID, principal.BearerID)
			if err != nil {
				return domain.Product{}, fmt.Errorf("products: scope: %w", err)
			}

			if !contains(ids, product.ID) {
				return domain.Product{}, apperr.NotFound("product", alias)
			}
		}
	case domain.RoleLicense:
		if !product.Open() {
			productID, err := s.licenseProductID(ctx, account.ID, principal)
			if err != nil {
				return domain.Product{}, err
			}

			if productID != product.ID {
				return domain.Product{}, apperr.NotFound("product", alias)
			}
		}
	}

	return product, nil
}

func (s Scenario) licenseProductID(ctx context.Context, accountID uuid.UUID, principal access.Principal) (uuid.UUID, error) { //nolint:lll // the wire name is long
	if principal.License == nil {
		return uuid.UUID{}, apperr.NotFound("product", "")
	}

	productID, err := s.store.GetLicenseProductID(ctx, accountID, principal.License.ID)
	if err != nil {
		return uuid.UUID{}, fmt.Errorf("products: license product: %w", err)
	}

	return productID, nil
}

func (s Scenario) countScope(ctx context.Context, accountID uuid.UUID, principal access.Principal) (int, error) {
	var (
		count int
		err   error
	)

	switch principal.Role {
	case domain.RoleUser:
		count, err = s.store.CountUserProducts(ctx, accountID, principal.BearerID)
	case domain.RoleLicense:
		var productID uuid.UUID

		productID, err = s.licenseProductID(ctx, accountID, principal)
		if err != nil {
			return 0, err
		}

		count, err = s.store.CountLicenseProducts(ctx, accountID, productID)
	case domain.RoleProduct:
		count, err = s.store.CountOwnProducts(ctx, accountID, principal.BearerID)
	default:
		count, err = s.store.CountAllProducts(ctx, accountID)
	}

	if err != nil {
		return 0, fmt.Errorf("products: count: %w", err)
	}

	return count, nil
}

// validate runs the model validations (Product's rules).
func (s Scenario) validate(product domain.Product) error {
	var errs []*apperr.T

	attr := func(code, detail, name string) {
		errs = append(errs, apperr.Validation(code, detail,
			"/data/attributes/"+name, "products/#products-object-attrs-"+name))
	}

	if strings.TrimSpace(product.Name) == "" {
		attr("NAME_MISSING", "can't be blank", "name")
	} else if len(product.Name) > 255 { //nolint:mnd // the reference's literal bound
		attr("NAME_TOO_LONG", "is too long (maximum is 255 characters)", "name")
	}

	if detail := domain.MetadataViolation(product.Metadata); detail != "" {
		attr("METADATA_TOO_LONG", detail, "metadata")
	}

	switch {
	case product.Code == "":
		// null is fine
	case len(product.Code) > 255: //nolint:mnd // the reference's literal bound
		attr("CODE_TOO_LONG", "is too long (maximum is 255 characters)", "code")
	case isUUID(product.Code):
		attr("CODE_INVALID", "is invalid", "code")
	case reservedCode(product.Code):
		attr("CODE_NOT_ALLOWED", "is reserved", "code")
	}

	if product.DistributionStrategy != "" && !in(domain.ProductDistributionStrategies(), product.DistributionStrategy) {
		attr("DISTRIBUTION_STRATEGY_NOT_ALLOWED", "unsupported distribution strategy", "distributionStrategy")
	}

	if product.URL != "" {
		for _, violation := range domain.ValidateURL(product.URL, productURLProtocols, s.resolver, s.allowed) {
			attr(violation.Code, violation.Detail, "url")
		}
	}

	if err := apperr.MultiOf(errs...); err != nil {
		return err //nolint:wrapcheck // the document itself
	}

	return nil
}

// productURLProtocols is the product's protocol set (the endpoint's
// is https only).
var productURLProtocols = []string{"https", "http"} //nolint:gochecknoglobals // typed-params data

// mapWrite renders the write's index violation as the CODE_TAKEN 422.
func mapWrite(err error) error {
	if errors.Is(err, ErrCodeTaken) {
		return apperr.Validation("CODE_TAKEN", "has already been taken",
			"/data/attributes/code", "products/#products-object-attrs-code")
	}

	return fmt.Errorf("products: write: %w", err)
}

func (s Scenario) publish(ctx context.Context, event string, product domain.Product) {
	_ = s.events.Publish(ctx, event, product)
}

func metadataOf(in Input) map[string]any {
	if !in.MetadataOn || in.Metadata == nil {
		return map[string]any{}
	}

	return in.Metadata
}

func contains(ids []uuid.UUID, id uuid.UUID) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}

	return false
}

func in(list []string, value string) bool {
	for _, candidate := range list {
		if candidate == value {
			return true
		}
	}

	return false
}

func reservedCode(code string) bool {
	for _, reserved := range domain.ReservedProductCodes {
		if strings.EqualFold(reserved, code) {
			return true
		}
	}

	return false
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)

	return err == nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

// Relationship loads the scoped product and runs the relationship
// guard (the products/*_policy.rb checks) for its sub-resources.
func (s Scenario) Relationship(
	ctx context.Context, account domain.Account, alias, permission string,
) (domain.Product, error) {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return domain.Product{}, err
	}

	if err := access.Require(ctx, permission); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductRelationship(access.PrincipalOf(ctx), product); err != nil {
		return domain.Product{}, err //nolint:wrapcheck // the authorization's own error
	}

	return product, nil
}

// Tokens answers the product's tokens page.
func (s Scenario) Tokens(
	ctx context.Context, account domain.Account, alias string, params paging.Params,
) (TokenList, error) {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return TokenList{}, err
	}

	if err := access.Require(ctx, "token.read"); err != nil {
		return TokenList{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductTokenIndex(access.PrincipalOf(ctx), product); err != nil {
		return TokenList{}, err //nolint:wrapcheck // the authorization's own error
	}

	return s.tokenPage(ctx, account.ID, product.ID, params)
}

// Token shows one of the product's tokens.
func (s Scenario) Token(ctx context.Context, account domain.Account, alias string, tokenID uuid.UUID) (domain.Token, error) { //nolint:lll // the wire name is long
	if err := access.Require(ctx, "token.read"); err != nil {
		return domain.Token{}, err //nolint:wrapcheck // the authorization's own error
	}

	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return domain.Token{}, err
	}

	token, err := s.store.GetProductToken(ctx, account.ID, product.ID, tokenID)
	if errors.Is(err, ErrNotFound) {
		return domain.Token{}, apperr.NotFound("token", tokenID.String())
	}

	if err != nil {
		return domain.Token{}, fmt.Errorf("products: token: %w", err)
	}

	if err := access.ProductTokenShow(access.PrincipalOf(ctx), product, token.BearerID); err != nil {
		return domain.Token{}, err //nolint:wrapcheck // the authorization's own error
	}

	return token, nil
}

// TokenList is one product-tokens page.
type TokenList struct {
	Items []domain.Token
	Count int
	Next  uuid.NullUUID
}

func (s Scenario) tokenPage(
	ctx context.Context, accountID, productID uuid.UUID, params paging.Params,
) (TokenList, error) {
	page := params.Window()

	items, err := s.store.ListProductTokens(ctx, accountID, productID, page)
	if err != nil {
		return TokenList{}, fmt.Errorf("products: tokens: %w", err)
	}

	out := TokenList{Items: items, Count: len(items)}

	switch params.Mode { //nolint:exhaustive // the default branch covers the rest
	case paging.ModeOffset:
		count, err := s.store.CountProductTokens(ctx, accountID, productID)
		if err != nil {
			return TokenList{}, fmt.Errorf("products: tokens count: %w", err)
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

// Users answers the product's users page (roles defaults to user).
func (s Scenario) Users(
	ctx context.Context, account domain.Account, alias string, roles []string, params paging.Params,
) (UserList, error) {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return UserList{}, err
	}

	if err := access.Require(ctx, "user.read"); err != nil {
		return UserList{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductRelationship(access.PrincipalOf(ctx), product); err != nil {
		return UserList{}, err //nolint:wrapcheck // the authorization's own error
	}

	page := params.Window()

	items, err := s.store.ListProductUsers(ctx, account.ID, product.ID, roles, page)
	if err != nil {
		return UserList{}, fmt.Errorf("products: users: %w", err)
	}

	out := UserList{Items: []UserEntry{}, Count: len(items)}

	for _, user := range items { //nolint:gocritic // mirrors the reference's shape
		active, err := s.users.UserHasActiveLicense(ctx, account.ID, user.ID)
		if err != nil {
			return UserList{}, fmt.Errorf("products: users status: %w", err)
		}

		out.Items = append(out.Items, UserEntry{User: user, Status: user.Status(active, s.clock())})
	}

	switch params.Mode { //nolint:exhaustive // the default branch covers the rest
	case paging.ModeOffset:
		count, err := s.store.CountProductUsers(ctx, account.ID, product.ID, roles)
		if err != nil {
			return UserList{}, fmt.Errorf("products: users count: %w", err)
		}

		out.Count = count
	case paging.ModeCursor:
		if len(items) > int(page.Limit) {
			out.Items = out.Items[:page.Limit]
			out.Next = uuid.NullUUID{UUID: out.Items[len(out.Items)-1].User.ID, Valid: true}
		}
	}

	return out, nil
}

// User shows one of the product's users (by id or email alias).
func (s Scenario) User(
	ctx context.Context, account domain.Account, alias, userAlias string, roles []string,
) (UserEntry, error) {
	product, err := s.loadScoped(ctx, account, alias)
	if err != nil {
		return UserEntry{}, err
	}

	if err := access.Require(ctx, "user.read"); err != nil {
		return UserEntry{}, err //nolint:wrapcheck // the authorization's own error
	}

	if err := access.ProductRelationship(access.PrincipalOf(ctx), product); err != nil {
		return UserEntry{}, err //nolint:wrapcheck // the authorization's own error
	}

	var user domain.User

	if id, err := uuid.Parse(userAlias); err == nil {
		user, err = s.store.GetProductUserByID(ctx, account.ID, product.ID, roles, id)
		if err != nil {
			return UserEntry{}, userLoad(err, userAlias)
		}
	} else {
		user, err = s.store.GetProductUserByEmail(ctx, account.ID, product.ID, roles, userAlias)
		if err != nil {
			return UserEntry{}, userLoad(err, userAlias)
		}
	}

	active, err := s.users.UserHasActiveLicense(ctx, account.ID, user.ID)
	if err != nil {
		return UserEntry{}, fmt.Errorf("products: user status: %w", err)
	}

	return UserEntry{User: user, Status: user.Status(active, s.clock())}, nil
}

func userLoad(err error, alias string) error {
	if errors.Is(err, ErrNotFound) {
		return apperr.NotFound("user", alias)
	}

	return fmt.Errorf("products: user: %w", err)
}

// UserEntry pairs a user with her computed status (the wire renders
// it).
type UserEntry struct {
	User   domain.User
	Status string
}

// UserList is one product-users page.
type UserList struct {
	Items []UserEntry
	Count int
	Next  uuid.NullUUID
}
