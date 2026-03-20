package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/tracker"
)

// Client is a Telegram Bot API client.
type Client struct {
	baseURL string
	token   string
	http    tracker.HTTPDoer
}

// New creates a Telegram client with the given bot token.
func New(token string) *Client {
	return &Client{
		baseURL: "https://api.telegram.org",
		token:   token,
		http:    http.DefaultClient,
	}
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer tracker.HTTPDoer) {
	c.http = doer
}

// GetUpdates fetches pending updates from the Telegram Bot API.
// It does not pass an offset, so this is a read-only peek at pending messages.
func (c *Client) GetUpdates(ctx context.Context, limit int) ([]Update, error) {
	path := fmt.Sprintf("/bot%s/getUpdates?limit=%d&allowed_updates=[\"message\"]", c.token, limit)
	resp, err := c.doRequest(ctx, http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var result getUpdatesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, errors.WrapWithDetails(err, "decoding Telegram response")
	}
	if !result.OK {
		return nil, errors.WithDetails(
			fmt.Sprintf("Telegram API error: %s", result.Description))
	}
	return result.Result, nil
}

// GetUpdate fetches all pending updates and returns the one matching updateID.
// Returns an error if the update is not found among pending updates.
func (c *Client) GetUpdate(ctx context.Context, updateID int) (*Update, error) {
	updates, err := c.GetUpdates(ctx, 100)
	if err != nil {
		return nil, err
	}
	for i := range updates {
		if updates[i].UpdateID == updateID {
			return &updates[i], nil
		}
	}
	return nil, errors.WithDetails(
		fmt.Sprintf("update %d not found in pending updates", updateID),
		"updateID", updateID)
}

func (c *Client) doRequest(ctx context.Context, method, path string) (*http.Response, error) {
	if err := tracker.ValidateURL(c.baseURL); err != nil {
		return nil, err
	}
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing base URL", "baseURL", c.baseURL)
	}

	parsedPath, err := url.Parse(path)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "parsing path")
	}
	u.Path = parsedPath.Path
	u.RawQuery = parsedPath.RawQuery

	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "creating request",
			"method", method)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.WrapWithDetails(err, "requesting Telegram",
			"method", method)
	}
	if resp == nil {
		return nil, errors.WithDetails("requesting Telegram: nil response",
			"method", method)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		// Sanitize token from path in error messages.
		sanitizedPath := sanitizeTokenInPath(path, c.token)
		return nil, errors.WithDetails(
			fmt.Sprintf("telegram %s %s returned %d: %s", method, sanitizedPath, resp.StatusCode, string(respBody)),
			"statusCode", resp.StatusCode, "method", method)
	}
	return resp, nil
}

// sanitizeTokenInPath replaces the bot token in a path with "bot<REDACTED>".
func sanitizeTokenInPath(path, token string) string {
	if token == "" {
		return path
	}
	return strings.ReplaceAll(path, token, "<REDACTED>")
}
