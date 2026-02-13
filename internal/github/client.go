package github

import (
	"context"
	"fmt"

	gh "github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

// Client wraps the go-github client with token auth and caches the authenticated username.
type Client struct {
	gh       *gh.Client
	username string
}

// NewClient creates an authenticated GitHub client and fetches the current user.
func NewClient(token string) (*Client, error) {
	ctx := context.Background()
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(ctx, ts)
	client := gh.NewClient(tc)

	user, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get authenticated user: %w", err)
	}

	return &Client{
		gh:       client,
		username: user.GetLogin(),
	}, nil
}

// GetUsername returns the login of the authenticated user.
func (c *Client) GetUsername() string {
	return c.username
}
