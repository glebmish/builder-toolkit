// Snippet: internal/cmd/schema.go — runtime introspection of the embedded spec.
//
// The OpenAPI spec is embedded at build time via go:embed. The
// operationIDToCommand map is the single source of truth for spec ↔ CLI
// names; the bijection test (integration_test.go) enforces that the map
// has no orphans in either direction.
//
// `acme schema --list` prints the table; `acme schema <op>` shows one
// operation; `acme schema <Type>` shows a schema definition. Errors that
// indicate spec/schema problems return *cliexit.DiscoveryError so they
// exit with code 4.
package cmd

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/example/acme-cli/internal/cliexit"
	"github.com/spf13/cobra"
)

//go:embed openapi-spec.json
var specData []byte

// operationIDToCommand maps spec operationIds → dotted CLI command names.
// Kept in sync with each resource file's init() — the bijection test
// (integration_test.go) fails if you drift.
var operationIDToCommand = map[string]string{
	"listProjects":  "projects.list",
	"createProject": "projects.create",
	// ... one entry per operation
}

func discoveryErr(err error) error {
	if err == nil {
		return nil
	}
	return &cliexit.DiscoveryError{Err: err}
}

var schemaCmd = &cobra.Command{
	Use:   "schema [<resource>.<action>|<TypeName>]",
	Short: "Inspect the embedded API schema",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		spec, err := parseSpec()
		if err != nil {
			return err
		}
		resolveRefs, _ := cmd.Flags().GetBool("resolve-refs")
		if list, _ := cmd.Flags().GetBool("list"); list {
			return printList(cmd.OutOrStdout(), spec)
		}
		if len(args) == 0 {
			return discoveryErr(fmt.Errorf("provide an operation (e.g. projects.list) or type (e.g. Project)\n  Use --list to see all operations"))
		}
		arg := args[0]
		if !strings.Contains(arg, ".") {
			return printType(spec, arg, resolveRefs)
		}
		return printOperation(spec, arg, resolveRefs)
	},
}

func init() {
	schemaCmd.Flags().Bool("list", false, "List all operations")
	schemaCmd.Flags().Bool("resolve-refs", false, "Inline $ref references")
	rootCmd.AddCommand(schemaCmd)
}

func parseSpec() (map[string]any, error) {
	var spec map[string]any
	if err := json.Unmarshal(specData, &spec); err != nil {
		return nil, discoveryErr(fmt.Errorf("parsing embedded OpenAPI spec: %w", err))
	}
	return spec, nil
}

// printList writes to the passed writer rather than os.Stdout so cobra's
// output capture works and the command is testable.
func printList(w io.Writer, spec map[string]any) error {
	type row struct{ cli, method, path string }
	var rows []row
	paths, _ := spec["paths"].(map[string]any)
	for p, item := range paths {
		pi, _ := item.(map[string]any)
		for _, m := range []string{"get", "post", "put", "delete", "patch"} {
			op, ok := pi[m].(map[string]any)
			if !ok {
				continue
			}
			opID, _ := op["operationId"].(string)
			cli := operationIDToCommand[opID]
			if cli == "" {
				cli = "(unmapped) " + opID
			}
			rows = append(rows, row{cli, strings.ToUpper(m), p})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].cli < rows[j].cli })
	for _, r := range rows {
		fmt.Fprintf(w, "  %-40s %-6s %s\n", r.cli, r.method, r.path)
	}
	return nil
}

// printOperation and printType: walk spec, find the target, json.MarshalIndent.
// resolveRefs uses a simple recursive walker that replaces
// {"$ref": "#/components/schemas/X"} with the resolved schema.
// Implementation omitted from snippet for brevity; ~50 LOC straightforward.
func printOperation(spec map[string]any, cliName string, resolveRefs bool) error {
	return discoveryErr(fmt.Errorf("printOperation not implemented in snippet — see reference impl"))
}

func printType(spec map[string]any, name string, resolveRefs bool) error {
	return discoveryErr(fmt.Errorf("printType not implemented in snippet — see reference impl"))
}
