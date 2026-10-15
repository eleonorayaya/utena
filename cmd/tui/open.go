package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/browser"
)

const browserAlias = "utena-open"

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "open <url>...",
		Short:        "Open URLs in this session's browser window",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			base := fmt.Sprintf("http://localhost:%s", port)
			sessionID := sessionIDFromEnv()
			for _, u := range args {
				if err := openURL(cmd.Context(), base, sessionID, u, systemOpen); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func openURL(ctx context.Context, baseURL string, sessionID uint, url string, fallback func(string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := browser.RequestOpen(ctx, http.DefaultClient, baseURL+"/browser", browser.OpenRequest{SessionID: sessionID, URL: url})
	if err != nil {
		return fallback(url)
	}
	return nil
}

func systemOpen(url string) error {
	return exec.Command("/usr/bin/open", url).Run()
}

func sessionIDFromEnv() uint {
	id, err := strconv.ParseUint(os.Getenv("UTENA_SESSION_ID"), 10, 64)
	if err != nil {
		return 0
	}
	return uint(id)
}

func rewriteArgs(args []string) []string {
	if len(args) == 0 || filepath.Base(args[0]) != browserAlias {
		return args
	}
	return append([]string{args[0], "open"}, args[1:]...)
}
