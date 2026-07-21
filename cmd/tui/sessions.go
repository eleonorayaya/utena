package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/session"
)

func sessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "sessions",
		Short:        "Inspect utena sessions",
		SilenceUsage: true,
	}
	cmd.AddCommand(sessionsLsCmd())
	return cmd
}

func sessionsLsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "ls",
		Short:        "List sessions (name, status, root), one per line",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")

			sessions, err := fetchSessions(port)
			if err != nil {
				return err
			}

			for _, s := range filterSessions(sessions, all) {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", s.Name, s.Status, s.SessionRoot)
			}
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "include hidden (archived or broken) sessions")
	return cmd
}

func filterSessions(sessions []session.Session, all bool) []session.Session {
	out := make([]session.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.ListVisible(all) {
			out = append(out, s)
		}
	}
	return out
}

func fetchSessions(port string) ([]session.Session, error) {
	resp, err := fetchSessionListResponse(port, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("could not reach utena daemon at http://localhost:%s/sessions (is it running?): %w", port, err)
	}

	sessions := make([]session.Session, len(resp.Sessions))
	for i, sr := range resp.Sessions {
		sessions[i] = *sr.Session
	}
	return sessions, nil
}

func fetchSessionListResponse(port string, timeout time.Duration) (*session.SessionListResponse, error) {
	url := fmt.Sprintf("http://localhost:%s/sessions", port)
	client := &http.Client{Timeout: timeout}
	res, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch sessions: unexpected status %d", res.StatusCode)
	}

	var resp session.SessionListResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
