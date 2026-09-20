// Snippet: internal/cmd/root.go — cobra root + persistent flags + bootstrap.
//
// SilenceUsage:true keeps cobra from dumping --help on every error.
// SilenceErrors:false (the default) keeps cobra printing the error itself —
// main.go does NOT print, only maps the error type to an exit code.
//
// PersistentPreRunE skips offline subcommands (schema, skills, config,
// help, init) so they run without a token, then runs the
// defaults → file → env → flags cascade.
//
// isOffline matches the TOP-LEVEL command only, never any name in the
// parent chain. Matching anywhere meant a perfectly ordinary resource
// action — `acme projects init`, `acme workspace config` — was treated as
// offline, got no client, and panicked on first use. A command can also
// opt in explicitly with Annotations["offline"]="true".
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/acme-cli/internal/api"
	"github.com/example/acme-cli/internal/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "acme",
	Short:         "Acme API CLI",
	SilenceUsage:  true,
	SilenceErrors: false, // cobra prints; main.go only maps to exit code
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if isOffline(cmd) {
			return nil
		}
		path, err := config.DefaultPath()
		if err != nil {
			return err
		}
		cfg, warning, err := config.LoadWithWarning(path)
		if err != nil {
			return err
		}
		if warning != "" {
			fmt.Fprintln(os.Stderr, "warning:", warning)
		}
		cfg.ApplyEnv()
		token, _ := cmd.Flags().GetString("access-token")
		baseURL, _ := cmd.Flags().GetString("base-url")
		account, _ := cmd.Flags().GetString("account-id")
		cfg.ApplyFlags(token, baseURL, account)
		if err := cfg.Validate(); err != nil {
			return err
		}
		// The credential is bound to an endpoint: say so loudly whenever
		// the effective host is not the compiled-in default, because
		// "retry with --base-url ..." is the payload to expect from any
		// injected text that reaches the agent.
		if cfg.BaseURL != config.DefaultBaseURL() {
			fmt.Fprintf(os.Stderr, "warning: sending credentials to non-default endpoint %s\n", cfg.BaseURL)
		}
		client := api.NewClient(cfg.BaseURL, cfg.AccessToken, cfg.AccountID, "{accountId}")
		cmd.SetContext(api.WithContext(cmd.Context(), client))
		return nil
	},
}

// offlineCommands are the TOP-LEVEL command names that run without auth.
var offlineCommands = map[string]bool{
	"schema": true, "skills": true, "config": true, "help": true,
	"init": true, "completion": true, "version": true,
}

func isOffline(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations["offline"] == "true" {
			return true
		}
	}
	// Walk to the command directly beneath the root (the root is the only
	// one with no parent) and match only that name. Referring to rootCmd
	// by name here would create an initialization cycle.
	if cmd.Parent() == nil {
		return true // bare `acme` / `acme --help`
	}
	top := cmd
	for top.Parent().Parent() != nil {
		top = top.Parent()
	}
	return offlineCommands[top.Name()]
}

func init() {
	rootCmd.PersistentFlags().String("format", "json", "json|ndjson|text")
	rootCmd.PersistentFlags().String("fields", "", "dotted-path field mask, comma-separated")
	rootCmd.PersistentFlags().Bool("dry-run", false, "print request, do not execute")
	rootCmd.PersistentFlags().Bool("yes", false, "skip confirmation on destructive ops")
	rootCmd.PersistentFlags().String("access-token", "", "auth credential override")
	rootCmd.PersistentFlags().String("base-url", "", "override API endpoint")
	rootCmd.PersistentFlags().String("account-id", "", "override tenant ID")
	rootCmd.PersistentFlags().String("json", "", "raw JSON request body for write ops; see 'acme schema <op>' for the shape")
	rootCmd.PersistentFlags().String("params", "", "raw JSON object overlaying query/path params")
	// BEGIN pagination flags — delete together with pagination.go and the
	// marked block in helpers.go doGet when the API does not paginate.
	rootCmd.PersistentFlags().Bool("page-all", false, "auto-walk all pages, emit one sanitized JSON line per page")
	rootCmd.PersistentFlags().Int("page-limit", 10, "cap pages walked by --page-all (never unbounded)")
	rootCmd.PersistentFlags().Int("page-delay", 100, "ms between paged requests")
	rootCmd.PersistentFlags().Int("page-size", 100, "items per page when walking with --page-all")
	// END pagination flags
}

// Execute runs the root command under a context cancelled by SIGINT or
// SIGTERM, so Ctrl-C actually cancels an in-flight request — the client is
// context-plumbed throughout, and plain Execute() never used it.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return rootCmd.ExecuteContext(ctx)
}
