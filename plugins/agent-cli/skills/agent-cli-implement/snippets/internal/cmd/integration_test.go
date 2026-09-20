// Snippet: internal/cmd/integration_test.go — bijection test.
//
// operationIDToCommand is the contract between the embedded spec and the
// cobra tree, and it has to hold in every direction to be worth anything:
//
//	spec  → map    every operationId has a CLI mapping (new endpoint added)
//	map   → spec   every mapping still exists in the spec (endpoint removed)
//	map   → cobra  every mapped name resolves to a command (typo, rename)
//	cobra → map    every registered command is mapped (forgot to register)
//
// Checking only the first and third lets a stale entry survive a
// regenerated spec: `schema --list` stops advertising it while agents can
// still invoke it against a dead endpoint — the exact silent API drift
// this test is sold as preventing.
//
// File location is flexible: schema_test.go also works. Pick one and keep
// all four directions in the same file.
package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// specOperationIDs collects every operationId in the embedded spec.
func specOperationIDs(t *testing.T) map[string]bool {
	t.Helper()
	spec, err := parseSpec()
	if err != nil {
		t.Fatal(err)
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		// Guards against a truncated or empty spec silently turning every
		// assertion below into a no-op.
		t.Fatal("embedded spec has no paths — is openapi-spec.json empty or truncated?")
	}
	ids := map[string]bool{}
	for _, item := range paths {
		pi, _ := item.(map[string]any)
		for _, m := range []string{"get", "post", "put", "delete", "patch"} {
			op, ok := pi[m].(map[string]any)
			if !ok {
				continue
			}
			if id, _ := op["operationId"].(string); id != "" {
				ids[id] = true
			}
		}
	}
	if len(ids) == 0 {
		t.Fatal("embedded spec declares no operationIds")
	}
	return ids
}

// resolve walks a dotted CLI name through the command tree. Handles names
// of any depth, not just two levels — `skills.install` and deeper groups
// both resolve.
func resolve(name string) *cobra.Command {
	cur := rootCmd
	for _, seg := range strings.Split(name, ".") {
		var next *cobra.Command
		for _, sub := range cur.Commands() {
			if sub.Name() == seg {
				next = sub
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// registeredLeaves returns the dotted name of every runnable command,
// skipping cobra's built-ins and the offline utility groups.
func registeredLeaves(cur *cobra.Command, prefix string, out map[string]bool) {
	for _, sub := range cur.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		name := sub.Name()
		if prefix != "" {
			name = prefix + "." + sub.Name()
		}
		if len(sub.Commands()) > 0 {
			registeredLeaves(sub, name, out)
			continue
		}
		if offlineCommands[strings.SplitN(name, ".", 2)[0]] {
			continue
		}
		if sub.RunE != nil || sub.Run != nil {
			out[name] = true
		}
	}
}

func TestEveryOperationIDIsMapped(t *testing.T) {
	var missing []string
	for id := range specOperationIDs(t) {
		if _, ok := operationIDToCommand[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d operationIds missing a CLI mapping: %v", len(missing), missing)
	}
}

func TestEveryMappingStillExistsInSpec(t *testing.T) {
	ids := specOperationIDs(t)
	var stale []string
	for id, cliName := range operationIDToCommand {
		if !ids[id] {
			stale = append(stale, id+" → "+cliName)
		}
	}
	if len(stale) > 0 {
		t.Fatalf("%d mappings reference operationIds no longer in the spec: %v", len(stale), stale)
	}
}

func TestEveryMappedCLINameExists(t *testing.T) {
	var missing []string
	for _, cliName := range operationIDToCommand {
		if resolve(cliName) == nil {
			missing = append(missing, cliName)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d mapped CLI names not registered: %v", len(missing), missing)
	}
}

func TestEveryRegisteredCommandIsMapped(t *testing.T) {
	mapped := map[string]bool{}
	for _, cliName := range operationIDToCommand {
		mapped[cliName] = true
	}
	leaves := map[string]bool{}
	registeredLeaves(rootCmd, "", leaves)
	var unmapped []string
	for name := range leaves {
		if !mapped[name] {
			unmapped = append(unmapped, name)
		}
	}
	if len(unmapped) > 0 {
		t.Fatalf("%d registered commands have no spec mapping: %v", len(unmapped), unmapped)
	}
}
