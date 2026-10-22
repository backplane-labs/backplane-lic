// Package console bridges the internal operator API to the real HTTP API.
// It never makes a network request and never creates an admin credential.
package console

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/backplane-labs/backplane-lic/internal/domain"
	"github.com/backplane-labs/backplane-lic/internal/infra/httpapi"
	licconsolev1 "github.com/backplane-labs/backplane-lic/proto/lic/console/v1"
)

const maxBodyBytes = 1 << 20

// Store is the console's account-scoped read port.
type Store interface {
	Account(ctx context.Context, id uuid.UUID) (domain.Account, error)
	ConsoleCounts(ctx context.Context, id uuid.UUID) (map[string]int64, error)
}

// Server is registered only on Backplane's internal port. The relay must
// carry an operator session even if the caller knows the internal secret.
type Server struct {
	licconsolev1.UnimplementedConsoleServiceServer
	handler http.Handler
	store   Store
	account uuid.UUID
	version string
	log     *slog.Logger
}

// New builds a bridge over the same handler the service mounts publicly.
func New(handler http.Handler, store Store, account uuid.UUID, version string, log *slog.Logger) *Server {
	return &Server{handler: handler, store: store, account: account, version: version, log: log}
}

// Call executes a relative API request as the console's admin principal.
// HTTP errors remain HTTP responses, including their JSON:API pointers.
func (s *Server) Call(ctx context.Context, in *licconsolev1.CallRequest) (*licconsolev1.CallResponse, error) {
	operator, ok := backplane.OperatorOf(ctx)
	if !ok {
		return nil, rpcError(codes.PermissionDenied, "a console operator session is required")
	}

	target, err := requestURL(in)
	if err != nil {
		s.audit(ctx, operator, "lic.console.call", "invalid-request", "rejected", 0)

		return nil, err
	}

	// A fixed origin is intentional: path/query cannot escape the in-process
	// router or become an SSRF request. No caller-controlled auth headers.
	r, err := http.NewRequestWithContext(httpapi.ConsoleContext(ctx, s.account),
		in.GetMethod(), "https://lic.console"+target.RequestURI(), bytes.NewReader(in.GetBody()))
	if err != nil {
		s.audit(ctx, operator, "lic.console.call", "invalid-request", "rejected", 0)

		return nil, rpcError(codes.InvalidArgument, "invalid HTTP request")
	}

	r.Header.Set("Accept", "application/vnd.api+json")
	r.Header.Set("Content-Type", "application/vnd.api+json")
	r.RemoteAddr = "127.0.0.1:0"

	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)

	outcome := "succeeded"
	if rec.Code >= http.StatusBadRequest {
		outcome = "failed"
	}

	// Never audit query values, bodies, response data, or secrets. API
	// paths identify subjects; query values can contain keys and emails.
	s.audit(ctx, operator, "lic.console.call", in.GetMethod()+" "+target.Path, outcome, rec.Code)

	headers := make(map[string]string, len(rec.Header()))
	for key, values := range rec.Header() {
		headers[key] = strings.Join(values, ", ")
	}

	return &licconsolev1.CallResponse{
		Status:  int32(rec.Code), //nolint:gosec // HTTP status is in [100,599].
		Headers: headers, Body: rec.Body.Bytes(),
	}, nil
}

func requestURL(in *licconsolev1.CallRequest) (*url.URL, error) {
	if in == nil || len(in.GetBody()) > maxBodyBytes {
		return nil, rpcError(codes.InvalidArgument, "request body exceeds 1 MiB")
	}

	switch in.GetMethod() {
	case http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodHead:
	default:
		return nil, rpcError(codes.InvalidArgument, "unsupported HTTP method")
	}

	target, err := url.Parse(in.GetPath())
	if err != nil || target.IsAbs() || target.Host != "" || target.User != nil ||
		target.RawQuery != "" || target.Fragment != "" || !strings.HasPrefix(target.Path, "/v1/") ||
		strings.HasPrefix(in.GetPath(), "//") || strings.ContainsAny(target.Path, "\r\n") {
		return nil, rpcError(codes.InvalidArgument, "path must be a relative /v1/ API path without query or fragment")
	}

	q := make(url.Values, len(in.GetQuery()))
	for key, value := range in.GetQuery() {
		q.Set(key, value)
	}

	target.RawQuery = q.Encode()

	return target, nil
}

// Info exposes public verification material, never private keys or secrets.
func (s *Server) Info(ctx context.Context, _ *licconsolev1.InfoRequest) (*licconsolev1.InfoResponse, error) {
	operator, ok := backplane.OperatorOf(ctx)
	if !ok {
		return nil, rpcError(codes.PermissionDenied, "a console operator session is required")
	}

	account, err := s.store.Account(ctx, s.account)
	if err != nil {
		s.audit(ctx, operator, "lic.console.info", "account/"+s.account.String(), "failed", 0)
		s.log.ErrorContext(ctx, "console account lookup failed", "error", err)

		return nil, rpcError(codes.Unavailable, "account is not ready")
	}

	counts, err := s.store.ConsoleCounts(ctx, s.account)
	if err != nil {
		s.audit(ctx, operator, "lic.console.info", "account/"+s.account.String(), "failed", 0)
		s.log.ErrorContext(ctx, "console resource counts failed", "error", err)

		return nil, rpcError(codes.Internal, "resource counts failed; see the service log")
	}

	s.audit(ctx, operator, "lic.console.info", "account/"+s.account.String(), "succeeded", http.StatusOK)

	return &licconsolev1.InfoResponse{
		AccountId: account.ID.String(), AccountSlug: account.Slug, AccountName: account.Name,
		Version: s.version, Counts: counts,
		PublicKeys: map[string]string{
			"rsa": account.Keys.RSAPublic, "ed25519": account.Keys.Ed25519Public, "ecdsa": account.Keys.ECDSAPublic,
		},
	}, nil
}

func (s *Server) audit(ctx context.Context, operator backplane.Operator, action, subject, outcome string, code int) {
	s.log.InfoContext(ctx, action,
		backplane.AuditLabel, true, backplane.AuditID, uuid.NewString(),
		"event.name", action, backplane.AuditActor, operator.Actor(),
		backplane.AuditSubject, subject, backplane.AuditOutcome, outcome,
		"http.response.status_code", code)
}

var _ licconsolev1.ConsoleServiceServer = (*Server)(nil)

// rpcError preserves the wire status without wrapping an application error.
func rpcError(code codes.Code, message string) error {
	return status.Error(code, message) //nolint:wrapcheck // gRPC statuses are the boundary result.
}
