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

func (c *Client) WriteComment(id int64, comment string) error {
	payload := rm.IssueUpdateObject{
		Notes: &comment,
	}
	code, err := c.api.IssueUpdate(id, rm.IssueUpdate{
		Issue: payload,
	})
	if code == 403 {
		return fmt.Errorf("access forbidden on %d: %d", id, code)
	}
	if code != 204 {
		return fmt.Errorf("unexpected code on %d: %d", id, code)
	}
	if err != nil {
		return fmt.Errorf("error commenting issue %d: %s", id, err)
	}

	return nil
}

func (c *Client) GetIssue(id int64) (*rm.IssueObject, error) {
	i, code, err := c.api.IssueSingleGet(id, rm.IssueSingleGetRequest{})
	if code == 403 {
		return nil, fmt.Errorf("access forbidden on %d: %d", id, code)
	}
	if code != 200 {
		return nil, fmt.Errorf("unexpected code on %d: %d", id, code)
	}
	if err != nil {
		return nil, fmt.Errorf("error getting issue %d: %s", id, err)
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
