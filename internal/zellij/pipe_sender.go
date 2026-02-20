package zellij

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

type PipeSender struct {
	pipeName string
}

func NewPipeSender() *PipeSender {
	pipeName := os.Getenv("UTENA_PIPE_NAME")
	if pipeName == "" {
		pipeName = "utena-commands"
	}
	return &PipeSender{
		pipeName: pipeName,
	}
}

func (p *PipeSender) SendCommand(cmd Command) error {
	// Serialize command to JSON
	payload, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	slog.Info("sending pipe command", "pipe", p.pipeName, "payload", string(payload))

	shellCmd := exec.Command(
		"zellij",
		"pipe",
		"--name", p.pipeName,
	)
	shellCmd.Stdin = strings.NewReader(string(payload))

	output, err := shellCmd.CombinedOutput()
	if err != nil {
		slog.Error("pipe command failed", "error", err, "output", string(output))
		return fmt.Errorf("zellij pipe failed: %w, output: %s", err, output)
	}

	slog.Info("pipe command sent successfully")
	return nil
}
