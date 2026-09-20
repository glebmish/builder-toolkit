// Snippet: internal/cmd/helpers.go — workhorses + accessors.
//
// Every resource-file command goes through one of these:
//
//	doGet     — JSON GET, with --page-all → doPaginate
//	doMutate  — POST/PUT/PATCH with optional JSON body
//	doDelete  — HTTP DELETE with --yes confirmation
//
// Each helper checks --dry-run before any HTTP call — including the
// destructive ones, where the confirmation gate must not preempt the
// preview. Each routes through format.Write (sanitization + field mask)
// on the way to stdout; nothing writes response bytes to stdout directly.
//
// API-specific helpers (extend, don't inline) — add when patterns repeat:
//
//	doDownload    — binary GET (charts, exports, attachments)
//	doUpload      — multipart POST (file imports, avatars)
//	doPostDelete  — POST-with-body delete (e.g. /trades/delete)
package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/example/acme-cli/internal/api"
	"github.com/example/acme-cli/internal/cliexit"
	"github.com/example/acme-cli/internal/format"
	"github.com/example/acme-cli/internal/validate"
	"github.com/spf13/cobra"
)

func fmtOpts(cmd *cobra.Command) (format.Options, error) {
	f, _ := cmd.Flags().GetString("format")
	canonical, err := format.Validate(f)
	if err != nil {
		return format.Options{}, &cliexit.ValidationError{Err: err}
	}
	fields, _ := cmd.Flags().GetString("fields")
	return format.FormatFromFlag(canonical, fields), nil
}

// client fetches the bootstrapped API client, surfacing a typed error
// instead of a nil pointer when the command was treated as offline.
func client(cmd *cobra.Command) (*api.Client, error) {
	return api.FromContext(cmd.Context())
}

// maxResponseBody caps a success body the same way the error path is
// capped — an unbounded read lands straight in the agent's context.
const maxResponseBody = 32 << 20

func readBody(r io.Reader, what string) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", what, err)
	}
	return body, nil
}

func requireString(cmd *cobra.Command, name string) (string, error) {
	v, _ := cmd.Flags().GetString(name)
	if v == "" {
		return "", &cliexit.ValidationError{Err: fmt.Errorf("--%s is required", name)}
	}
	return v, nil
}

func requireJSON(cmd *cobra.Command) (string, error) {
	body, err := requireString(cmd, "json")
	if err != nil {
		return "", err
	}
	if err := validate.JSONBody(body); err != nil {
		return "", err
	}
	return body, nil
}

// mergeParams overlays the persistent --params JSON onto base. Order
// matters: --params is overlaid first, then `base` is written on top so
// caller-set keys (validated path params, etc.) win on collision.
//
// --params cannot override a param the command EXPLICITLY SET, but it is
// not "strictly additive": a {placeholder} the command left unset is
// still filled from here and substituted into the path. That is
// deliberate — it is how an agent reaches a sub-resource the command did
// not expose — so the safety comes from buildURL escaping every
// substituted value, not from this function refusing it.
func mergeParams(cmd *cobra.Command, base map[string]string) (map[string]string, error) {
	merged := map[string]string{}
	raw, _ := cmd.Flags().GetString("params")
	if raw != "" {
		if err := validate.JSONBody(raw); err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
		dec.UseNumber() // else 1000000 renders as "1e+06" on the wire
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			return nil, &cliexit.ValidationError{Err: fmt.Errorf("--params: %w", err)}
		}
		for k, v := range m {
			sv, err := paramString(v)
			if err != nil {
				return nil, &cliexit.ValidationError{Err: fmt.Errorf("--params %s: %w", k, err)}
			}
			merged[k] = sv
		}
	}
	for k, v := range base {
		merged[k] = v
	}
	return merged, nil
}

// paramString renders a JSON scalar as the server should see it.
// json.Number keeps the caller's literal, so large integers survive and
// nothing arrives in scientific notation.
func paramString(v any) (string, error) {
	switch val := v.(type) {
	case json.Number:
		return val.String(), nil
	case string:
		return val, nil
	case bool:
		return fmt.Sprintf("%t", val), nil
	case nil:
		return "", nil
	default:
		return "", fmt.Errorf("expected a scalar, got %T", v)
	}
}

