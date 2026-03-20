package telegram

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type errDoer struct {
	err error
}

func (d *errDoer) Do(*http.Request) (*http.Response, error) {
	return nil, d.err
}

type nilDoer struct{}

func (*nilDoer) Do(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestDoRequest_invalidBaseURL(t *testing.T) {
	client := New("test-token")
	client.baseURL = "ftp://api.telegram.org"

	_, err := client.doRequest(context.Background(), http.MethodGet, "/bot/getUpdates")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheme must be http or https")
}

func TestDoRequest_networkError(t *testing.T) {
	client := New("test-token")
	client.SetHTTPDoer(&errDoer{err: fmt.Errorf("connection refused")})

	_, err := client.doRequest(context.Background(), http.MethodGet, "/bot/getUpdates")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requesting Telegram")
}

func TestDoRequest_nilResponse(t *testing.T) {
	client := New("test-token")
	client.SetHTTPDoer(&nilDoer{})

	_, err := client.doRequest(context.Background(), http.MethodGet, "/bot/getUpdates")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestDoRequest_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprint(w, `{"ok":false,"description":"Unauthorized"}`)
	}))
	defer srv.Close()

	client := New("bad-token")
	client.baseURL = srv.URL
	_, err := client.doRequest(context.Background(), http.MethodGet, "/botbad-token/getUpdates")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returned 401")
	assert.NotContains(t, err.Error(), "bad-token", "token should be redacted in error messages")
}

func TestGetUpdates_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Contains(t, r.URL.Path, "/bottest-token/getUpdates")
		_, _ = fmt.Fprint(w, `{
			"ok": true,
			"result": [
				{
					"update_id": 100,
					"message": {
						"message_id": 1,
						"from": {"id": 42, "is_bot": false, "first_name": "John", "last_name": "Doe", "username": "johndoe"},
						"chat": {"id": 42, "type": "private"},
						"date": 1700000000,
						"text": "Hello bot"
					}
				},
				{
					"update_id": 101,
					"message": {
						"message_id": 2,
						"from": {"id": 43, "is_bot": false, "first_name": "Jane"},
						"chat": {"id": -100, "type": "group", "title": "Team Chat"},
						"date": 1700000060,
						"text": "Hi there"
					}
				}
			]
		}`)
	}))
	defer srv.Close()

	client := New("test-token")
	client.baseURL = srv.URL
	updates, err := client.GetUpdates(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, updates, 2)

	assert.Equal(t, 100, updates[0].UpdateID)
	assert.Equal(t, "Hello bot", updates[0].Message.Text)
	assert.Equal(t, "John", updates[0].Message.From.FirstName)
	assert.Equal(t, "Doe", updates[0].Message.From.LastName)

	assert.Equal(t, 101, updates[1].UpdateID)
	assert.Equal(t, "Hi there", updates[1].Message.Text)
	assert.Equal(t, "Team Chat", updates[1].Message.Chat.Title)
}

func TestGetUpdates_empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok": true, "result": []}`)
	}))
	defer srv.Close()

	client := New("test-token")
	client.baseURL = srv.URL
	updates, err := client.GetUpdates(context.Background(), 100)
	require.NoError(t, err)
	assert.Empty(t, updates)
}

func TestGetUpdates_apiError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok": false, "description": "Not Found: bot token is invalid"}`)
	}))
	defer srv.Close()

	client := New("test-token")
	client.baseURL = srv.URL
	_, err := client.GetUpdates(context.Background(), 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bot token is invalid")
}

func TestGetUpdate_happy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{
			"ok": true,
			"result": [
				{
					"update_id": 100,
					"message": {"message_id": 1, "from": {"id": 42, "is_bot": false, "first_name": "John"}, "chat": {"id": 42, "type": "private"}, "date": 1700000000, "text": "Hello"}
				},
				{
					"update_id": 101,
					"message": {"message_id": 2, "from": {"id": 43, "is_bot": false, "first_name": "Jane"}, "chat": {"id": 43, "type": "private"}, "date": 1700000060, "text": "World"}
				}
			]
		}`)
	}))
	defer srv.Close()

	client := New("test-token")
	client.baseURL = srv.URL
	update, err := client.GetUpdate(context.Background(), 101)
	require.NoError(t, err)
	assert.Equal(t, 101, update.UpdateID)
	assert.Equal(t, "World", update.Message.Text)
}

func TestGetUpdate_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{
			"ok": true,
			"result": [
				{
					"update_id": 100,
					"message": {"message_id": 1, "from": {"id": 42, "is_bot": false, "first_name": "John"}, "chat": {"id": 42, "type": "private"}, "date": 1700000000, "text": "Hello"}
				}
			]
		}`)
	}))
	defer srv.Close()

	client := New("test-token")
	client.baseURL = srv.URL
	_, err := client.GetUpdate(context.Background(), 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update 999 not found")
}

func TestSanitizeTokenInPath(t *testing.T) {
	assert.Equal(t, "/bot<REDACTED>/getUpdates", sanitizeTokenInPath("/bot123:ABC/getUpdates", "123:ABC"))
	assert.Equal(t, "/bot/getUpdates", sanitizeTokenInPath("/bot/getUpdates", ""))
}
