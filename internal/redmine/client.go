package redmine

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	redmine "github.com/nixys/nxs-go-redmine/v5"
	rm "github.com/nixys/nxs-go-redmine/v5"
	"github.com/sanity-io/litter"
)

type Client struct {
	APIKey string
	URL    string
	Prefix string

	Dry bool

	api *rm.Context
}

func (c *Client) getIssueID(issueIDs []string) (int64, error) {
	for _, ID := range issueIDs {
		if ID[:len(c.Prefix)] == c.Prefix {
			ID = ID[len(c.Prefix):]

			issueID, err := strconv.ParseInt(ID, 10, 64)
			if err != nil {
				return 0, err
			}

			return issueID, nil
		}
	}

	return 0, nil
}

func (c *Client) GetActivityID(projectID, activityName string) (int64, error) {
	project, code, err := c.api.ProjectSingleGet(
		projectID,
		redmine.ProjectSingleGetRequest{
			Includes: []redmine.ProjectInclude{redmine.ProjectIncludeTimeEntryActivities},
		},
	)
	if err != nil {
		return 0, fmt.Errorf("error getting project %s: %w", projectID, err)
	}
	if code != http.StatusOK {
		return 0, fmt.Errorf("error getting project %s: %d", projectID, code)
	}

	for _, activity := range *project.TimeEntryActivities {
		if strings.Contains(activity.Name, activityName) {
			return activity.ID, nil
		}
	}

	return 0, fmt.Errorf("activity %s not found in project %s", activityName, projectID)
}

type TimeEntry struct {
	ID         string
	IssueIDs   []string
	Start      time.Time
	End        time.Time
	Duration   float64
	Hours      time.Duration
	Tags       []string
	Comment    string
	ActivityID string
	errors     []string
	IsRedmine  bool
	IsJira     bool
}

func (c *Client) Log(te TimeEntry) error {
	ID, err := c.getIssueID(te.IssueIDs)
	if err != nil {
		return err
	}

	issueID := int64(ID)

	AID := te.ActivityID
	activityID, err := strconv.ParseInt(AID, 10, 64)
	if err != nil {
		return err
	}

	date := te.Start.Format("2006-01-02")

	teo := redmine.TimeEntryCreateObject{
		IssueID:    &issueID,
		ActivityID: activityID,
		Hours:      te.Duration,
		SpentOn:    &date,
		Comments:   te.Comment,
	}

	if c.Dry {
		litter.Dump(teo)
		return nil
	}

	log.Print("sending ... ")
	_, code, err := c.api.TimeEntryCreate(
		redmine.TimeEntryCreate{
			TimeEntry: teo,
		},
	)
	if err != nil {
		return err
	}
	if code != http.StatusCreated {
		return fmt.Errorf("could not log time entry")
	}

	log.Println("seemed ok with code %d", code)

	return nil
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

func NewClient(URL, key, prefix string, dry bool) (*Client, error) {
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
		Dry:    dry,
		api:    api,
	}, nil
}
