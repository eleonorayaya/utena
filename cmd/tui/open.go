package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
)

const browserAlias = "utena-open"

const fallbackEnvVar = "UTENA_OPEN_FALLBACK"

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "open <url>...",
		Short:        "Open URLs in this session's browser window",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			base := daemonURL(port)
			sessionID := sessionIDFromEnv()
			for _, u := range args {
				if err := openURL(cmd.Context(), cmd.ErrOrStderr(), base, sessionID, u, systemOpen); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func openURL(ctx context.Context, out io.Writer, baseURL string, sessionID uint, url string, fallback func(string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	res, err := browser.RequestOpen(ctx, http.DefaultClient, baseURL+"/browser", browser.OpenRequest{SessionID: sessionID, URL: url})
	if err != nil {
		return fallback(url)
	}
	if res.Pending {
		_, _ = fmt.Fprintf(out, "utena: %s is waiting in session %d's browser window\n", url, sessionID)
	}
	return nil
}

func systemOpen(url string) error {
	if insideFallback(os.Getenv(fallbackEnvVar), os.Getppid(), parentPID) {
		return fmt.Errorf("utena: browser unavailable and already inside the system opener, not opening %s", url)
	}
	cmd := exec.Command("/usr/bin/open", url)
	cmd.Env = fallbackEnv(os.Environ(), userManagerEnv(), os.Getpid())
	return cmd.Run()
}

func insideFallback(guard string, ppid int, parent func(int) int) bool {
	pid, err := strconv.Atoi(guard)
	if err != nil || pid <= 1 {
		return false
	}
	for i, p := 0, ppid; i < 64 && p > 1; i, p = i+1, parent(p) {
		if p == pid {
			return true
		}
	}
	return false
}

func parentPID(pid int) int {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	ppid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0
	}
	return ppid
}

var desktopEnvVars = []string{"DISPLAY", "WAYLAND_DISPLAY", "XDG_CURRENT_DESKTOP"}

func fallbackEnv(env, session []string, pid int) []string {
	out := make([]string, 0, len(env)+len(desktopEnvVars)+1)
	have := map[string]bool{}
	for _, kv := range env {
		if strings.HasPrefix(kv, "BROWSER=") || strings.HasPrefix(kv, fallbackEnvVar+"=") {
			continue
		}
		have[strings.SplitN(kv, "=", 2)[0]] = true
		out = append(out, kv)
	}
	for _, kv := range session {
		name := strings.SplitN(kv, "=", 2)[0]
		if !have[name] && slices.Contains(desktopEnvVars, name) {
			have[name] = true
			out = append(out, kv)
		}
	}
	return append(out, fallbackEnvVar+"="+strconv.Itoa(pid))
}

func userManagerEnv() []string {
	out, err := exec.Command("systemctl", "--user", "show-environment").Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func sessionIDFromEnv() uint {
	id, err := strconv.ParseUint(os.Getenv(session.EnvSessionID), 10, 64)
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
