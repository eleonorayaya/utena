package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type config struct {
	SessionRoots []string `json:"session_roots"`
	RepoRoots    []string `json:"repo_roots"`
	Repos        []string `json:"repos"`
}

var (
	configOnce   sync.Once
	loadedConfig config
)

func configPath() string {
	if p := os.Getenv("UTENA_CONFIG"); p != "" {
		return p
	}
	if dir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herdr", "plugins", "config",
		"eleonorayaya.utena", "config.json")
}

func expandHome(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
}

func loadConfig() config {
	configOnce.Do(func() {
		path := configPath()
		if path == "" {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var c config
		if json.Unmarshal(data, &c) != nil {
			return
		}
		for i := range c.SessionRoots {
			c.SessionRoots[i] = expandHome(c.SessionRoots[i])
		}
		for i := range c.RepoRoots {
			c.RepoRoots[i] = expandHome(c.RepoRoots[i])
		}
		for i := range c.Repos {
			c.Repos[i] = expandHome(c.Repos[i])
		}
		loadedConfig = c
	})
	return loadedConfig
}
