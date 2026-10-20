package coder

import (
	"fmt"
	"time"
)

const (
	DefaultSSHSuffix      = ".coder"
	DefaultFollowerPort   = 3333
	DefaultLeaderPort     = 3334
	DefaultIdleDisconnect = 10 * time.Minute
)

type FileConfig struct {
	Template       string            `json:"template"`
	Parameters     map[string]string `json:"parameters"`
	SSHSuffix      string            `json:"ssh_suffix"`
	FollowerPort   int               `json:"follower_port"`
	LeaderPort     int               `json:"leader_port"`
	IdleDisconnect string            `json:"idle_disconnect"`
}

type Config struct {
	Template       string
	Parameters     map[string]string
	SSHSuffix      string
	FollowerPort   int
	LeaderPort     int
	IdleDisconnect time.Duration
	LocalAPIPort   int
}

func ParseConfig(f FileConfig, localAPIPort int) (Config, error) {
	cfg := Config{
		Template:       f.Template,
		Parameters:     f.Parameters,
		SSHSuffix:      f.SSHSuffix,
		FollowerPort:   f.FollowerPort,
		LeaderPort:     f.LeaderPort,
		IdleDisconnect: DefaultIdleDisconnect,
		LocalAPIPort:   localAPIPort,
	}
	if cfg.SSHSuffix == "" {
		cfg.SSHSuffix = DefaultSSHSuffix
	}
	if cfg.FollowerPort == 0 {
		cfg.FollowerPort = DefaultFollowerPort
	}
	if cfg.LeaderPort == 0 {
		cfg.LeaderPort = DefaultLeaderPort
	}
	if f.IdleDisconnect != "" {
		d, err := time.ParseDuration(f.IdleDisconnect)
		if err != nil {
			return Config{}, fmt.Errorf("invalid coder.idle_disconnect %q: %w", f.IdleDisconnect, err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("invalid coder.idle_disconnect %q: must be positive", f.IdleDisconnect)
		}
		cfg.IdleDisconnect = d
	}
	for _, port := range []struct {
		name  string
		value int
	}{{"follower_port", cfg.FollowerPort}, {"leader_port", cfg.LeaderPort}} {
		if port.value < 1 || port.value > 65535 {
			return Config{}, fmt.Errorf("invalid coder.%s %d", port.name, port.value)
		}
	}
	for k := range cfg.Parameters {
		if k == "" {
			return Config{}, fmt.Errorf("coder.parameters has an empty name")
		}
		if k == "repo" {
			return Config{}, fmt.Errorf("coder.parameters must not set repo: it comes from the session's workspace")
		}
	}
	return cfg, nil
}

func (c Config) Enabled() bool {
	return c.Template != ""
}

func (c Config) SSHHost(workspace string) string {
	return workspace + c.SSHSuffix
}
