package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/claude"
)

func statusLineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "status-line",
		Short:        "Print a one-line tmux status-bar segment for sessions waiting on you",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
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

type barRow struct {
	Name      string
	Attention claude.ClaudeSessionStatus
}

func fetchStatusLineRows(port string) ([]barRow, error) {
	resp, err := fetchSessionListResponse(port, 1500*time.Millisecond)
	if err != nil {
		return nil, err
	}

	rows := make([]barRow, 0, len(resp.Sessions))
	for _, sr := range resp.Sessions {
		if sr.Session == nil || !sr.ListVisible(false) {
			continue
		}
		rows = append(rows, barRow{Name: sr.DisplayLabel(), Attention: sr.AttentionStatus})
	}
	return rows, nil
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
