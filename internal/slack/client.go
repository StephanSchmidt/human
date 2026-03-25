package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/StephanSchmidt/human/errors"
	"github.com/StephanSchmidt/human/internal/apiclient"
)

// Client is a Slack Bot API client.
type Client struct {
	api     *apiclient.Client
	channel string
}

// New creates a Slack client with the given bot token and channel.
func New(token, channel string) *Client {
	return newWithBaseURL("https://slack.com/api", token, channel)
}

// newWithBaseURL creates a Slack client with a custom base URL (for testing).
func newWithBaseURL(baseURL, token, channel string) *Client {
	return &Client{
		api: apiclient.New(baseURL,
			apiclient.WithAuth(apiclient.BearerToken(token)),
			apiclient.WithProviderName("slack"),
		),
		channel: channel,
	}
}

// SetHTTPDoer replaces the HTTP client used for API requests.
func (c *Client) SetHTTPDoer(doer apiclient.HTTPDoer) {
	c.api.SetHTTPDoer(doer)
}

// SendMessage sends a text message to the configured Slack channel.
func (c *Client) SendMessage(ctx context.Context, text string) error {
	body, err := json.Marshal(struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
	}{Channel: c.channel, Text: text})
	if err != nil {
		return errors.WrapWithDetails(err, "marshaling Slack postMessage request")
	}
	resp, err := c.api.Do(ctx, http.MethodPost, "/chat.postMessage", "", bytes.NewReader(body))
	if err != nil {
		return err
	}
	var result slackResponse
	if err := apiclient.DecodeJSON(resp, &result); err != nil {
		return err
	}
	if !result.OK {
		return errors.WithDetails(
			fmt.Sprintf("Slack chat.postMessage error: %s", result.Error),
			"channel", c.channel)
	}
	return nil
}

// ListMessages returns recent messages from the configured Slack channel.
func (c *Client) ListMessages(ctx context.Context, limit int) ([]MessageSummary, error) {
	query := fmt.Sprintf("channel=%s&limit=%d", c.channel, limit)
	resp, err := c.api.Do(ctx, http.MethodGet, "/conversations.history", query, nil)
	if err != nil {
		return nil, err
	}
	var result historyResponse
	if err := apiclient.DecodeJSON(resp, &result); err != nil {
		return nil, err
	}
	if !result.OK {
		return nil, errors.WithDetails(
			fmt.Sprintf("Slack conversations.history error: %s", result.Error),
			"channel", c.channel)
	}
	summaries := make([]MessageSummary, len(result.Messages))
	for i, m := range result.Messages {
		summaries[i] = MessageSummary{
			User: m.User,
			Text: m.Text,
			TS:   m.TS,
		}
	}
	return summaries, nil
}
