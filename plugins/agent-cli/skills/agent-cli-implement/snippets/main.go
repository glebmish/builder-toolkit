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
	"fmt"
	"os"
	"runtime/debug"

	"github.com/example/acme-cli/internal/api"
	"github.com/example/acme-cli/internal/cliexit"
	"github.com/example/acme-cli/internal/cmd"
)

func main() {
	os.Exit(run())
}

// run wraps Execute so a panic becomes exit 5 (internal error) rather
// than Go's default exit 2 — the code the contract reserves for auth
// failures. Without this recover, any nil-deref told the agent its token
// was bad and sent it off to re-authenticate. os.Exit is called from
// main, never here, so deferred cleanup still runs.
func run() (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "internal error: %v\n%s\n", r, debug.Stack())
			code = 5
		}
	}()
	if err := cmd.Execute(); err != nil {
		return exitCode(err)
	}
	return 0
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
	return 1 // unexpected; panics map to 5 via the recover in run()
}
