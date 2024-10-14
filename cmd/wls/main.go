package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/b1tray3r/go/internal/redmine"
	"github.com/spf13/viper"
	"golang.org/x/crypto/bcrypt"
)

type BasicAuth struct {
	Username string
	Secret   string
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func NewServer(auth *BasicAuth) (*Server, error) {
	secret, err := hashPassword(auth.Secret)
	if err != nil {
		return nil, err
	}
	auth.Secret = secret

	return &Server{
		Auth: auth,
	}, nil
}

type Server struct {
	Auth *BasicAuth

	init sync.Once
	mux  *http.ServeMux
}

// HTTPMiddleware defines the required function interface which
// can be implemented in order to be used in the withMiddleware function.
type HTTPMiddleware func(http.HandlerFunc) http.HandlerFunc

// withMiddleware will wrap the given handler with the
// provided middleware(s) in reverse order.
func withMiddleware(h http.HandlerFunc, m ...HTTPMiddleware) http.HandlerFunc {
	if len(m) < 1 {
		return h
	}
	slices.Reverse(m)

	wrapped := h
	for _, mw := range m {
		wrapped = mw(wrapped)
	}

	return wrapped
}

// withAuth is a middleware that checks the basic auth credentials.
func (srv *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		slog.Info("with auth triggered")
		username, password, ok := r.BasicAuth()

		if ok {
			if username == srv.Auth.Username {
				slog.Info("matching users", username, srv.Auth.Username)
				if err := bcrypt.CompareHashAndPassword([]byte(srv.Auth.Secret), []byte(password)); err != nil {
					w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					slog.Error("failed to authenticate", "user", username, slog.Any("Error", err))
					return
				}

				next.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}

// ServeHTTP implements the http.Handler interface.
// It initializes the server and the available endpoints.
func (srv *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv.init.Do(func() {
		router := http.NewServeMux()

		// public endpoints
		router.HandleFunc("/health", srv.healthCheck)

		// private endpoints with auth

		router.HandleFunc("GET /all", srv.listAll)
		router.HandleFunc("GET /day", srv.listEntriesforDay)

		router.HandleFunc("POST /sync", srv.syncEntry)
		router.HandleFunc("POST /log", srv.handleAddLog)

		srv.mux = router
	})

	srv.mux.ServeHTTP(w, r)
}

// ServerResponse is a simple response struct for the server.
type ServerResponse struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
}

func (srv *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	slog.Debug("health check requested")
	json.NewEncoder(w).Encode(
		&ServerResponse{
			Status:  http.StatusOK,
			Message: "OK",
		},
	)
}

type Tag struct {
	Name  string
	Value string
}

type TimeEntry struct {
	Hours  float64
	Tags   []Tag
	Note   string
	Synced bool
}

func (srv *Server) listAll(w http.ResponseWriter, r *http.Request) {

}

func (srv *Server) listEntriesforDay(w http.ResponseWriter, r *http.Request) {
	slog.Debug("list logs triggered")
	date := r.URL.Query().Get("date")
	year := date[:4]
	month := date[5:7]
	dataDir := "./data"
	filePath := filepath.Join(dataDir, year, month, date+".json")
	if date == "" {
		http.Error(w, "Date parameter is required", http.StatusBadRequest)
		return
	}

	slog.Debug("Reading entries for date", "date", date)

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "No entries found for the given date", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			slog.Error("Failed to open file", "error", err)
		}
		return
	}
	defer file.Close()

	var entries []TimeEntry
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&entries); err != nil {
		http.Error(w, "Failed to decode entries", http.StatusInternalServerError)
		slog.Error("Failed to decode entries", "error", err)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<!DOCTYPE html><html><head><title>Time Entries for " + date + "</title></head></html><body>"))
	defer w.Write([]byte("</body></html>"))
	w.Write([]byte(`
			<style>
				.calendar {
					display: grid;
					grid-template-columns: repeat(7, 1fr);
					gap: 5px;
					margin-top: 20px;
				}
				.calendar div {
					padding: 10px;
					text-align: center;
					border: 1px solid #ccc;
					cursor: pointer;
				}
				.calendar .header {
					font-weight: bold;
					background-color: #f0f0f0;
				}
				.calendar .today {
					background-color: #ffeb3b;
				}
				.calendar .weekend {
					background-color: #f0f0f0;
				}
			</style>
			<div class="calendar">
				<div class="header">Mon</div>
				<div class="header">Tue</div>
				<div class="header">Wed</div>
				<div class="header">Thu</div>
				<div class="header">Fri</div>
				<div class="header">Sat</div>
				<div class="header">Sun</div>
	`))

	now := time.Now()
	firstDayOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	startDay := (int(firstDayOfMonth.Weekday()) + 6) % 7 // Adjust to start with Monday

	for i := 0; i < startDay; i++ {
		w.Write([]byte(`<div></div>`))
	}

	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	for day := 1; day <= daysInMonth; day++ {
		dayDate := time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, now.Location())
		classes := ""
		if dayDate.Weekday() == time.Saturday || dayDate.Weekday() == time.Sunday {
			classes += " weekend"
		}
		if dayDate.Day() == now.Day() {
			classes += " today"
		}
		w.Write([]byte(fmt.Sprintf(`<div class="%s" onclick="window.location.href='?date=%s'">%d</div>`, classes, now.Format("2006-01-")+fmt.Sprintf("%02d", day), day)))
	}

	w.Write([]byte(`</div>`))
	w.Write([]byte("<h2>" + date + "</h2>"))
	w.Write([]byte("<table border='1'><tr><th>Hours</th><th>Tags</th><th>Note</th><th>Sync</th><th>Action</th></tr>"))
	for i, entry := range entries {
		tags := make([]string, len(entry.Tags))
		for j, tag := range entry.Tags {
			tags[j] = tag.Name + "/" + tag.Value
		}
		syncIcon := "&#10060;" // ❌
		if entry.Synced {
			syncIcon = "&#9989;" // ✅
		}
		w.Write([]byte("<tr><td>" + strconv.FormatFloat(entry.Hours, 'f', 2, 64) + "</td><td>" + strings.Join(tags, ", ") + "</td><td>" + entry.Note + "</td>"))
		w.Write([]byte("<td>" + syncIcon + "</td>"))
		if !entry.Synced {
			w.Write([]byte("<td><button onclick=\"syncEntry(" + strconv.Itoa(i) + ")\">Sync</button></td></tr>"))
		}
	}
	w.Write([]byte("</table>"))

	w.Write([]byte(`
		<script>
			function syncEntry(index) {
				const baseUrl = window.location.origin;
				fetch(baseUrl + '/sync', {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json'
					},
					body: JSON.stringify({ date: '` + date + `', index: index })
				})
				.then(response => {
					if (response.ok) {
						location.reload();
					} else {
						alert('Failed to sync entry');
						alert(response.statusText);
					}
				})
				.catch(error => {
					console.error('Error:', error);
					alert('Failed to sync entry');
				});
			}
		</script>
	`))
}

