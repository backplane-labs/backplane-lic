// lic is the Backplane licensing service: a 1:1 clone of the Keygen CE
// licensing API (URLs, JSON:API documents, headers, error codes,
// behavior) so Keygen clients cannot tell the difference. Singleplayer,
// API version 1.7.
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

	// The public JSON:API. The middleware pipeline of the architecture
	// contract (request id, path normalisation, negotiation,
	// authentication, response signing) wraps svc.State().HTTP here as
	// it lands in phase A2.
	svc.HTTP("/v1/", svc.State().HTTP)

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
