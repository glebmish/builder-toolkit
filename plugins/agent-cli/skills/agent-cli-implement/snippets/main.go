// Snippet: main.go — entry point.
//
// Two responsibilities: run cobra, map the returned error type to a
// structured exit code (0–5 per docs/design.md). Does NOT print the error
// itself — cobra already does (SilenceUsage:true, SilenceErrors:false).
// Printing here on top would double every error.
//
// Substitute the module path "github.com/example/acme-cli" with yours.
package main

import (
	"errors"
	"os"

	"github.com/example/acme-cli/internal/api"
	"github.com/example/acme-cli/internal/cliexit"
	"github.com/example/acme-cli/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	var authErr *cliexit.AuthError
	if errors.As(err, &authErr) {
		return 2
	}
	var valErr *cliexit.ValidationError
	if errors.As(err, &valErr) {
		return 3
	}
	var discErr *cliexit.DiscoveryError
	if errors.As(err, &discErr) {
		return 4
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		if apiErr.IsAuth() { // 401/403
			return 2
		}
		return 1
	}
	return 1 // unexpected; reserve 5 for panics if you wire a recover
}
