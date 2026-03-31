package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/StephanSchmidt/human/errors"
)

// DefaultTimeout is the HTTP client timeout applied when no custom HTTPDoer is provided.
const DefaultTimeout = 30 * time.Second

// ValidateURL checks that rawURL is a valid HTTP(S) URL.
// This guards against SSRF by rejecting non-HTTP schemes.
func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return errors.WrapWithDetails(err, "invalid URL", "url", rawURL)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.WithDetails("URL scheme must be http or https", "url", rawURL, "scheme", u.Scheme)
	}
	if u.Host == "" {
		return errors.WithDetails("URL must have a host", "url", rawURL)
	}
	return nil
}

// ErrorFormatter formats an HTTP error response into an error value.
type ErrorFormatter func(providerName, method, path string, statusCode int, body []byte) error

// Client is a shared HTTP API client that handles URL construction,
// authentication, headers, and error handling.
// Client is not safe for concurrent modification. All configuration (including
// SetHTTPDoer) must be done before the first call to Do.
type Client struct {
	baseURL        string
	auth           AuthFunc
	urlBuilder     URLBuilder
	headers        map[string]string
	contentType    string // if set, always use this Content-Type; if empty, set "application/json" only when body != nil
	providerName   string
	errorFormatter ErrorFormatter
	http           HTTPDoer
	timeout        time.Duration
}

// Option configures a Client.
type Option func(*Client)

// New creates a new API client with the given base URL and options.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    baseURL,
		auth:       NoAuth(),
		urlBuilder: StandardURL(),
		headers:    make(map[string]string),
		timeout:    DefaultTimeout,
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: c.timeout}
	}
	return c
}

// WithAuth sets the authentication strategy.
func WithAuth(auth AuthFunc) Option {
	return func(c *Client) { c.auth = auth }
}

// WithURLBuilder sets the URL construction strategy.
func WithURLBuilder(ub URLBuilder) Option {
	return func(c *Client) { c.urlBuilder = ub }
}

// WithHeader adds a header to every request.
func WithHeader(name, value string) Option {
	return func(c *Client) { c.headers[name] = value }
}

// WithContentType sets a Content-Type header on every request, regardless of
// whether a body is present. When empty (the default), Content-Type is set to
// "application/json" only when a body is provided.
func WithContentType(ct string) Option {
	return func(c *Client) { c.contentType = ct }
}

// WithProviderName sets the provider name used in error messages.
func WithProviderName(name string) Option {
	return func(c *Client) { c.providerName = name }
}

// WithErrorFormatter sets a custom error formatter.
func WithErrorFormatter(ef ErrorFormatter) Option {
	return func(c *Client) { c.errorFormatter = ef }
}

// WithTimeout sets the HTTP client timeout. Only effective when no custom
// HTTPDoer is provided via WithHTTPDoer.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithHTTPDoer sets the HTTP client used for requests.
func WithHTTPDoer(doer HTTPDoer) Option {
	return func(c *Client) { c.http = doer }
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer HTTPDoer) {
	c.http = doer
}

// Do executes an HTTP request with the client's configuration.
func (c *Client) Do(ctx context.Context, method, path, rawQuery string, body io.Reader) (*http.Response, error) {
	return c.doExec(ctx, method, path, rawQuery, body, "")
}

// DoWithContentType executes an HTTP request with an explicit Content-Type,
// overriding the client's default for this single request.
func (c *Client) DoWithContentType(ctx context.Context, method, path, rawQuery string, body io.Reader, contentType string) (*http.Response, error) {
	return c.doExec(ctx, method, path, rawQuery, body, contentType)
}

func (c *Client) doExec(ctx context.Context, method, path, rawQuery string, body io.Reader, contentTypeOverride string) (*http.Response, error) {
	if err := ValidateURL(c.baseURL); err != nil {
		return nil, err
	}

	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing base URL", "baseURL", c.baseURL)
	}

	fullURL, err := c.urlBuilder(base, path, rawQuery)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating request",
			"method", method, "path", path)
	}

	c.auth(req)

	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	// Determine Content-Type.
	ct := contentTypeOverride
	if ct == "" {
		ct = c.contentType
	}
	if ct != "" {
		req.Header.Set("Content-Type", ct)
	} else if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, fmt.Sprintf("requesting %s", c.displayName()),
			"method", method, "path", path)
	}
	if resp == nil {
		return nil, errors.WithDetails(fmt.Sprintf("requesting %s: nil response", c.displayName()),
			"method", method, "path", path)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		if c.errorFormatter != nil {
			return nil, c.errorFormatter(c.providerName, method, path, resp.StatusCode, respBody)
		}
		return nil, errors.WithDetails(
			fmt.Sprintf("%s %s %s returned %d: %s", c.displayName(), method, path, resp.StatusCode, string(respBody)),
			"statusCode", resp.StatusCode, "method", method, "path", path)
	}
	return resp, nil
}

func (c *Client) displayName() string {
	if c.providerName != "" {
		return c.providerName
	}
	return "api"
}

// DecodeJSON reads and decodes a JSON response body into dest, then closes the
// body. The context args are passed to errors.WrapWithDetails on decode failure.
func DecodeJSON(resp *http.Response, dest interface{}, contextArgs ...interface{}) error {
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.WrapWithDetails(err, "reading response body", contextArgs...)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return errors.WithDetails(
			fmt.Sprintf("decoding response: %s (body: %s)", err, snippet),
			contextArgs...)
	}
	return nil
}
