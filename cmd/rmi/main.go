package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/b1tray3r/go/internal/redmine"
	md "github.com/nao1215/markdown"
	"github.com/sanity-io/litter"
	"github.com/spf13/viper"
)

func setupConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yml")

	home, err := os.UserHomeDir()
	if err != nil {
		panic(err)
	}
	viper.AddConfigPath(home + "/.config/rmi")

	// Read in environment variables that match
	viper.AutomaticEnv()
	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)

	// Read the config file
	if err := viper.ReadInConfig(); !os.IsNotExist(err) {
		if err != nil {
			panic(fmt.Errorf("fatal error config file: %s", err))
		}
		return
	}
}

func main() {
	setupConfig()

	URL := viper.GetString("rmi.redmine.url")
	KEY := viper.GetString("rmi.redmine.key")

	if URL == "" || KEY == "" {
		panic("no credentials found in config or environment.")
	}

	if len(os.Args) == 1 {
		fmt.Fprintln(os.Stderr, "expected issue id not given as first param.")
		os.Exit(1)
	}

	rmc, err := redmine.NewClient(URL, KEY, "#", true)
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
			HorizontalRule().
			PlainTextf("sdz-project: %s", pn).
			PlainTextf("sdz-reporter: %s", i.Author.Name).
			PlainTextf("sdz-issue: \"%s/issues/%d\"", viper.GetString("rmi.redmine.url"), i.ID).
			PlainTextf("last-update: %s", time.Now().Format("2006-01-02")).
			HorizontalRule().
			H1(i.Subject).
			PlainText("\n").
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

	// https://adeboyedn.hashnode.dev/git-hooks-a-simple-guide#heading-post-commit
	commit_hash := os.Args[3]
	if commit_hash == "" {
		fmt.Println("commit hash not found!")
		os.Exit(0)
	}

	linkRegEx := regexp.MustCompile(`    - https://projects.sdzecom.de/issues/\d+`)
	match := linkRegEx.FindStringSubmatch(commit)

	for _, m := range match {
		m = strings.TrimSpace(m)
		parts := strings.Split(m, "/")

		issueID, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil {
			panic(err)
		}

		comment := `
Notiz: Dieser Kommentar wurde automatisch erzeugt, weil an diesem Ticket gearbeitet wurde:
<pre>
%s
</pre>

Commit-Hash: %s
`

		if viper.GetBool("rmi.redmine.dryrun") {
			litter.Dump(fmt.Sprintf(comment, commit, commit_hash))
			continue
		}

		if err := rmc.WriteComment(issueID, fmt.Sprintf(comment, commit, commit_hash)); err != nil {
			panic(err)
		}
	}
}
