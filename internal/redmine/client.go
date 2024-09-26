package redmine

import (
	"fmt"

	rm "github.com/nixys/nxs-go-redmine/v5"
)

type Client struct {
	APIKey string
	URL    string
	Prefix string

	api *rm.Context
}

func (c *Client) GetIssue(id int64) (*rm.IssueObject, error) {
	i, code, err := c.api.IssueSingleGet(id, rm.IssueSingleGetRequest{})
	if code == 403 {
		return nil, fmt.Errorf("Access forbidden on %d: %d", id, code)
	}
	if code != 200 {
		return nil, fmt.Errorf("Unexpected code on %d: %d", id, code)
	}
	if err != nil {
		return nil, fmt.Errorf("Error getting issue %d: %s", id, err)
	}

	return &i, nil
}

func NewClient(URL, key, prefix string) (*Client, error) {
	if URL == "" || key == "" {
		return nil, fmt.Errorf("failed to create new client: make sure to provide URL and key.")
	}

	api := rm.Init(
		rm.Settings{
			Endpoint: URL,
			APIKey:   key,
		},
	)

	return &Client{
		APIKey: key,
		URL:    URL,
		Prefix: prefix,
		api:    api,
	}, nil
}
