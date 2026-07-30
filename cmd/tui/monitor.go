package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/spf13/cobra"
)

const monitorRetryDelay = 5 * time.Second

func monitorCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "monitor <session-id>",
		Short:        "Stream session events as newline-delimited JSON for Claude Code's Monitor tool",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := strconv.ParseUint(args[0], 10, 64)
			if err != nil || sessionID == 0 {
				return fmt.Errorf("invalid session id %q", args[0])
			}
			port, _ := cmd.Root().Flags().GetString("port")
			url := fmt.Sprintf("ws://localhost:%s/monitor/ws?session_id=%d", port, sessionID)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			for ctx.Err() == nil {
				if err := streamSessionEvents(ctx, url, os.Stdout); err != nil && ctx.Err() == nil {
					// ponytail: stdout lines are events, so retry noise goes to stderr only
					fmt.Fprintf(os.Stderr, "utena monitor: %s, reconnecting in %s\n", describeDisconnect(err), monitorRetryDelay)
				}
				select {
				case <-ctx.Done():
				case <-time.After(monitorRetryDelay):
				}
			}
			return nil
		},
	}
}

// describeDisconnect keeps the expected case — the daemon restarting, which
// drops the socket without a close handshake — from reading like a failure.
func describeDisconnect(err error) string {
	if errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) ||
		websocket.CloseStatus(err) != -1 || strings.Contains(err.Error(), "connection refused") {
		return "daemon connection closed (daemon restarting?)"
	}
	return err.Error()
}

func streamSessionEvents(ctx context.Context, url string, out io.Writer) error {
	sock, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	defer func() { _ = sock.CloseNow() }()

	for {
		kind, data, err := sock.Read(ctx)
		if err != nil {
			return err
		}
		if kind != websocket.MessageText {
			continue
		}
		if _, err := fmt.Fprintln(out, string(data)); err != nil {
			return err
		}
	}
}
