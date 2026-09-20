// Snippet: internal/cmd/integration_test.go — bijection test.
//
// Direction 1: every operationId in the embedded spec must be in
// operationIDToCommand. Direction 2: every dotted CLI name in the map
// must resolve to a registered cobra command. Together they catch API
// drift the moment you regenerate the spec.
//
// File location is flexible: schema_test.go also works. Pick one and
// keep both directions in the same file.
package cmd

import (
	"strings"
	"testing"
)

func TestAllOperationIdsMapped(t *testing.T) {
	spec, err := parseSpec()
	if err != nil {
		t.Fatal(err)
	}
	paths, _ := spec["paths"].(map[string]interface{})
	var missing []string
	for _, item := range paths {
		pi, _ := item.(map[string]interface{})
		for _, m := range []string{"get", "post", "put", "delete", "patch"} {
			op, ok := pi[m].(map[string]interface{})
			if !ok {
				continue
			}
			opID, _ := op["operationId"].(string)
			if opID == "" {
				continue
			}
			if _, ok := operationIDToCommand[opID]; !ok {
				missing = append(missing, opID)
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d operationIds missing CLI mapping: %v", len(missing), missing)
	}
}

func TestEveryMappedCLINameExists(t *testing.T) {
	var missing []string
	for _, cliName := range operationIDToCommand {
		parts := strings.SplitN(cliName, ".", 2)
		if len(parts) != 2 {
			continue
		}
		group := rootCmd
		for _, sub := range group.Commands() {
			if sub.Name() == parts[0] {
				group = sub
				break
			}
		}
		if group == rootCmd {
			missing = append(missing, cliName+" (group not found)")
			continue
		}
		found := false
		for _, sub := range group.Commands() {
			if sub.Name() == parts[1] {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, cliName)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d mapped CLI names not registered: %v", len(missing), missing)
	}
}
