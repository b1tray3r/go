package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

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
		router.HandleFunc(
			"POST /log",
			// withMiddleware(
			srv.handleAddLog,
			//srv.withAuth,
			//),
		)

		router.HandleFunc("/list", srv.listEntries)

		router.HandleFunc("/sync", srv.syncEntry)

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

func (srv *Server) listEntries(w http.ResponseWriter, r *http.Request) {
	slog.Debug("list logs triggered")

	date := r.URL.Query().Get("date")
	if date == "" {
		http.Error(w, "Date query parameter is required", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join("./data", date+".json")
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
	w.Write([]byte("<!DOCTYPE html><html><head><title>Time Entries</title></head><body>"))
	defer w.Write([]byte("</body></html>"))
	w.Write([]byte("<table border='1'><tr><th>Hours</th><th>Tags</th><th>Note</th><th>Sync</th></tr>"))
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
		w.Write([]byte("<td>" + syncIcon + " <button onclick=\"syncEntry(" + strconv.Itoa(i) + ")\">Sync</button></td></tr>"))
	}
	w.Write([]byte("</table>"))

	w.Write([]byte(`
		<script>
			function syncEntry(index) {
				fetch('/sync', {
					method: 'POST',
					headers: {
						'Content-Type': 'application/json'
					},
					body: JSON.stringify({ date: '` + date + `', index: index })
				})
				.then(response => {
					if (response.ok) {
						alert('Entry synced successfully');
					} else {
						alert('Failed to sync entry');
					}
				})
				.catch(error => {
					console.error('Error syncing entry:', error);
					alert('Error syncing entry');
				});
			}
		</script>
	`))
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

	filePath := filepath.Join("./data", req.Date+".json")
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

	entries[req.Index].Synced = true

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
	// w.Header().Set("Content-Type", "application/json")

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

	json.NewEncoder(w).Encode(&ServerResponse{
		Status:  200,
		Message: "MD accepted!",
	})

	// Extract the date from the markdown body
	dateRegex := regexp.MustCompile(`#\s*(\d{4}-\d{2}-\d{2})`)
	dateMatches := dateRegex.FindStringSubmatch(string(body))
	if len(dateMatches) < 2 {
		return
	}
	date := dateMatches[1]

	// Create the data directory if it doesn't exist
	dataDir := "./data"
	if err := os.MkdirAll(dataDir, os.ModePerm); err != nil {
		http.Error(w, "Failed to create data directory", http.StatusInternalServerError)
		slog.Error("Failed to create data directory", "error", err)
		return
	}

	// Create the file with the date as the filename
	filePath := filepath.Join(dataDir, date+".json")
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

	json.NewEncoder(w).Encode(entries)
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

	viper.SetDefault("wls.server.address", "8085")
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
