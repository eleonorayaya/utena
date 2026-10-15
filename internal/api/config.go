package api

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Port             int
	ConfigDir        string
	PrettyLogs       bool
	BranchPrefix     string
	SessionsRoot     string
	WM               string
	BrowserWorkspace string
	ParkWorkspace    string
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
	fs.StringVar(&cfg.BrowserWorkspace, "browser-workspace", "2", "workspace for the attached session's browser windows")
	fs.StringVar(&cfg.ParkWorkspace, "park-workspace", "P", "workspace for other sessions' browser windows")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if v := os.Getenv("UTENA_PORT"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &cfg.Port); err != nil {
			return Config{}, fmt.Errorf("invalid UTENA_PORT %q: %w", v, err)
		}
	}
	if v := os.Getenv("UTENA_DATA_DIR"); v != "" {
		cfg.ConfigDir = v
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
	if v := os.Getenv("UTENA_BROWSER_WORKSPACE"); v != "" {
		cfg.BrowserWorkspace = v
	}
	if v := os.Getenv("UTENA_PARK_WORKSPACE"); v != "" {
		cfg.ParkWorkspace = v
	}

	if cfg.WM != "aerospace" && cfg.WM != "none" {
		return Config{}, fmt.Errorf("invalid window manager %q: must be aerospace or none", cfg.WM)
	}

	return cfg, nil
}

func (c Config) Addr() string {
	return fmt.Sprintf("127.0.0.1:%d", c.Port)
}
