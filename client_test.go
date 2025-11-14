package paprika

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errReadCloser struct {
	err error
}

func (e errReadCloser) Read([]byte) (int, error) {
	return 0, e.err
}

func (e errReadCloser) Close() error {
	return nil
}

func TestNewClientUsesDefaultURL(t *testing.T) {
	c, err := NewClient("user", "pass")
	require.NoError(t, err)
	require.NotNil(t, c.baseURL)
	assert.Equal(t, "user", c.username)
	assert.Equal(t, "pass", c.password)
	assert.Equal(t, DefaultBaseURL, c.baseURL.String())
}

func TestNewClientWithURLValidatesCredentials(t *testing.T) {
	baseURL, err := url.Parse("https://example.com/api/")
	require.NoError(t, err)

	_, err = NewClientWithURL("   ", "secret", baseURL)
	require.EqualError(t, err, "username must not be empty")

	_, err = NewClientWithURL("user", "   ", baseURL)
	require.EqualError(t, err, "password must not be empty")
}

func TestPrepareGetBuildsRequest(t *testing.T) {
	baseURL, err := url.Parse("https://example.com/api/")
	require.NoError(t, err)
	c, err := NewClientWithURL("user", "pass", baseURL)
	require.NoError(t, err)

	req, err := c.prepareGet(context.Background(), "recipes", "123")
	require.NoError(t, err)

	assert.Equal(t, http.MethodGet, req.Method)
	assert.Equal(t, "https://example.com/api/recipes/123", req.URL.String())
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	username, password, ok := req.BasicAuth()
	require.True(t, ok)
	assert.Equal(t, "user", username)
	assert.Equal(t, "pass", password)
}

func TestClientRequestBuildersUseCorrectPaths(t *testing.T) {
	baseURL, err := url.Parse("https://example.com/api/")
	require.NoError(t, err)
	c, err := NewClientWithURL("user", "pass", baseURL)
	require.NoError(t, err)

	ctx := context.Background()
	tests := []struct {
		name     string
		builder  func() (*http.Request, error)
		wantPath string
	}{
		{
			name:     "recipes",
			builder:  func() (*http.Request, error) { return c.RecipesRequest(ctx) },
			wantPath: "/api/recipes",
		},
		{
			name:     "recipe",
			builder:  func() (*http.Request, error) { return c.RecipeRequest(ctx, "abc") },
			wantPath: "/api/recipe/abc",
		},
		{
			name:     "bookmarks",
			builder:  func() (*http.Request, error) { return c.BookmarksRequest(ctx) },
			wantPath: "/api/bookmarks",
		},
		{
			name:     "categories",
			builder:  func() (*http.Request, error) { return c.CategoriesRequest(ctx) },
			wantPath: "/api/categories",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := tt.builder()
			require.NoError(t, err)
			assert.Equal(t, tt.wantPath, req.URL.Path)
		})
	}
}

func TestClientEndpointMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		require.True(t, ok)
		assert.Equal(t, "user", username)
		assert.Equal(t, "pass", password)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, http.MethodGet, r.Method)

		switch r.URL.Path {
		case "/recipes":
			fmt.Fprint(w, `{"result":[{"uid":"r1"}]}`)
		case "/recipe/abc":
			fmt.Fprint(w, `{"result":{"uid":"abc","name":"Soup"}}`)
		case "/bookmarks":
			fmt.Fprint(w, `{"result":[{"uid":"b1","title":"Bookmark"}]}`)
		case "/categories":
			fmt.Fprint(w, `{"result":[{"uid":"c1","name":"Category"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL, err := url.Parse(server.URL + "/")
	require.NoError(t, err)

	c, err := NewClientWithURL("user", "pass", baseURL)
	require.NoError(t, err)
	c.httpClient = *server.Client()

	ctx := context.Background()

	recipes, err := c.Recipes(ctx)
	require.NoError(t, err)
	assert.Equal(t, []RecipeItem{{UID: "r1"}}, recipes)

	recipe, err := c.Recipe(ctx, "abc")
	require.NoError(t, err)
	assert.Equal(t, Recipe{UID: "abc", Name: "Soup"}, recipe)

	bookmarks, err := c.Bookmarks(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Bookmark{{UID: "b1", Title: "Bookmark"}}, bookmarks)

	categories, err := c.Categories(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Category{{UID: "c1", Name: "Category"}}, categories)
}

func TestDoRequestHTTPError(t *testing.T) {
	expectedErr := errors.New("network down")
	c := &Client{
		httpClient: http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, expectedErr
			}),
		},
	}

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	err = c.DoRequest(req, &struct{}{})
	require.Error(t, err)
	assert.ErrorIs(t, err, expectedErr)
	assert.Contains(t, err.Error(), "failed to GET http://example.com")
}

func TestDoRequestStatusError(t *testing.T) {
	c := &Client{
		httpClient: http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Status:     "502 Bad Gateway",
					Body:       io.NopCloser(strings.NewReader("bad upstream")),
				}, nil
			}),
		},
	}

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	err = c.DoRequest(req, &struct{}{})
	require.EqualError(t, err, "unexpected status code: 502 Bad Gateway bad upstream")
}

func TestDoRequestBodyReadError(t *testing.T) {
	bodyErr := errors.New("read failure")
	c := &Client{
		httpClient: http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Status:     "200 OK",
					Body:       errReadCloser{err: bodyErr},
				}, nil
			}),
		},
	}

	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	err = c.DoRequest(req, &struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error reading response body")
	assert.ErrorIs(t, err, bodyErr)
}

func TestUnmarshalWrappedResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(`{"result":{"uid":"abc"}}`)),
	}

	var recipe Recipe
	c := &Client{}
	err := c.UnmarshalWrappedResponse(resp, &recipe)
	require.NoError(t, err)
	assert.Equal(t, Recipe{UID: "abc"}, recipe)
}

func TestUnmarshalWrappedResponseStatusError(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       io.NopCloser(strings.NewReader("boom")),
	}

	c := &Client{}
	err := c.UnmarshalWrappedResponse(resp, &Recipe{})
	require.EqualError(t, err, "unexpected status code: 500 Internal Server Error boom")
}

func TestUnmarshalWrappedResponseReadError(t *testing.T) {
	bodyErr := errors.New("broken pipe")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       errReadCloser{err: bodyErr},
	}

	c := &Client{}
	err := c.UnmarshalWrappedResponse(resp, &Recipe{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error reading response body")
	assert.Contains(t, err.Error(), bodyErr.Error())
}

func TestUnwrapResultSuccess(t *testing.T) {
	data := []byte(`{"result":{"uid":"xyz"}}`)
	var recipe Recipe
	err := UnwrapResult(data, &recipe)
	require.NoError(t, err)
	assert.Equal(t, Recipe{UID: "xyz"}, recipe)
}

func TestUnwrapResultInvalidJSON(t *testing.T) {
	err := UnwrapResult([]byte("not-json"), &Recipe{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal result wrapper")
}

func TestUnwrapResultTargetUnmarshalFailure(t *testing.T) {
	data := []byte(`{"result":{"value":"not-an-int"}}`)
	var target struct {
		Value int `json:"value"`
	}

	err := UnwrapResult(data, &target)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal result from {\"value\":\"not-an-int\"}")
}
