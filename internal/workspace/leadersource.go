package workspace

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultLeaderTimeout = 30 * time.Second

type LeaderSourceReader struct {
	baseURL string
	client  *http.Client
}

var _ SourceReader = (*LeaderSourceReader)(nil)

func NewLeaderSourceReader(baseURL string, client *http.Client) *LeaderSourceReader {
	if client == nil {
		client = &http.Client{Timeout: defaultLeaderTimeout}
	}
	return &LeaderSourceReader{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (r *LeaderSourceReader) ReadSourceFile(ctx context.Context, ws Workspace, relPath string) ([]byte, fs.FileMode, error) {
	if ws.LeaderWorkspaceID == nil {
		return nil, 0, fmt.Errorf("workspace %q: %w", ws.Name, ErrNoLeaderWorkspace)
	}
	if err := ValidateRelPath(relPath); err != nil {
		return nil, 0, fmt.Errorf("source path %q: %w", relPath, err)
	}
	u := fmt.Sprintf("%s/workspaces/%d/files/%s", r.baseURL, *ws.LeaderWorkspaceID, escapeRelPath(relPath))
	return r.get(ctx, u, relPath)
}

func (r *LeaderSourceReader) ReadGlobalSetupScript(ctx context.Context) ([]byte, fs.FileMode, error) {
	return r.get(ctx, r.baseURL+"/workspaces/"+GlobalSetupScriptName, "global "+GlobalSetupScriptName)
}

func (r *LeaderSourceReader) get(ctx context.Context, u, what string) ([]byte, fs.FileMode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("leader unreachable reading %s: %w", what, err)
	}
	defer res.Body.Close()

	switch {
	case res.StatusCode == http.StatusOK:
		mode, err := parseFileMode(res.Header.Get(FileModeHeader))
		if err != nil {
			return nil, 0, fmt.Errorf("leader response for %s: %w", what, err)
		}
		data, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, 0, fmt.Errorf("leader response for %s: %w", what, err)
		}
		return data, mode, nil
	case res.StatusCode == http.StatusNotFound && res.Header.Get(FileMissingHeader) != "":
		return nil, 0, fmt.Errorf("%s on leader: %w", what, fs.ErrNotExist)
	default:
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, 0, fmt.Errorf("leader GET %s: status %d: %s", u, res.StatusCode, strings.TrimSpace(string(msg)))
	}
}

func parseFileMode(v string) (fs.FileMode, error) {
	if v == "" {
		return 0, fmt.Errorf("missing %s header", FileModeHeader)
	}
	n, err := strconv.ParseUint(v, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid %s header %q: %w", FileModeHeader, v, err)
	}
	return fs.FileMode(n).Perm(), nil
}

func FormatFileMode(mode fs.FileMode) string {
	return fmt.Sprintf("%04o", uint32(mode.Perm()))
}

func escapeRelPath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}
