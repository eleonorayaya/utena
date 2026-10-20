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
			target := attachTarget{}
			if len(args) == 1 {
				target.SessionName = args[0]
			}
			for {
				next, err := attachUntilSwitch(cmd.Context(), port, target)
				if err != nil || next.SessionName == "" {
					return err
				}
				target = next
			}
		},
	}
}

type attachTarget struct {
	SessionName string `json:"session_name"`
	Host        string `json:"host,omitempty"`
}

func tuiosAttachArgs(target attachTarget) []string {
	args := []string{"attach"}
	if target.SessionName == "" {
		return args
	}
	if target.Host != "" {
		args = append(args, "--host", target.Host)
	}
	return append(args, target.SessionName)
}

func attachUntilSwitch(ctx context.Context, port string, target attachTarget) (attachTarget, error) {
	child := exec.Command("tuios", tuiosAttachArgs(target)...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		return attachTarget{}, err
	}
	if target.SessionName != "" {
		postClientHook(port, "client-session-changed", target)
	}

	pollCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	switched := make(chan attachTarget, 1)
	go func() {
		for pollCtx.Err() == nil {
			next, err := nextActivation(pollCtx, port)
			if err != nil {
				select {
				case <-pollCtx.Done():
				case <-time.After(2 * time.Second):
				}
				continue
			}
			if next.SessionName != "" {
				switched <- next
				_ = child.Process.Signal(syscall.SIGTERM)
				return
			}
		}
	}()

	waitErr := child.Wait()
	cancel()
	if target.SessionName != "" {
		postClientHook(port, "client-detached", target)
	}

	select {
	case next := <-switched:
		return next, nil
	default:
		return attachTarget{}, waitErr
	}
}

func nextActivation(ctx context.Context, port string) (attachTarget, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/mux/activations/next", daemonURL(port)), nil)
	if err != nil {
		return attachTarget{}, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return attachTarget{}, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusNoContent:
		return attachTarget{}, nil
	case http.StatusOK:
		var body attachTarget
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			return attachTarget{}, err
		}
		return body, nil
	default:
		return attachTarget{}, fmt.Errorf("activations: unexpected status %d", res.StatusCode)
	}
}

func postClientHook(port, event string, target attachTarget) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	putJSON(ctx, fmt.Sprintf("%s/mux/hooks/%s", daemonURL(port), event), target)
}

func putJSON(ctx context.Context, url string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err == nil {
		res.Body.Close()
	}
}
