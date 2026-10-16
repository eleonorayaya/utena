package main

import (
	"context"
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
			reportCheckout(cmd.Context(), daemonURL(port), args)
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

	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return
	}
	toplevel, branch, ok := strings.Cut(strings.TrimSpace(string(out)), "\n")
	if !ok {
		return
	}
	if branch == "HEAD" {
		branch = ""
	}

	putJSON(ctx, baseURL+"/sessions/branch-hook", map[string]string{"path": toplevel, "branch": branch})
}
