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
	if len(os.Args) == 1 {
		fmt.Fprintln(os.Stderr, "expected issue id not given as first param.")
		os.Exit(1)
	}

	param := os.Args[1]
	id, err := strconv.ParseInt(param, 10, 64)
	if err != nil {
		fmt.Fprintln(os.Stderr, "you provided a parameter which can not be converted to int64.")
		os.Exit(1)
	}

	URL, ok := os.LookupEnv("RMI_URL")
	if !ok || URL == "" {
		fmt.Fprintln(os.Stderr, "RMI_URL is not defined in your environment.")
		os.Exit(1)
	}

	KEY, ok := os.LookupEnv("RMI_KEY")
	if !ok || KEY == "" {
		fmt.Fprintln(os.Stderr, "RMI_KEY is not defined in your environment.")
		os.Exit(1)
	}

	rmc, err := redmine.NewClient(URL, KEY, "#")
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	i, err := rmc.GetIssue(id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
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
