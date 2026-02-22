package api

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	Port       int
	ConfigDir  string
	PrettyLogs bool
}

func LoadConfig(args []string) (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolving home directory: %w", err)
	}

	defaultConfigDir := filepath.Join(homeDir, ".config", "utena")

	fs := flag.NewFlagSet("utena-daemon", flag.ContinueOnError)
	cfg := Config{}
	fs.IntVar(&cfg.Port, "port", 3333, "port to listen on")
	fs.StringVar(&cfg.ConfigDir, "config-dir", defaultConfigDir, "path to config directory")
	fs.BoolVar(&cfg.PrettyLogs, "pretty-logs", false, "enable pretty log output")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if v := os.Getenv("UTENA_PORT"); v != "" {
		fmt.Sscanf(v, "%d", &cfg.Port)
	}
	if v := os.Getenv("UTENA_DATA_DIR"); v != "" {
		cfg.ConfigDir = v
	}
	if os.Getenv("UTENA_LOG_PRETTY") == "true" {
		cfg.PrettyLogs = true
	}

	return cfg, nil
}

func (c Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}
