package cmd

import (
	"errors"
	"testing"

	"github.com/example/acme-cli/internal/cliexit"
	"github.com/spf13/cobra"
)

func paramsCmd(t *testing.T, raw string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "x"}
	c.Flags().String("params", raw, "")
	return c
}

// Finding #2: numbers must reach the server as the caller wrote them.
func TestMergeParamsPreservesNumbers(t *testing.T) {
	got, err := mergeParams(paramsCmd(t, `{"offset":1000000,"id":1234567890123456789}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got["offset"] != "1000000" {
		t.Errorf("offset = %q, want 1000000", got["offset"])
	}
	if got["id"] != "1234567890123456789" {
		t.Errorf("id = %q, want 1234567890123456789", got["id"])
	}
}

// A non-scalar param has no sane query-string rendering; reject it rather
// than sending Go's map formatting over the wire.
func TestMergeParamsRejectsNonScalar(t *testing.T) {
	_, err := mergeParams(paramsCmd(t, `{"nested":{"a":1}}`), nil)
	if err == nil {
		t.Fatal("expected non-scalar param to be rejected")
	}
	var ve *cliexit.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("want ValidationError (exit 3), got %T", err)
	}
}

// Finding: caller-set params still win over --params.
func TestMergeParamsBaseWins(t *testing.T) {
	got, err := mergeParams(paramsCmd(t, `{"projectId":"evil"}`), map[string]string{"projectId": "42"})
	if err != nil {
		t.Fatal(err)
	}
	if got["projectId"] != "42" {
		t.Errorf("base should win, got %q", got["projectId"])
	}
}

// Finding #9: a refusal or cancellation is a validation failure (exit 3),
// not an API error (exit 1) — an agent must be able to tell them apart.
func TestConfirmDeleteIsTypedValidationError(t *testing.T) {
	c := &cobra.Command{Use: "delete"}
	c.Flags().Bool("yes", false, "")
	err := confirmDelete(c, "projects", "42")
	if err == nil {
		t.Fatal("expected refusal without --yes in non-interactive mode")
	}
	var ve *cliexit.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("want ValidationError (exit 3), got %T", err)
	}
}

// Finding #6: isOffline must key on the top-level command only, so an
// ordinary resource action named `init` or `config` still gets a client.
func TestIsOfflineMatchesTopLevelOnly(t *testing.T) {
	root := &cobra.Command{Use: "acme"}
	projects := &cobra.Command{Use: "projects"}
	projInit := &cobra.Command{Use: "init"}
	projects.AddCommand(projInit)
	root.AddCommand(projects)

	skills := &cobra.Command{Use: "skills"}
	install := &cobra.Command{Use: "install"}
	skills.AddCommand(install)
	root.AddCommand(skills)

	if isOffline(projInit) {
		t.Error("`projects init` must not be treated as offline")
	}
	if !isOffline(install) {
		t.Error("`skills install` must be treated as offline")
	}
}

func TestIsOfflineHonoursAnnotation(t *testing.T) {
	root := &cobra.Command{Use: "acme"}
	c := &cobra.Command{Use: "whoami", Annotations: map[string]string{"offline": "true"}}
	root.AddCommand(c)
	if !isOffline(c) {
		t.Error("explicit offline annotation ignored")
	}
}
