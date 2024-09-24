package main

import (
	"fmt"

	"github.com/b1tray3r/go/internal/redmine"
)

func main() {
	var id int64 = 41417

	rmc, err := redmine.NewClient("http://localhost/", "somekey", "#")
	if err != nil {
		fmt.Errorf("%v", err)
	}

	i, err := rmc.GetIssue(id)
	if err != nil {
		fmt.Errorf("%v", err)
	}

	fmt.Printf("%#v", i)
}