func findInTags(tags []Tag, name string) string {
	for _, tag := range tags {
		if tag.Name == name {
			return tag.Value
		}
	}
	return ""
}

func (srv *Server) syncEntry(w http.ResponseWriter, r *http.Request) {
	slog.Debug("sync entry triggered")

	var req struct {
		Date  string `json:"date"`
		Index int    `json:"index"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Failed to decode request body", http.StatusBadRequest)
		slog.Error("Failed to decode request body", "error", err)
		return
	}

	year := req.Date[:4]
	month := req.Date[5:7]
	dataDir := "./data"
	filePath := filepath.Join(dataDir, year, month, req.Date+".json")

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "No entries found for the given date", http.StatusNotFound)
		} else {
			http.Error(w, "Failed to open file", http.StatusInternalServerError)
			slog.Error("Failed to open file", "error", err)
		}
		return
	}
	defer file.Close()

	var entries []TimeEntry
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&entries); err != nil {
		http.Error(w, "Failed to decode entries", http.StatusInternalServerError)
		slog.Error("Failed to decode entries", "error", err)
		return
	}

	if req.Index < 0 || req.Index >= len(entries) {
		http.Error(w, "Invalid entry index", http.StatusBadRequest)
		return
	}

	// Handle Redmine Sync

	entry := entries[req.Index]
	if entry.Synced {
		http.Error(w, "Entry already synced", http.StatusBadRequest)
		return
	}

	duration := time.Duration(entry.Hours * float64(time.Hour))

	rc, err := redmine.NewClient(
		viper.GetString("wls.redmine.url"),
		viper.GetString("wls.redmine.key"),
		"",
		viper.GetBool("wls.redmine.dryrun"),
	)
	if err != nil {
		http.Error(w, "Failed to create Redmine client", http.StatusInternalServerError)
		slog.Error("Failed to create Redmine client", "error", err)
		return
	}

	date, err := time.Parse("2006-01-02", req.Date)
	if err != nil {
		log.Printf("failed to parse date string to time %s", req.Date)
		http.Error(w, "Failed to parse date", http.StatusBadRequest)
		return
	}

	issueID := findInTags(entry.Tags, "issue")
	if issueID == "" {
		http.Error(w, "No issue ID found in tags", http.StatusBadRequest)
		slog.Error("No issue ID found in tags")
		return
	}

	aID := findInTags(entry.Tags, "action")
	if aID == "" {
		http.Error(w, "No activity ID found in tags", http.StatusBadRequest)
		slog.Error("No activity ID found in tags")
		return
	}

	issueID = strings.TrimPrefix(issueID, "#")
	iid, err := strconv.ParseInt(issueID, 10, 64)
	if err != nil {
		http.Error(w, "Failed to parse issue ID", http.StatusBadRequest)
		slog.Error("Failed to parse issue ID", "issueID", issueID)
		return
	}
	issue, err := rc.GetIssue(iid)
	if err != nil {
		http.Error(w, "Failed to get issue", http.StatusInternalServerError)
		slog.Error("Failed to get issue", "issueID", issueID)
		return
	}

	pid := strconv.Itoa(int(issue.Project.ID))
	activityID, err := rc.GetActivityID(pid, aID)
	if err != nil {
		http.Error(w, "Failed to get activity ID", http.StatusInternalServerError)
		slog.Error("Failed to get activity ID", "activityID", aID)
		return
	}

	te := redmine.TimeEntry{
		IssueIDs:   []string{fmt.Sprintf("%d", issue.ID)},
		ActivityID: strconv.Itoa(int(activityID)),
		Start:      date,
		Duration:   duration.Hours(),
		IsRedmine:  true,
		Comment:    entry.Note,
	}

	if err := rc.Log(te); err != nil {
		http.Error(w, "Failed to log time entry", http.StatusInternalServerError)
		slog.Error("Failed to log time entry", "error", err)
		return
	}

	entries[req.Index].Synced = true

	// Store the entry
	file, err = os.Create(filePath)
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		slog.Error("Failed to create file", "error", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		http.Error(w, "Failed to write entries to file", http.StatusInternalServerError)
		slog.Error("Failed to write entries to file", "error", err)
		return
	}

	slog.Info("Entry successfully synced", "file", filePath, "index", req.Index)
	w.WriteHeader(http.StatusOK)
}

// handleStockUpdate is responsible to handle the incoming stock updates.
func (srv *Server) handleAddLog(w http.ResponseWriter, r *http.Request) {
	slog.Debug("add log triggered")
	//w.Header().Set("Content-Type", "application/json")

	regex := `\s+▶.*`
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		slog.Error("Failed to read request body", "error", err)
		return
	}
	defer r.Body.Close()

	re := regexp.MustCompile(regex)
	matches := re.FindAllString(string(body), -1)
	entries := make([]TimeEntry, 0)
	for _, match := range matches {
		if match == "" {
			continue
		}

		split := strings.Split(match, "|")

		h := strings.TrimSpace(split[1])
		hours, err := strconv.ParseFloat(strings.TrimSpace(h), 64)
		if err != nil {
			http.Error(w, "Failed to parse hours", http.StatusBadRequest)
			slog.Error("Failed to parse hours", "error", err)
			return
		}
		note := strings.TrimSpace(split[3])
		ts := split[2]

		tags := make([]Tag, 0)
		for _, t := range strings.Split(ts, " ") {
			t = strings.TrimPrefix(t, "#")
			if t == "" {
				continue
			}
			p := strings.Split(t, "/")

			tags = append(tags, Tag{
				Name:  p[0],
				Value: p[1],
			})
		}

		entries = append(entries, TimeEntry{
			Hours: hours,
			Note:  note,
			Tags:  tags,
		})
	}

	// Extract the date from the markdown body
	dateRegex := regexp.MustCompile(`#\s*(\d{4}-\d{2}-\d{2})`)
	dateMatches := dateRegex.FindStringSubmatch(string(body))
	if len(dateMatches) < 2 {
		return
	}
	date := dateMatches[1]

	// Create the data directory if it doesn't exist
	year := date[:4]
	month := date[5:7]
	dataDir := "./data"
	if err := os.MkdirAll(filepath.Join(dataDir, year, month), os.ModePerm); err != nil {
		http.Error(w, "Failed to create data directory", http.StatusInternalServerError)
		slog.Error("Failed to create data directory", "error", err)
		return
	}

	// Create the file with the date as the filename
	filePath := filepath.Join(dataDir, year, month, date+".json")
	file, err := os.Create(filePath)
	if err != nil {
		http.Error(w, "Failed to create file", http.StatusInternalServerError)
		slog.Error("Failed to create file", "error", err)
		return
	}
	defer file.Close()

	// Write the entries to the file as JSON
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entries); err != nil {
		http.Error(w, "Failed to write entries to file", http.StatusInternalServerError)
		slog.Error("Failed to write entries to file", "error", err)
		return
	}

	slog.Info("Entries successfully written to file", "file", filePath)

	json.NewEncoder(w).Encode(&ServerResponse{
		Status:  200,
		Message: "MD accepted!",
	})
}