func doGet(cmd *cobra.Command, path string, params map[string]string) error {
	merged, err := mergeParams(cmd, params)
	if err != nil {
		return err
	}
	opts, err := fmtOpts(cmd)
	if err != nil {
		return err
	}
	c, err := client(cmd)
	if err != nil {
		return err
	}
	if dr, _ := cmd.Flags().GetBool("dry-run"); dr {
		preview, err := c.DryRun("GET", path, merged, nil)
		if err != nil {
			return err
		}
		return format.DryRunOutput(os.Stdout, preview)
	}
	// BEGIN pagination — delete this block together with pagination.go and
	// the four page-* flags in root.go when the API does not paginate.
	if all, _ := cmd.Flags().GetBool("page-all"); all {
		return doPaginate(cmd, path, merged, opts)
	}
	// END pagination
	resp, err := c.DoWithContext(cmd.Context(), "GET", path, merged, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := readBody(resp.Body, "response")
	if err != nil {
		return err
	}
	return format.Write(os.Stdout, body, opts)
}

func doMutate(cmd *cobra.Command, method, path string, params map[string]string, jsonBody string) error {
	merged, err := mergeParams(cmd, params)
	if err != nil {
		return err
	}
	opts, err := fmtOpts(cmd)
	if err != nil {
		return err
	}
	c, err := client(cmd)
	if err != nil {
		return err
	}
	var body []byte
	if jsonBody != "" {
		if err := validate.JSONBody(jsonBody); err != nil {
			return err
		}
		body = []byte(jsonBody)
	}
	if dr, _ := cmd.Flags().GetBool("dry-run"); dr {
		preview, err := c.DryRun(method, path, merged, body)
		if err != nil {
			return err
		}
		return format.DryRunOutput(os.Stdout, preview)
	}
	resp, err := c.DoWithContext(cmd.Context(), method, path, merged, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := readBody(resp.Body, "response")
	if err != nil {
		return err
	}
	if len(respBody) == 0 {
		// 204 No Content — typical for action endpoints. Tell the agent.
		fmt.Fprintf(os.Stderr, "%s %s → %d (empty body; refetch to see the effect)\n", method, path, resp.StatusCode)
		return nil
	}
	return format.Write(os.Stdout, respBody, opts)
}

// doDelete previews before it confirms. --dry-run must reach the preview:
// an agent asking "what would this delete?" was answered with "pass --yes"
// when the confirmation gate ran first, which is the one case --dry-run
// exists to serve.
func doDelete(cmd *cobra.Command, path string, params map[string]string, resource, id string) error {
	if dr, _ := cmd.Flags().GetBool("dry-run"); !dr {
		if err := confirmDelete(cmd, resource, id); err != nil {
			return err
		}
	}
	return doMutate(cmd, "DELETE", path, params, "")
}

func confirmDelete(cmd *cobra.Command, resource, id string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if yes {
		return nil
	}
	// A failed Stat (closed stdin, as in some agent harnesses) is treated
	// as non-interactive rather than dereferenced — this is the
	// destructive-operation path, the worst place for a nil panic.
	fi, err := os.Stdin.Stat()
	if err != nil || (fi.Mode()&os.ModeCharDevice) == 0 {
		return &cliexit.ValidationError{Err: fmt.Errorf("delete %s %s requires --yes in non-interactive mode", resource, id)}
	}
	fmt.Fprintf(os.Stderr, "Delete %s %s? [y/N] ", resource, id)
	var resp string
	// A read failure means no affirmative answer; fall through to cancel.
	if _, err := fmt.Scanln(&resp); err != nil && !errors.Is(err, io.EOF) {
		resp = ""
	}
	if resp != "y" && resp != "Y" {
		return &cliexit.ValidationError{Err: fmt.Errorf("cancelled")}
	}
	return nil
}
