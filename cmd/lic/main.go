// lic is the Backplane licensing service: a 1:1 clone of the Keygen CE
// licensing API (URLs, JSON:API documents, headers, error codes,
// behavior) so Keygen clients cannot tell the difference. Singleplayer,
// API version 1.8.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/backplane-labs/backplane/pkg/backplane"
	"github.com/backplane-labs/backplane/pkg/backplane/drivers/standard"

	"github.com/backplane-labs/backplane-lic/internal/service"
)

func run(ctx context.Context) error {
	svc, err := backplane.Open(ctx, service.New, standard.Drivers(), backplane.Name("lic"))
	if err != nil {
		return fmt.Errorf("lic: %w", err)
	}

	// The public JSON:API: the full middleware pipeline mounted at the
	// root, so paths outside /v1 get the reference's 404 and the
	// singleplayer bare paths (/v1/me, /v1/tokens, ...) resolve.
	svc.HTTP("/", svc.State().HTTP)

	if err := svc.Run(ctx); err != nil {
		return fmt.Errorf("lic: %w", err)
	}

	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