// setupLoglevel sets the log level based on given verbosity
// The verbosity is a number between 0 and 3
// 0: Error
// 1: Warning
// 2: Info
// 3: Debug
func setupLoglevel(verbosity int) {
	result := 8 // equal to slog.LevelError // which is int 8

	// vor each -v we will decrease this by 4
	// at max we will interpret -vvv which represents slog.LevelDebug
	if verbosity > 3 {
		verbosity = 3
	}

	for i := 1; i <= verbosity; i++ {
		result -= 4
	}

	lvl := new(slog.LevelVar)
	lvl.Set(slog.Level(result))

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		AddSource: (verbosity > 2),
		Level:     lvl,
	}))
	slog.SetDefault(logger)

	slog.Debug("Log level is set to DEBUG.")
}

func setupConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath(".")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Error("Error getting home directory", "error", err)
		return
	}
	configPath := filepath.Join(homeDir, ".config", "wls")
	viper.AddConfigPath(configPath)

	// Read in environment variables that match
	viper.AutomaticEnv()

	// Read the config file
	if err := viper.ReadInConfig(); err != nil {
		slog.Error("Error reading config file", "error", err)
	}

	viper.SetDefault("wls.server.address", ":8085")
}

func main() {
	setupConfig()
	setupLoglevel(viper.GetInt("wls.app.loglevel"))

	srv, err := NewServer(
		&BasicAuth{
			Username: viper.GetString("wls.auth.username"),
			Secret:   viper.GetString("wls.auth.password"),
		},
	)
	if err != nil {
		slog.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	addr := viper.GetString("wls.server.address")

	slog.Debug(addr)

	slog.Info("Starting server", "address", addr)
	slog.Debug("With basic auth", "username", viper.GetString("wls.auth.username"), "secret", viper.GetString("wls.auth.password"))
	if err := http.ListenAndServe(addr, srv); err != nil {
		slog.Error("failed to start server", "error", err)
		os.Exit(1)
	}
}
