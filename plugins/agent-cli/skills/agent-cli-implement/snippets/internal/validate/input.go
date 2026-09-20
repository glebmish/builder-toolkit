// Snippet: internal/validate/input.go — input hardening.
//
// Validators return *cliexit.ValidationError directly so every callsite
// propagates the right exit code (3) automatically. Callers don't need
// to remember to wrap.
//
// Add domain validators (server-quirk catchers) alongside these — same
// vErr helper.
package validate

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/example/acme-cli/internal/cliexit"
)

func vErr(format string, args ...any) error {
	return &cliexit.ValidationError{Err: fmt.Errorf(format, args...)}
}

// PathParam validates that a value is safe to embed in a URL path segment.
func PathParam(name, value string) error {
	if value == "" {
		return vErr("%s is required", name)
	}
	if strings.Contains(value, "..") {
		return vErr("%s contains path traversal", name)
	}
	if strings.ContainsAny(value, "?#&") {
		return vErr("%s must not contain ?, #, or & (query injection)", name)
	}
	if strings.Contains(value, "%") {
		return vErr("%s must not be pre-URL-encoded", name)
	}
	for _, r := range value {
		if r < 0x20 {
			return vErr("%s contains control characters", name)
		}
	}
	return nil
}

// IntParam validates that a string parses as a Go int. Allows negatives.
func IntParam(name, value string) error {
	if value == "" {
		return vErr("%s is required", name)
	}
	if _, err := strconv.Atoi(value); err != nil {
		return vErr("%s: expected integer, got %q", name, value)
	}
	return nil
}

// DateParam accepts ISO YYYY-MM-DD only.
func DateParam(name, value string) error {
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return vErr("%s: invalid date %q (expected YYYY-MM-DD)", name, value)
	}
	return nil
}

// JSONBody ensures the body is syntactically valid JSON without rogue
// control characters that would break the request mid-line.
func JSONBody(body string) error {
	if body == "" {
		return vErr("empty JSON body")
	}
	for i, r := range body {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return vErr("JSON body contains control character at position %d", i)
		}
	}
	if !json.Valid([]byte(body)) {
		return vErr("invalid JSON body")
	}
	return nil
}
