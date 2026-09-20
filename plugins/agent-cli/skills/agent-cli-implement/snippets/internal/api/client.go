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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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
		httpClient:        &http.Client{Timeout: 60 * time.Second},
	}
}

func WithContext(ctx context.Context, c *Client) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

func FromContext(ctx context.Context) *Client {
	c, _ := ctx.Value(contextKey{}).(*Client)
	return c
}

func (c *Client) Do(method, pathTemplate string, params map[string]string, body []byte) (*http.Response, error) {
	return c.DoWithContext(context.Background(), method, pathTemplate, params, body)
}

func (c *Client) DoWithContext(ctx context.Context, method, pathTemplate string, params map[string]string, body []byte) (*http.Response, error) {
	rawURL := c.buildURL(pathTemplate, params)
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
func (c *Client) DryRun(method, pathTemplate string, params map[string]string, body []byte) string {
	rawURL := c.buildURL(pathTemplate, params)
	out := fmt.Sprintf("%s %s", method, rawURL)
	if len(body) > 0 {
		preview := string(body)
		if len(preview) > 500 {
			preview = preview[:500] + "...(truncated)"
		}
		out += "\nbody: " + preview
	}
	return out
}

func (c *Client) buildURL(pathTemplate string, params map[string]string) string {
	path := pathTemplate
	if c.tenantPlaceholder != "" {
		path = strings.ReplaceAll(path, c.tenantPlaceholder, c.tenantID)
	}
	remaining := map[string]string{}
	for k, v := range params {
		ph := "{" + k + "}"
		if strings.Contains(path, ph) {
			path = strings.ReplaceAll(path, ph, v)
		} else {
			remaining[k] = v
		}
	}
	rawURL := c.baseURL + path
	if len(remaining) > 0 {
		q := url.Values{}
		for k, v := range remaining {
			q.Set(k, v)
		}
		rawURL += "?" + q.Encode()
	}
	return rawURL
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
	msg := fmt.Sprintf("API error %d %s: %s %s", e.StatusCode, http.StatusText(e.StatusCode), e.Method, e.Path)
	if e.Body != "" {
		msg += ": " + e.Body
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

func (c *Client) classifyError(resp *http.Response, method, path string) error {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
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
// Implementation omitted here; see the running CLIs (intelinvest's CSV
// import) for a tested version. ~30 LOC: build mime/multipart writer, write
// each FileUpload as a form-file, set Content-Type to mw.FormDataContentType().

// helper omitted to keep the snippet a single-file paste; copy from a
// reference implementation when uploads are required.
var _ = json.Valid
