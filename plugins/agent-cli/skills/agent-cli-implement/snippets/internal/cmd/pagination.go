// Snippet: internal/cmd/pagination.go — the --page-all walk.
//
// Conditional: include only if docs/design.md says the API paginates.
// Kept in its own file precisely so "only if the API paginates" is a
// clean `rm` — when this lived in helpers.go, deleting the function left
// orphaned strconv/time imports and the package no longer compiled.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/example/acme-cli/internal/format"
	"github.com/spf13/cobra"
)

// Query-parameter names for the walk. Change these two to match the API
// (pageSize/per_page, skip/start_at, ...) — not just the offset line
// below. A wrong limit key is silently ignored by the server, the first
// page comes back short, and the walk stops after one page looking
// perfectly successful.
var (
	pageSizeParam   = "limit"
	pageOffsetParam = "offset"
)

// doPaginate walks an offset/limit GET endpoint with --page-all. Emits one
// sanitized JSON line per page on stdout. Stops when the page response's
// items array is shorter than --page-size, or when --page-limit is hit —
// and says so on stderr in both cases, because a silent stop is
// indistinguishable from "that was all the data".
//
// For page-number or cursor schemes, swap the per-scheme line at the
// marked spot and adjust the stop condition.
func doPaginate(cmd *cobra.Command, path string, params map[string]string, opts format.Options) error {
	c, err := client(cmd)
	if err != nil {
		return err
	}
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
	walking[pageSizeParam] = strconv.Itoa(pageSize)

	for page := 0; page < pageLimit; page++ {
		// Per-scheme — pick one based on design.md:
		walking[pageOffsetParam] = strconv.Itoa(page * pageSize) // OFFSET-LIMIT
		// walking["page"]   = strconv.Itoa(page + 1)     // PAGE-NUMBER
		// walking["cursor"] = nextCursor                 // CURSOR (read from prev response)

		resp, err := c.DoWithContext(cmd.Context(), "GET", path, walking, nil)
		if err != nil {
			return err
		}
		body, readErr := readBody(resp.Body, fmt.Sprintf("page %d", page))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		// Through format.WriteLine, not os.Stdout directly: this is the
		// highest-volume path and the one carrying the most third-party
		// data, so it is the last place to skip sanitization or --fields.
		if err := format.WriteLine(os.Stdout, body, opts); err != nil {
			return err
		}
		count, ok := pageItemCount(body)
		if !ok {
			fmt.Fprintf(os.Stderr, "warning: stopped after page %d — unrecognised page shape, cannot tell if more data follows\n", page+1)
			return nil
		}
		if count < pageSize {
			return nil
		}
		if page+1 == pageLimit {
			fmt.Fprintf(os.Stderr, "warning: stopped at --page-limit=%d; more data may remain (raise --page-limit)\n", pageLimit)
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
