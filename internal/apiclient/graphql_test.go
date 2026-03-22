package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoGraphQL_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/graphql", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"viewer":{"id":"user-1"}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, WithProviderName("test"))
	data, err := c.DoGraphQL(context.Background(), "/graphql", `{ viewer { id } }`, nil)
	require.NoError(t, err)

	var result struct {
		Viewer struct {
			ID string `json:"id"`
		} `json:"viewer"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	assert.Equal(t, "user-1", result.Viewer.ID)
}

func TestDoGraphQL_graphqlError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":null,"errors":[{"message":"not found"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL, WithProviderName("linear"))
	_, err := c.DoGraphQL(context.Background(), "/graphql", `{ issue(id:"X") { id } }`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "linear graphql error")
	assert.Contains(t, err.Error(), "not found")
}

func TestDoGraphQL_networkError(t *testing.T) {
	c := New("https://example.com",
		WithProviderName("test"),
		WithHTTPDoer(&errDoer{err: fmt.Errorf("network down")}),
	)
	_, err := c.DoGraphQL(context.Background(), "/graphql", `{ viewer { id } }`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requesting test")
}

func TestDoGraphQL_nilResponse(t *testing.T) {
	c := New("https://example.com",
		WithProviderName("test"),
		WithHTTPDoer(&nilDoer{}),
	)
	_, err := c.DoGraphQL(context.Background(), "/graphql", `{ viewer { id } }`, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response")
}

func TestDoGraphQL_withVariables(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req GraphQLRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "ENG-1", req.Variables["id"])
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"issue":{"id":"123"}}}`))
	}))
	defer srv.Close()

	c := New(srv.URL, WithProviderName("test"))
	data, err := c.DoGraphQL(context.Background(), "/graphql",
		`query($id: String!) { issue(id: $id) { id } }`,
		map[string]any{"id": "ENG-1"},
	)
	require.NoError(t, err)
	assert.NotNil(t, data)
}
