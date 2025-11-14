package paprika

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const DefaultBaseURL = "https://www.paprikaapp.com/api/v1/sync/"

type Client struct {
	username   string
	password   string
	httpClient http.Client
	baseURL    *url.URL
}

func NewClient(username, password string) (*Client, error) {
	// Must parse DefaultBaseURL
	u, err := url.Parse(DefaultBaseURL)
	if err != nil {
		panic(err)
	}
	return NewClientWithURL(username, password, u)
}

func NewClientWithURL(username, password string, baseURL *url.URL) (*Client, error) {
	if strings.TrimSpace(username) == "" {
		return nil, fmt.Errorf("username must not be empty")
	}

	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("password must not be empty")
	}

	return &Client{
		httpClient: http.Client{},
		username:   username,
		password:   password,
		baseURL:    baseURL,
	}, nil
}

func (c *Client) Recipes(ctx context.Context) ([]RecipeItem, error) {
	rs := []RecipeItem{}
	req, err := c.RecipesRequest(ctx)
	if err != nil {
		return nil, err
	}
	err = c.DoRequest(req, &rs)
	return rs, err
}

func (c *Client) RecipesRequest(ctx context.Context) (*http.Request, error) {
	return c.prepareGet(ctx, "recipes")
}

func (c *Client) Recipe(ctx context.Context, uid string) (Recipe, error) {
	rs := Recipe{}
	req, err := c.RecipeRequest(ctx, uid)
	if err != nil {
		return rs, err
	}
	err = c.DoRequest(req, &rs)
	return rs, err
}

func (c *Client) RecipeRequest(ctx context.Context, uid string) (*http.Request, error) {
	return c.prepareGet(ctx, "recipe", uid)
}

func (c *Client) Bookmarks(ctx context.Context) ([]Bookmark, error) {
	rs := []Bookmark{}
	req, err := c.BookmarksRequest(ctx)
	if err != nil {
		return rs, err
	}
	err = c.DoRequest(req, &rs)
	return rs, err
}

func (c *Client) BookmarksRequest(ctx context.Context) (*http.Request, error) {
	return c.prepareGet(ctx, "bookmarks")
}

func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	rs := []Category{}
	req, err := c.CategoriesRequest(ctx)
	if err != nil {
		return rs, err
	}
	err = c.DoRequest(req, &rs)
	return rs, err
}

func (c *Client) CategoriesRequest(ctx context.Context) (*http.Request, error) {
	return c.prepareGet(ctx, "categories")
}

func (c *Client) UnmarshalWrappedResponse(resp *http.Response, target any) error {
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %s", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %s %s", resp.Status, bodyText)
	}

	err = UnwrapResult(bodyText, target)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) prepareGet(ctx context.Context, paths ...string) (*http.Request, error) {
	url := c.baseURL.JoinPath(paths...).String()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.SetBasicAuth(c.username, c.password)
	return req, nil
}

func (c *Client) DoRequest(req *http.Request, value any) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to %s %s: %w", req.Method, req.URL, err)
	}
	defer resp.Body.Close()

	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %s %s", resp.Status, bodyText)
	}

	err = UnwrapResult(bodyText, value)
	if err != nil {
		return err
	}

	return nil
}

func UnwrapResult(jsonData []byte, value interface{}) error {
	var wrapper Result

	err := json.Unmarshal(jsonData, &wrapper)
	if err != nil {
		return fmt.Errorf("failed to unmarshal result wrapper from %s: %s", string(jsonData), err)
	}
	unwrapped, err := wrapper.Result.MarshalJSON()
	if err != nil {
		return fmt.Errorf("failed to prepare result for unmarshal: %s", err)
	}
	err = json.Unmarshal(unwrapped, &value)
	if err != nil {
		return fmt.Errorf("failed to unmarshal result from %s: %s", string(unwrapped), err)
	}

	return nil
}
