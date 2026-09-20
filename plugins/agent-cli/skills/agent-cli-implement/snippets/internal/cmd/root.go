// Snippet: internal/cmd/root.go — cobra root + persistent flags + bootstrap.
//
// SilenceUsage:true keeps cobra from dumping --help on every error.
// SilenceErrors:false (the default) keeps cobra printing the error itself —
// main.go does NOT print, only maps the error type to an exit code.
//
// PersistentPreRunE skips offline subcommands (schema, skills, config,
// help, init) so they run without a token, then runs the
// defaults → file → env → flags cascade. The "skills" case covers
// `skills list`, `skills get`, and `skills install` because the parent-
// chain walk in isOffline matches the group name.
package cmd

import (
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
		cfg, err := config.Load(config.DefaultPath())
		if err != nil {
			return err
		}
		cfg.ApplyEnv()
		token, _ := cmd.Flags().GetString("access-token")
		baseURL, _ := cmd.Flags().GetString("base-url")
		account, _ := cmd.Flags().GetString("account-id")
		cfg.ApplyFlags(token, baseURL, account)
		if err := cfg.Validate(); err != nil {
			return err
		}
		client := api.NewClient(cfg.BaseURL, cfg.AccessToken, cfg.AccountID, "{accountId}")
		cmd.SetContext(api.WithContext(cmd.Context(), client))
		return nil
	},
}

func isOffline(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "schema", "skills", "config", "help", "init":
			return true
		}
	}
	return false
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
	// Pagination (only if API paginates):
	rootCmd.PersistentFlags().Bool("page-all", false, "auto-walk all pages, emit NDJSON, one page per line")
	rootCmd.PersistentFlags().Int("page-limit", 10, "cap pages walked by --page-all (never unbounded)")
	rootCmd.PersistentFlags().Int("page-delay", 100, "ms between paged requests")
	rootCmd.PersistentFlags().Int("page-size", 100, "items per page when walking with --page-all")
}

func Execute() error { return rootCmd.Execute() }
