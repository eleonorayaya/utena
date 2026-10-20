package coder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type FollowerSession struct {
	session.Session
	AttentionStatus claude.ClaudeSessionStatus `json:"attention_status,omitempty"`
}

type FollowerClient struct {
	baseURL string
	http    *http.Client
}

func NewFollowerClient(baseURL string, client *http.Client) *FollowerClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &FollowerClient{baseURL: strings.TrimRight(baseURL, "/"), http: client}
}

type followerError struct {
	status int
	body   string
}

func (e *followerError) Error() string {
	return fmt.Sprintf("follower returned %d: %s", e.status, e.body)
}

func isNotFound(err error) bool {
	var fe *followerError
	return errors.As(err, &fe) && fe.status == http.StatusNotFound
}

func (c *FollowerClient) do(ctx context.Context, method, path string, body any, out any, ok ...int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("follower %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	accepted := ok
	if len(accepted) == 0 {
		accepted = []int{http.StatusOK}
	}
	matched := false
	for _, code := range accepted {
		matched = matched || res.StatusCode == code
	}
	if !matched {
		data, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return &followerError{status: res.StatusCode, body: strings.TrimSpace(string(data))}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("follower %s %s: decode: %w", method, path, err)
	}
	return nil
}

func (c *FollowerClient) Ping(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/bundles", nil, nil)
}

func (c *FollowerClient) LinkWorkspace(ctx context.Context, path string, leaderWorkspaceID uint) (uint, error) {
	var resp workspace.WorkspaceListResponse
	req := workspace.AddWorkspaceRequest{Path: path, LeaderWorkspaceID: &leaderWorkspaceID}
	if err := c.do(ctx, http.MethodPost, "/workspaces", req, &resp, http.StatusOK, http.StatusCreated); err != nil {
		return 0, err
	}
	for _, ws := range resp.Workspaces {
		if ws.Path == path {
			return ws.ID, nil
		}
	}
	return 0, fmt.Errorf("follower did not return workspace %s", path)
}

func (c *FollowerClient) CreateSession(ctx context.Context, req session.CreateSessionRequest) (*FollowerSession, error) {
	var out FollowerSession
	if err := c.do(ctx, http.MethodPost, "/sessions", req, &out, http.StatusOK, http.StatusCreated, http.StatusAccepted); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *FollowerClient) GetSession(ctx context.Context, id uint) (*FollowerSession, error) {
	var out FollowerSession
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/sessions/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *FollowerClient) ActivateSession(ctx context.Context, id uint) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/sessions/%d/activate", id), nil, nil)
}

func (c *FollowerClient) ArchiveSession(ctx context.Context, id uint) error {
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/sessions/%d/archive", id), nil, nil)
}

func (c *FollowerClient) DeleteSession(ctx context.Context, id uint) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/sessions/%d?force=true", id), nil, nil, http.StatusOK, http.StatusNoContent)
}
