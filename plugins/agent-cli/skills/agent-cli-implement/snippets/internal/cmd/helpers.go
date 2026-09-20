// Snippet: internal/cmd/helpers.go — workhorses + accessors.
//
// Every resource-file command goes through one of these:
//
//	doGet     — JSON GET, with --page-all → doPaginate
//	doMutate  — POST/PUT/PATCH with optional JSON body
//	doDelete  — HTTP DELETE with --yes confirmation
//
// Each helper checks --dry-run before any HTTP call. Each routes through
// format.Write (sanitization + field mask) on the way to stdout.
//
// API-specific helpers (extend, don't inline) — add when patterns repeat:
//
//	doDownload    — binary GET (charts, exports, attachments)
//	doUpload      — multipart POST (file imports, avatars)
//	doPostDelete  — POST-with-body delete (e.g. /trades/delete)
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/example/acme-cli/internal/api"
	"github.com/example/acme-cli/internal/cliexit"
	"github.com/example/acme-cli/internal/format"
	"github.com/example/acme-cli/internal/validate"
	"github.com/spf13/cobra"
)

func fmtOpts(cmd *cobra.Command) format.Options {
	f, _ := cmd.Flags().GetString("format")
	fields, _ := cmd.Flags().GetString("fields")
	return format.FormatFromFlag(f, fields)
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
// caller-set keys (validated path params, etc.) win on collision. --params
// is strictly additive — it can fill gaps the command didn't expose, but
// cannot silently override path or required params the command built.
func mergeParams(cmd *cobra.Command, base map[string]string) (map[string]string, error) {
	merged := map[string]string{}
	raw, _ := cmd.Flags().GetString("params")
	if raw != "" {
		if err := validate.JSONBody(raw); err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			return nil, &cliexit.ValidationError{Err: fmt.Errorf("--params: %w", err)}
		}
		for k, v := range m {
			merged[k] = fmt.Sprint(v)
		}
	}
	for k, v := range base {
		merged[k] = v
	}
	return merged, nil
}

func doGet(cmd *cobra.Command, path string, params map[string]string) error {
	merged, err := mergeParams(cmd, params)
	if err != nil {
		return err
	}
	c := api.FromContext(cmd.Context())
	if dr, _ := cmd.Flags().GetBool("dry-run"); dr {
		return format.DryRunOutput(os.Stdout, c.DryRun("GET", path, merged, nil))
	}
	if all, _ := cmd.Flags().GetBool("page-all"); all {
		return doPaginate(cmd, path, merged)
	}
	resp, err := c.DoWithContext(cmd.Context(), "GET", path, merged, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	return format.Write(os.Stdout, body, fmtOpts(cmd))
}

func doMutate(cmd *cobra.Command, method, path string, params map[string]string, jsonBody string) error {
	merged, err := mergeParams(cmd, params)
	if err != nil {
		return err
	}
	c := api.FromContext(cmd.Context())
	var body []byte
	if jsonBody != "" {
		if err := validate.JSONBody(jsonBody); err != nil {
			return err
		}
		body = []byte(jsonBody)
	}
	if dr, _ := cmd.Flags().GetBool("dry-run"); dr {
		return format.DryRunOutput(os.Stdout, c.DryRun(method, path, merged, body))
	}
	resp, err := c.DoWithContext(cmd.Context(), method, path, merged, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if len(respBody) == 0 {
		// 204 No Content — typical for action endpoints. Tell the agent.
		fmt.Fprintf(os.Stderr, "%s %s → %d (empty body; refetch to see the effect)\n", method, path, resp.StatusCode)
		return nil
	}
	return format.Write(os.Stdout, respBody, fmtOpts(cmd))
}

func doDelete(cmd *cobra.Command, path string, params map[string]string, resource, id string) error {
	if err := confirmDelete(cmd, resource, id); err != nil {
		return err
	}
	return doMutate(cmd, "DELETE", path, params, "")
}

func confirmDelete(cmd *cobra.Command, resource, id string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if yes {
		return nil
	}
	fi, _ := os.Stdin.Stat()
	if (fi.Mode() & os.ModeCharDevice) == 0 {
		return fmt.Errorf("delete %s %s requires --yes flag in non-interactive mode", resource, id)
	}
	fmt.Fprintf(os.Stderr, "Delete %s %s? [y/N] ", resource, id)
	var resp string
	fmt.Scanln(&resp)
	if resp != "y" && resp != "Y" {
		return fmt.Errorf("cancelled")
	}
	return nil
}

// ---- Pagination (only if API paginates) ----------------------------------

// doPaginate walks an offset/limit GET endpoint with --page-all. Emits
// one NDJSON line per page on stdout. Stops when the page response's
// items array is shorter than --page-size, or when --page-limit is hit.
//
// For page-number or cursor schemes, swap the per-scheme line at the
// marked spot and adjust the stop condition.
func doPaginate(cmd *cobra.Command, path string, params map[string]string) error {
	c := api.FromContext(cmd.Context())
	pageSize, _ := cmd.Flags().GetInt("page-size")
	if pageSize <= 0 {
		pageSize = 100
	}
	pageLimit, _ := cmd.Flags().GetInt("page-limit")
	if pageLimit <= 0 {
		pageLimit = 10
	}
	delay, _ := cmd.Flags().GetInt("page-delay")

	walking := map[string]string{}
	for k, v := range params {
		walking[k] = v
	}
	walking["limit"] = strconv.Itoa(pageSize)

	for page := 0; page < pageLimit; page++ {
		// Per-scheme — pick one based on design.md:
		walking["offset"] = strconv.Itoa(page * pageSize) // OFFSET-LIMIT
		// walking["page"]   = strconv.Itoa(page + 1)     // PAGE-NUMBER
		// walking["cursor"] = nextCursor                 // CURSOR (read from prev response)

		resp, err := c.DoWithContext(cmd.Context(), "GET", path, walking, nil)
		if err != nil {
			return err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return fmt.Errorf("reading page %d: %w", page, readErr)
		}
		if _, err := os.Stdout.Write(append(body, '\n')); err != nil {
			return err
		}
		count, ok := pageItemCount(body)
		if !ok || count < pageSize {
			return nil
		}
		if delay > 0 && page+1 < pageLimit {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
	return nil
}

// pageItemCount returns the length of the items array for common pageable
// shapes ({content|rows|items|data: [...]}) or for a bare top-level array.
// Returns ok=false when the shape is unrecognised — callers stop walking
// conservatively.
func pageItemCount(body []byte) (int, bool) {
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, false
	}
	switch val := v.(type) {
	case []any:
		return len(val), true
	case map[string]any:
		for _, k := range []string{"content", "rows", "items", "data"} {
			if arr, ok := val[k].([]any); ok {
				return len(arr), true
			}
		}
	}
	return 0, false
}
