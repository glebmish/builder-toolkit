// Snippet: internal/api/client.go — hand-written HTTP client.
//
// Intentionally minimal: net/http + encoding/json. No HTTP framework,
// no struct generation. Path templates use disambiguated placeholders
// (e.g. {accountId}, {projectId}) — the client substitutes the configured
// tenant placeholder, then walks `params` for the rest. Leftover params
// become the query string.
//
// Bearer is shown; swap to req.SetBasicAuth("API_KEY", c.token) for Basic.
package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/example/acme-cli/internal/cliexit"
	"github.com/example/acme-cli/internal/format"
)

// maxErrorBody caps how much of a 4xx/5xx body is echoed back. The body
// lands in the calling agent's context via stderr, so an unbounded read
// of an HTML error page is a context-window denial of service.
const maxErrorBody = 8 << 10

// unresolvedPlaceholder finds any {name} left in a path after substitution.
var unresolvedPlaceholder = regexp.MustCompile(`\{[A-Za-z0-9_]+\}`)

type contextKey struct{}

type Client struct {
	baseURL           string
	token             string
	tenantID          string
	tenantPlaceholder string // e.g. "{accountId}"; "" if no tenant scope
	httpClient        *http.Client
}

func NewClient(baseURL, token, tenantID, tenantPlaceholder string) *Client {
	return &Client{
		baseURL:           baseURL,
		token:             token,
		tenantID:          tenantID,
		tenantPlaceholder: tenantPlaceholder,
		httpClient: &http.Client{
			Timeout:       60 * time.Second,
			CheckRedirect: refuseSchemeDowngrade,
		},
	}
}

// refuseSchemeDowngrade blocks an https→http redirect. Go's stdlib strips
// Authorization across hosts, but it compares host only — a same-host
// downgrade would forward the bearer token in cleartext.
func refuseSchemeDowngrade(req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return fmt.Errorf("refusing redirect from https to %s (would send the token in cleartext)", req.URL.Scheme)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func WithContext(ctx context.Context, c *Client) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// FromContext returns the client stashed by the root PersistentPreRunE.
// It returns a typed error rather than a nil client: a command that was
// wrongly classified as offline would otherwise nil-panic on first use,
// and Go exits 2 on an unrecovered panic — the code the contract reserves
// for auth failures, sending the agent off to re-authenticate.
func FromContext(ctx context.Context) (*Client, error) {
	c, _ := ctx.Value(contextKey{}).(*Client)
	if c == nil {
		return nil, &cliexit.AuthError{Err: fmt.Errorf("no API client configured for this command (it was treated as offline, or bootstrap did not run)")}
	}
	return c, nil
}

func (c *Client) DoWithContext(ctx context.Context, method, pathTemplate string, params map[string]string, body []byte) (*http.Response, error) {
	rawURL, err := c.buildURL(pathTemplate, params)
	if err != nil {
		return nil, err
	}
	var br io.Reader
	if len(body) > 0 {
		br = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, br)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, c.classifyError(resp, method, rawURL)
	}
	return resp, nil
}

// DryRun returns a human-readable preview without executing.
func (c *Client) DryRun(method, pathTemplate string, params map[string]string, body []byte) (string, error) {
	rawURL, err := c.buildURL(pathTemplate, params)
	if err != nil {
		return "", err
	}
	out := fmt.Sprintf("%s %s", method, rawURL)
	if len(body) > 0 {
		preview := string(body)
		if len(preview) > 500 {
			preview = preview[:500] + "...(truncated)"
		}
		out += "\nbody: " + preview
	}
	return out, nil
}

// buildURL substitutes the tenant placeholder and any params that match a
// {placeholder}; leftovers become the query string.
//
// Every substituted value is url.PathEscape'd here rather than at the
// callsites. Escaping at the chokepoint means correctness does not depend
// on each of dozens of generated commands remembering to validate first —
// a value like "42/secrets" can no longer address a sibling endpoint.
//
// Any {placeholder} still present after substitution is an error. Left
// alone it would be percent-escaped into the request path, producing a
// confusing 404 while the real key was quietly appended as a query param.
func (c *Client) buildURL(pathTemplate string, params map[string]string) (string, error) {
	path := pathTemplate
	if c.tenantPlaceholder != "" {
		path = strings.ReplaceAll(path, c.tenantPlaceholder, url.PathEscape(c.tenantID))
	}
	remaining := map[string]string{}
	for k, v := range params {
		ph := "{" + k + "}"
		if strings.Contains(path, ph) {
			path = strings.ReplaceAll(path, ph, url.PathEscape(v))
		} else {
			remaining[k] = v
		}
	}
	if m := unresolvedPlaceholder.FindString(path); m != "" {
		return "", fmt.Errorf("no value supplied for path placeholder %s in %s", m, pathTemplate)
	}
	rawURL := c.baseURL + path
	if len(remaining) > 0 {
		q := url.Values{}
		for k, v := range remaining {
			q.Set(k, v)
		}
		rawURL += "?" + q.Encode()
	}
	return rawURL, nil
}

// APIError is the error type returned for any 4xx/5xx response. See
// internal/cliexit for the typed wrappers used elsewhere.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	Body       string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("API error %d %s: %s %s", e.StatusCode, http.StatusText(e.StatusCode), e.Method, redactQuery(e.Path))
	if e.Body != "" {
		// The body is attacker-influenceable and lands in the agent's
		// context via stderr — same filter as stdout.
		msg += ": " + format.SanitizeText(e.Body)
	}
	switch e.StatusCode {
	case 401:
		msg += " (hint: check ACME_ACCESS_TOKEN — may be expired or wrong scope)"
	case 403:
		msg += " (hint: check the scopes/permissions on this token)"
	case 404:
		msg += " (hint: check the resource ID; run acme <resource> list)"
	case 409:
		msg += " (hint: concurrent modification; refetch and retry)"
	case 422:
		msg += " (hint: validation failed server-side; run with --dry-run to inspect the body)"
	case 429:
		msg += " (hint: rate limited; back off or use --page-delay)"
	case 500:
		msg += " (hint: usually a malformed request body; run with --dry-run to inspect)"
	case 503:
		msg += " (hint: transient; retry with backoff)"
	}
	return msg
}

// IsAuth reports whether this is an auth-class error main.go should map
// to exit code 2 instead of the generic 1.
func (e *APIError) IsAuth() bool { return e.StatusCode == 401 || e.StatusCode == 403 }

// redactQuery strips the query string, which can carry --params values.
func redactQuery(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i] + "?<redacted>"
	}
	return raw
}

func (c *Client) classifyError(resp *http.Response, method, path string) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return &APIError{
		StatusCode: resp.StatusCode,
		Method:     method,
		Path:       path,
		Body:       strings.TrimSpace(string(body)),
	}
}

// ---- Multipart upload (only if API accepts file uploads) -----------------

// FileUpload models one file for DoMultipart.
type FileUpload struct {
	Name string
	Data []byte
}

// DoMultipart POSTs files as multipart/form-data with the given field name.
// Conditional: include only if design.md says the API accepts uploads.
//
// Implementation omitted here. ~30 LOC: build a mime/multipart writer, write
// each FileUpload as a form-file, set Content-Type to mw.FormDataContentType().

// helper omitted to keep the snippet a single-file paste; copy from a
// reference implementation when uploads are required.
