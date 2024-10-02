package main

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/b1tray3r/go/internal/redmine"
	md "github.com/nao1215/markdown"
)

func main() {
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

	if len(os.Args) == 1 {
		fmt.Fprintln(os.Stderr, "expected issue id not given as first param.")
		os.Exit(1)
	}

	rmc, err := redmine.NewClient(URL, KEY, "#")
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	param := os.Args[1]
	if param != "-c" {
		id, err := strconv.ParseInt(param, 10, 64)
		if err != nil {
			fmt.Fprintln(os.Stderr, "you provided a parameter which can not be converted to int64.")
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

		os.Exit(0)
	}

	commit_msg_file := os.Args[2]
	if _, err := os.Stat(commit_msg_file); err != nil && os.IsNotExist(err) {
		fmt.Println("commit message file not found!")
		os.Exit(0)
	}
	dat, err := os.ReadFile(commit_msg_file)
	if err != nil {
		panic(err)
	}
	commit := string(dat)

	r := regexp.MustCompile(`(?i)issue: *(\d+)`)
	match := r.FindStringSubmatch(commit)

	// Split the input string into lines
	lines := strings.Split(commit, "\n")
	var result []string
	for _, line := range lines {
		if !r.MatchString(line) {
			result = append(result, line)
		}
	}

	// Join the remaining lines back into a single string
	output := strings.Join(result, "\n")

	issueID, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		panic(err)
	}
	comment := `
Notiz: Dieser Kommentar wurde automatisch erzeugt, weil an diesem Ticket gearbeitet wurde:
--
%s
--
`

	if err := rmc.WriteComment(issueID, fmt.Sprintf(comment, output)); err != nil {
		panic(err)
	}
}
