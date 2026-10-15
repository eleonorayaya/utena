package main

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/top"
)

func topCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "top",
		Short:        "Show CPU and memory use per session",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			once, _ := cmd.Flags().GetBool("once")
			interval, _ := cmd.Flags().GetDuration("interval")
			if interval < 500*time.Millisecond {
				return fmt.Errorf("--interval must be at least 500ms")
			}

			if once {
				resp, err := fetchResources(port, 10*time.Second)
				if err != nil {
					return err
				}
				return top.WriteTSV(cmd.OutOrStdout(), top.SortResources(resp.Sessions, top.SortCPU))
			}

			loadTheme()
			_, err := tea.NewProgram(top.New(interval, func() (*session.SessionResourcesResponse, error) {
				return fetchResources(port, interval+5*time.Second)
			}), tea.WithAltScreen()).Run()
			return err
		},
	}
	cmd.Flags().Bool("once", false, "print one tab-separated snapshot and exit")
	cmd.Flags().Duration("interval", 2*time.Second, "refresh interval for the live view")
	return cmd
}

func fetchResources(port string, timeout time.Duration) (*session.SessionResourcesResponse, error) {
	resp, err := getJSON[session.SessionResourcesResponse](port, "/sessions/resources", timeout)
	if err != nil {
		return nil, fmt.Errorf("could not reach utena daemon on port %s (is it running?): %w", port, err)
	}
	return resp, nil
}
