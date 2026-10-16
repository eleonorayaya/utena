package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
)

func resolveSession(sessions []session.Session, name string, envID string) (*session.Session, error) {
	if name != "" {
		return resolveSessionByName(sessions, name)
	}
	if envID == "" {
		return nil, errors.New("no session name given and UTENA_SESSION_ID is not set")
	}
	id, err := strconv.ParseUint(envID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid UTENA_SESSION_ID %q", envID)
	}
	for i := range sessions {
		if sessions[i].ID == uint(id) {
			return &sessions[i], nil
		}
	}
	return nil, fmt.Errorf("no session with id %d", id)
}

func resolveSessionByName(sessions []session.Session, name string) (*session.Session, error) {
	var visible, hidden []*session.Session
	for i := range sessions {
		s := &sessions[i]
		if s.Name != name || !s.ListVisible(true) {
			continue
		}
		if s.IsHidden() {
			hidden = append(hidden, s)
		} else {
			visible = append(visible, s)
		}
	}
	matches := visible
	if len(matches) == 0 {
		matches = hidden
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no session named %q", name)
	case 1:
		return matches[0], nil
	default:
		ids := make([]string, len(matches))
		for i, s := range matches {
			ids[i] = fmt.Sprintf("%d (%s)", s.ID, s.WorkspaceDisplay())
		}
		return nil, fmt.Errorf("session name %q is ambiguous; matching session ids: %s (run from inside the session or rename one)", name, strings.Join(ids, ", "))
	}
}

func lookupSession(port, name string) (*session.Session, error) {
	sessions, err := fetchSessions(port)
	if err != nil {
		return nil, err
	}
	return resolveSession(sessions, name, os.Getenv(session.EnvSessionID))
}

func blockerRequest(method, url string, body any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s %s: status %d: %s", method, url, res.StatusCode, bytes.TrimSpace(msg))
	}
	return nil
}

func optionalName(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func sessionsBlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "block [name]",
		Short:        "Add a blocker to a session",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			kind, _ := cmd.Flags().GetString("kind")
			note, _ := cmd.Flags().GetString("note")
			url, _ := cmd.Flags().GetString("url")
			if !session.BlockerKind(kind).Valid() {
				return errors.New("--kind must be one of review, slack, other")
			}
			sess, err := lookupSession(port, optionalName(args))
			if err != nil {
				return err
			}
			return blockerRequest(http.MethodPost,
				fmt.Sprintf("%s/sessions/%d/blockers", daemonURL(port), sess.ID),
				session.AddBlockerRequest{Kind: session.BlockerKind(kind), Note: note, URL: url})
		},
	}
	cmd.Flags().String("kind", "", "review, slack or other")
	cmd.Flags().String("note", "", "what you're waiting on")
	cmd.Flags().String("url", "", "link to the PR or Slack message")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func sessionsBlockersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "blockers [name]",
		Short:        "List a session's blockers (id, kind, label, age)",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")
			sess, err := lookupSession(port, optionalName(args))
			if err != nil {
				return err
			}
			for _, b := range sess.Blockers {
				if !all && !b.IsOpen() {
					continue
				}
				line := fmt.Sprintf("%d\t%s\t%s\t%s", b.ID, b.Kind, b.Label(), common.TimeAgo(b.CreatedAt))
				if !b.IsOpen() {
					line += "\tresolved"
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "include resolved blockers")
	return cmd
}

func sessionsUnblockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "unblock [name] [blocker-id]",
		Short:        "Resolve a blocker (or all with --all)",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")

			var name, idArg string
			switch {
			case all && len(args) == 2:
				return errors.New("--all takes at most a session name")
			case all:
				name = optionalName(args)
			case len(args) == 2:
				name, idArg = args[0], args[1]
			case len(args) == 1:
				idArg = args[0]
			default:
				return errors.New("blocker id required (or pass --all)")
			}

			sess, err := lookupSession(port, name)
			if err != nil {
				return err
			}

			var ids []uint
			if all {
				for _, b := range sess.OpenBlockers() {
					ids = append(ids, b.ID)
				}
			} else {
				id, err := strconv.ParseUint(idArg, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid blocker id %q", idArg)
				}
				ids = []uint{uint(id)}
			}

			for _, id := range ids {
				if err := blockerRequest(http.MethodPut,
					fmt.Sprintf("%s/sessions/%d/blockers/%d/resolve", daemonURL(port), sess.ID, id), nil); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "resolve every open blocker")
	return cmd
}
