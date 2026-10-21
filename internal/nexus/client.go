// Package nexus is a minimal client for the Nexus REST API.
//
// It only covers what Restish cannot express (multipart upload, credential
// checks, small reads for task polling). Everything else is delegated to the
// embedded Restish so we never reimplement it.
package nexus

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/wendaoji/nexus-cli"
	"github.com/wendaoji/nexus-cli/internal/i18n"
	"github.com/wendaoji/nexus-cli/internal/specs"
)

// DefaultOrigin is the build-time default target, taken from
// config/default-origin.txt (embedded into the binary). config.Resolve may
// override it at run time.
var DefaultOrigin = strings.TrimSpace(nexuscli.DefaultOriginText)

// Client is a Nexus client authenticated with HTTP Basic.
type Client struct {
	Origin  string // e.g. http://host:8081
	APIRoot string // e.g. /service/rest
	User    string
	Pass    string
	HTTP    *http.Client
}

// New builds a client from an already-resolved origin and credentials.
func New(origin, user, pass string) (*Client, error) {
	root, err := specs.APIRoot()
	if err != nil {
		return nil, err
	}
	return &Client{
		Origin:  strings.TrimRight(origin, "/"),
		APIRoot: strings.TrimRight(root, "/"),
		User:    user,
		Pass:    pass,
		HTTP:    &http.Client{Timeout: 300 * time.Second},
	}, nil
}

// URL joins the full request URL. path must start with "/", e.g. "/v1/repositories".
func (c *Client) URL(path string) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.Origin + c.APIRoot + path
}

// Do performs one authenticated request. The caller closes resp.Body.
func (c *Client) Do(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.URL(path), body)
	if err != nil {
		return nil, err
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if c.User != "" || c.Pass != "" {
		req.SetBasicAuth(c.User, c.Pass)
	}
	return c.HTTP.Do(req)
}

// CheckResult describes the outcome of a credential check.
type CheckResult struct {
	Status   int
	Verbatim string
}

// Valid reports whether authentication succeeded. 403 means the credentials
// are fine but the account lacks permission.
func (r CheckResult) Valid() bool { return r.Status == 200 || r.Status == 403 }

// Check validates credentials with GET /v1/repositories:
// 401 = invalid, 403 = valid but no permission.
func (c *Client) Check(ctx context.Context) (CheckResult, error) {
	resp, err := c.Do(ctx, http.MethodGet, "/v1/repositories", nil, "")
	if err != nil {
		return CheckResult{}, fmt.Errorf("%s: %w", i18n.T("network error while validating credentials"), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return CheckResult{Status: resp.StatusCode, Verbatim: strings.TrimSpace(string(raw))}, nil
}

// ExplainCheck turns a check result into a human-readable sentence.
func ExplainCheck(r CheckResult) string {
	switch {
	case r.Status == 200:
		return i18n.T("Credentials valid (HTTP 200)")
	case r.Status == 403:
		return i18n.T("Credentials valid, but this account cannot list repositories (HTTP 403) - authenticated")
	case r.Status == 401:
		return i18n.T("Invalid credentials (HTTP 401): check the username and password/token passcode")
	case r.Status == 429:
		return i18n.T("Nexus is rate-limiting authentication (HTTP 429, too many failed attempts): wait a few minutes")
	default:
		return i18n.T("Check failed (HTTP %d)", r.Status)
	}
}
