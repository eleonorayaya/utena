package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const branchCheckoutFlag = "1"

func hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Git hook entry points",
		Hidden: true,
	}
	cmd.AddCommand(postCheckoutHookCmd())
	return cmd
}

func postCheckoutHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "post-checkout [prev] [new] [flag]",
		Short:         "Report a branch checkout to the utena daemon",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			reportCheckout(cmd.Context(), "http://localhost:"+port, args)
			return nil
		},
	}
}

func reportCheckout(ctx context.Context, baseURL string, args []string) {
	if len(args) < 3 || args[2] != branchCheckoutFlag {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	toplevel, err := gitOutput(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return
	}
	branch, err := gitOutput(ctx, "branch", "--show-current")
	if err != nil {
		return
	}

	body, err := json.Marshal(map[string]string{"path": toplevel, "branch": branch})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, baseURL+"/sessions/branch-hook", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err == nil {
		res.Body.Close()
	}
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
