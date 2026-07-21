package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

func statusLineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "status-line",
		Short:        "Print a one-line tmux status-bar segment for sessions waiting on you",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port := resolveStatusLinePort(cmd)
			rows, err := fetchStatusLineRows(port)
			if err != nil {
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), formatStatusLine(rows))
			return nil
		},
	}
	return cmd
}

func resolveStatusLinePort(cmd *cobra.Command) string {
	if cmd.Root().Flags().Changed("port") {
		port, _ := cmd.Root().Flags().GetString("port")
		return port
	}
	if envPort := os.Getenv("UTENA_PORT"); envPort != "" {
		return envPort
	}
	return defaultPort
}

type barRow struct {
	Name      string
	Attention claude.ClaudeSessionStatus
}

func fetchStatusLineRows(port string) ([]barRow, error) {
	url := fmt.Sprintf("http://localhost:%s/sessions", port)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
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

	rows := make([]barRow, 0, len(resp.Sessions))
	for _, sr := range resp.Sessions {
		if sr.Session == nil || !sr.ListVisible(false) {
			continue
		}
		rows = append(rows, barRow{Name: sessionDisplayLabel(*sr.Session), Attention: sr.AttentionStatus})
	}
	return rows, nil
}

func sessionDisplayLabel(s session.Session) string {
	if s.Name != "" {
		return s.Name
	}
	return s.WorkspaceDisplay()
}

func formatStatusLine(rows []barRow) string {
	var needsAttention, readyForReview []string
	for _, r := range rows {
		switch r.Attention {
		case claude.StatusNeedsAttention:
			needsAttention = append(needsAttention, fmt.Sprintf("#[fg=red,bold]! %s#[default]", r.Name))
		case claude.StatusReadyForReview:
			readyForReview = append(readyForReview, fmt.Sprintf("#[fg=green]✓ %s#[default]", r.Name))
		}
	}
	return strings.Join(append(needsAttention, readyForReview...), " ")
}
