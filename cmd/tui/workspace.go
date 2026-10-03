package main

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/tmux"
)

func workspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Move between tuios workspaces like tmux windows",
	}
	cmd.AddCommand(&cobra.Command{
		Use:          "new",
		Short:        "Open a window in the next empty workspace and switch to it",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tmux.NewWorkspaceWindow(os.Getenv("TUIOS_SESSION"), os.Getenv("TUIOS_ACTIVE_PANE_CWD"))
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:          "next",
		Short:        "Switch to the next workspace that has windows",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tmux.SwitchWorkspace(os.Getenv("TUIOS_SESSION"), 1)
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:          "prev",
		Short:        "Switch to the previous workspace that has windows",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return tmux.SwitchWorkspace(os.Getenv("TUIOS_SESSION"), -1)
		},
	})
	return cmd
}
