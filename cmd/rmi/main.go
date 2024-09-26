package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/b1tray3r/go/internal/redmine"
	md "github.com/nao1215/markdown"
)

func main() {
	param := os.Args[1]
	id, err := strconv.ParseInt(param, 10, 64)
	if err != nil {
		fmt.Errorf("you provided a parameter which can not be converted to int64.")
	}

	URL, ok := os.LookupEnv("RMI_URL")
	if !ok || URL == "" {
		fmt.Errorf("RMI_URL is not defined in your environment.")
	}

	KEY, ok := os.LookupEnv("RMI_KEY")
	if !ok || KEY == "" {
		fmt.Errorf("RMI_KEY is not defined in your environment.")
	}

	rmc, err := redmine.NewClient(URL, KEY, "#")
	if err != nil {
		fmt.Errorf("%v", err)
	}

	i, err := rmc.GetIssue(id)
	if err != nil {
		fmt.Errorf("%v", err)
	}

	pn := strings.ReplaceAll(i.Project.Name, "-", "_")

	md.NewMarkdown(os.Stdout).
		H1(i.Subject).
		HorizontalRule().
		BlueBadgef("Project-%s", url.QueryEscape(pn)).
		GreenBadgef("Issue-%d", i.ID).
		YellowBadgef("Reporter-%s", url.QueryEscape(i.Author.Name)).
		HorizontalRule().
		PlainText(i.Description).
		Build()
}
