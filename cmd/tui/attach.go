package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func attachCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "attach [session]",
		Short:        "Attach to tuios and follow session switches made from the utena TUI",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			for {
				next, err := attachUntilSwitch(cmd.Context(), port, target)
				if err != nil || next == "" {
					return err
				}
				target = next
			}
		},
	}
}

func attachUntilSwitch(ctx context.Context, port, target string) (string, error) {
	tuiosArgs := []string{"attach"}
	if target != "" {
		tuiosArgs = append(tuiosArgs, target)
	}
	child := exec.Command("tuios", tuiosArgs...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		return "", err
	}
	if target != "" {
		postClientHook(port, "client-session-changed", target)
	}

	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	switched := make(chan string, 1)
	go func() {
		for pollCtx.Err() == nil {
			name, err := nextActivation(pollCtx, port)
			if err != nil {
				select {
				case <-pollCtx.Done():
				case <-time.After(2 * time.Second):
				}
				continue
			}
			if name != "" && name != target {
				switched <- name
				_ = child.Process.Signal(syscall.SIGTERM)
				return
			}
		}
	}()

	waitErr := child.Wait()
	cancel()
	if target != "" {
		postClientHook(port, "client-detached", target)
	}

	select {
	case next := <-switched:
		return next, nil
	default:
		return "", waitErr
	}
}

func nextActivation(ctx context.Context, port string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:%s/tmux/activations/next", port), nil)
	if err != nil {
		return "", err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusNoContent:
		return "", nil
	case http.StatusOK:
		var body struct {
			SessionName string `json:"session_name"`
		}
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			return "", err
		}
		return body.SessionName, nil
	default:
		return "", fmt.Errorf("activations: unexpected status %d", res.StatusCode)
	}
}

func postClientHook(port, event, sessionName string) {
	body, _ := json.Marshal(map[string]string{"session_name": sessionName})
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("http://localhost:%s/tmux/hooks/%s", port, event), bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	if res, err := client.Do(req); err == nil {
		res.Body.Close()
	}
}
