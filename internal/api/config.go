package api

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/eleonorayaya/utena/internal/bundles"
)

type Config struct {
	Port             int
	ConfigDir        string
	PrettyLogs       bool
	BranchPrefix     string
	SessionsRoot     string
	WM               string
	Browser          string
	BrowserWorkspace string
	ParkWorkspace    string
	Follower         bool
	LeaderURL        string
	Bundles          bundles.Catalog
	McpServers       bundles.Servers
}

func LoadConfig(args []string) (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolving home directory: %w", err)
	}

	defaultConfigDir := filepath.Join(homeDir, ".config", "utena")
	defaultSessionsRoot := filepath.Join(homeDir, "utena-sessions")

	fs := flag.NewFlagSet("utena-daemon", flag.ContinueOnError)
	cfg := Config{}
	fs.IntVar(&cfg.Port, "port", 3333, "port to listen on")
	fs.StringVar(&cfg.ConfigDir, "config-dir", defaultConfigDir, "path to config directory")
	fs.BoolVar(&cfg.PrettyLogs, "pretty-logs", false, "enable pretty log output")
	fs.StringVar(&cfg.BranchPrefix, "branch-prefix", "eqt/", "prefix for git branches created with worktrees")
	fs.StringVar(&cfg.SessionsRoot, "sessions-root", defaultSessionsRoot, "directory that holds multi-workspace session roots")
	fs.StringVar(&cfg.WM, "wm", "aerospace", "window manager driver: aerospace or none")
	fs.StringVar(&cfg.Browser, "browser", "chrome", "browser driver: chrome, firefox or chrome-applescript")
	fs.StringVar(&cfg.BrowserWorkspace, "browser-workspace", "2", "workspace for the attached session's browser windows")
	fs.StringVar(&cfg.ParkWorkspace, "park-workspace", "P", "workspace for other sessions' browser windows")
	fs.BoolVar(&cfg.Follower, "follower", false, "run as a follower of a leader daemon: source files and browser opens go to the leader")
	fs.StringVar(&cfg.LeaderURL, "leader-url", "", "base URL of the leader daemon, required with --follower")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	browserFlag := false
	fs.Visit(func(f *flag.Flag) { browserFlag = browserFlag || f.Name == "browser" })

	if v := os.Getenv("UTENA_PORT"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &cfg.Port); err != nil {
			return Config{}, fmt.Errorf("invalid UTENA_PORT %q: %w", v, err)
		}
	}
	if v := os.Getenv("UTENA_DATA_DIR"); v != "" {
		cfg.ConfigDir = v
	}
	file, err := loadDaemonFile(cfg.ConfigDir)
	if err != nil {
		return Config{}, err
	}
	cfg.Bundles = file.Bundles
	cfg.McpServers = file.McpServers
	if !browserFlag && os.Getenv("UTENA_BROWSER") == "" && file.Browser != "" {
		cfg.Browser = file.Browser
	}
	if os.Getenv("UTENA_LOG_PRETTY") == "true" {
		cfg.PrettyLogs = true
	}
	if v := os.Getenv("UTENA_BRANCH_PREFIX"); v != "" {
		cfg.BranchPrefix = v
	}
	if v := os.Getenv("UTENA_SESSIONS_ROOT"); v != "" {
		cfg.SessionsRoot = v
	}
	if v := os.Getenv("UTENA_WM"); v != "" {
		cfg.WM = v
	}
	if v := os.Getenv("UTENA_BROWSER"); v != "" {
		cfg.Browser = v
	}
	if v := os.Getenv("UTENA_BROWSER_WORKSPACE"); v != "" {
		cfg.BrowserWorkspace = v
	}
	if v := os.Getenv("UTENA_PARK_WORKSPACE"); v != "" {
		cfg.ParkWorkspace = v
	}

	if v := os.Getenv("UTENA_FOLLOWER"); v != "" {
		follower, err := strconv.ParseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid UTENA_FOLLOWER %q: %w", v, err)
		}
		cfg.Follower = follower
	}
	if v := os.Getenv("UTENA_LEADER_URL"); v != "" {
		cfg.LeaderURL = v
	}
	cfg.LeaderURL = strings.TrimRight(cfg.LeaderURL, "/")

	if cfg.Follower {
		if cfg.LeaderURL == "" {
			return Config{}, errors.New("--follower requires --leader-url (or UTENA_LEADER_URL)")
		}
		u, err := url.Parse(cfg.LeaderURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return Config{}, fmt.Errorf("invalid leader URL %q: must be absolute, e.g. http://127.0.0.1:3334", cfg.LeaderURL)
		}
	}
	if _, ok := browserDrivers[cfg.Browser]; !ok {
		return Config{}, fmt.Errorf("invalid browser %q: must be chrome, firefox or chrome-applescript", cfg.Browser)
	}
	if cfg.WM != "aerospace" && cfg.WM != "none" {
		return Config{}, fmt.Errorf("invalid window manager %q: must be aerospace or none", cfg.WM)
	}

	return cfg, nil
}

type daemonFile struct {
	Browser    string          `json:"browser"`
	Bundles    bundles.Catalog `json:"bundles"`
	McpServers bundles.Servers `json:"mcp_servers"`
}

func loadDaemonFile(dir string) (daemonFile, error) {
	var f daemonFile
	data, err := os.ReadFile(filepath.Join(dir, "daemon.json"))
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, fmt.Errorf("reading daemon.json: %w", err)
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("parsing daemon.json: %w", err)
	}
	return f, nil
}

func (c Config) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.Port)
}
