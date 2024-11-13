package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/urfave/cli/v2"
)

// BackupFile represents a backup file
type BackupFile struct {
	Name string
	Time time.Time
	Tags []string
}

// Rotator represents the backup rotation implementation
type Rotator struct {
	Dry bool

	Keep       int
	KeepDays   int
	KeepWeeks  int
	KeepMonths int
	KeepYears  int

	SourceDir      string
	DestinationDir string

	FoundFiles    []BackupFile
	SelectedFiles []BackupFile
}

// clear removes all existing links from the destination directory.
func (r *Rotator) clear() error {
	files, err := os.ReadDir(r.DestinationDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		err := os.Remove(r.DestinationDir + file.Name())
		if err != nil {
			return err
		}
	}

	return nil
}

// Read reads the files in the source directory and populates the Files slice.
func (r *Rotator) Read() ([]BackupFile, error) {
	files, err := os.ReadDir(r.SourceDir)
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2})\.sql\.gz`)
	r.FoundFiles = make([]BackupFile, 0)
	for _, file := range files {
		matches := re.FindStringSubmatch(file.Name())
		if len(matches) == 2 {
			timestamp, err := time.Parse("2006-01-02T15-04-05", matches[1])
			if err != nil {
				fmt.Println("error parsing timestamp:", err)
				continue
			}
			r.FoundFiles = append(r.FoundFiles, BackupFile{
				Name: file.Name(),
				Time: timestamp,
			})
		}
	}

	// Sort backups by time (newest first)
	sort.Slice(r.FoundFiles, func(i, j int) bool {
		return r.FoundFiles[i].Time.After(r.FoundFiles[j].Time)
	})

	return r.FoundFiles, nil
}

// link creates symlinks in the destination directory prepending the "biggest" tag.
// The tag order is: keep, daily, weekly, monthly, yearly where yearly is the "biggest".
func (r *Rotator) link() error {
	for _, result := range r.SelectedFiles {
		for _, tag := range result.Tags {
			srcPath := r.SourceDir + result.Name
			destPath := r.DestinationDir + tag + "-" + result.Name
			if _, err := os.Lstat(destPath); os.IsNotExist(err) {
				err := os.Symlink(srcPath, destPath)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (r *Rotator) remove() error {
	resultMap := make(map[string]bool)
	for _, result := range r.SelectedFiles {
		resultMap[result.Name] = true
	}
	for _, backup := range r.FoundFiles {
		if !resultMap[backup.Name] {
			if !r.Dry {
				if err := os.Remove(r.SourceDir + backup.Name); err != nil {
					return err
				}
			} else {
				fmt.Println("DryRun: remove", r.SourceDir+backup.Name)
			}
		}
	}
	return nil
}

// Rotate implements the rotation strategy.
func (r *Rotator) Rotate() {
	r.clear()

	// keep the first n backups
	r.SelectedFiles = r.FoundFiles[:r.Keep]
	for i := 0; i < r.Keep && i < len(r.FoundFiles); i++ {
		r.FoundFiles[i].Tags = append(r.FoundFiles[i].Tags, "keep")
	}

	// Collect backups (up to Keep[Days, Weeks, Months, Years]) beginning from the newest
	daily := make(map[string]BackupFile)
	weekly := make(map[string]BackupFile)
	monthly := make(map[string]BackupFile)
	yearly := make(map[string]BackupFile)

	for _, backup := range r.FoundFiles[r.Keep:] {
		date := backup.Time.Format("2006-01-02")
		_, weekNumber := backup.Time.ISOWeek()
		week := fmt.Sprintf("%d-W%02d", backup.Time.Year(), weekNumber)
		month := backup.Time.Format("2006-01")
		year := backup.Time.Format("2006")

		if _, exists := daily[date]; !exists && len(daily) < r.KeepDays {
			backup.Tags = append(backup.Tags, "daily")
			daily[date] = backup
			r.SelectedFiles = append(r.SelectedFiles, backup)
		}

		if _, exists := weekly[week]; !exists && len(weekly) < r.KeepWeeks {
			backup.Tags = append(backup.Tags, "weekly")
			weekly[week] = backup
			r.SelectedFiles = append(r.SelectedFiles, backup)
		}

		if _, exists := monthly[month]; !exists && len(monthly) < r.KeepMonths {
			backup.Tags = append(backup.Tags, "monthly")
			monthly[month] = backup
			r.SelectedFiles = append(r.SelectedFiles, backup)
		}

		if _, exists := yearly[year]; !exists && len(yearly) < r.KeepYears {
			backup.Tags = append(backup.Tags, "yearly")
			yearly[year] = backup
			r.SelectedFiles = append(r.SelectedFiles, backup)
		}
	}

	// Create Symlinks for the kept backups
	if err := r.link(); err != nil {
		fmt.Printf("error linking files: %v\n", err)
	}

	// Remove backups that are not selected
	if err := r.remove(); err != nil {
		fmt.Printf("error removing files: %v\n", err)
	}
}

func main() {
	var dryCount int

	app := &cli.App{
		Name:  "backup-rotator",
		Usage: "Rotate backups with keeps and generations",
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:  "keep",
				Usage: "Number of backups to keep",
				Value: 5,
			},
			&cli.IntFlag{
				Name:  "keep-days",
				Usage: "Number of daily backups to keep",
				Value: 7,
			},
			&cli.IntFlag{
				Name:  "keep-weeks",
				Usage: "Number of weekly backups to keep",
				Value: 5,
			},
			&cli.IntFlag{
				Name:  "keep-months",
				Usage: "Number of monthly backups to keep",
				Value: 6,
			},
			&cli.IntFlag{
				Name:  "keep-years",
				Usage: "Number of yearly backups to keep",
				Value: 2,
			},
			&cli.BoolFlag{
				Name:  "dry",
				Usage: "Dry run",
				Count: &dryCount,
			},
			&cli.StringFlag{
				Name:  "source",
				Usage: "Source directory",
			},
			&cli.StringFlag{
				Name:  "destination",
				Usage: "Destination directory",
			},
		},
		Action: func(c *cli.Context) error {
			srcDir := c.String("source")
			if srcDir[len(srcDir)-1] != '/' {
				srcDir += "/"
			}

			dstDir := c.String("destination")
			if dstDir[len(dstDir)-1] != '/' {
				dstDir += "/"
			}

			if dryCount > 0 {
				fmt.Println("Dry run enabled")
			}

			rotator := Rotator{
				Dry:            (dryCount > 0),
				Keep:           c.Int("keep"),
				KeepDays:       c.Int("keep-days"),
				KeepWeeks:      c.Int("keep-weeks"),
				KeepMonths:     c.Int("keep-months"),
				KeepYears:      c.Int("keep-years"),
				SourceDir:      srcDir,
				DestinationDir: dstDir,
			}

			files, err := rotator.Read()
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return err
			}

			rotator.Rotate()

			for _, file := range rotator.SelectedFiles {
				fmt.Println("Linked file:", file.Name, "Tags:", file.Tags)
			}

			return nil
		},
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, err.Error()+"\n")
		os.Exit(1)
	}
}
