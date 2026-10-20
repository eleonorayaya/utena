package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	GlobalSetupScriptName = "worktree-setup"
	FileModeHeader        = "X-Utena-File-Mode"
	FileMissingHeader     = "X-Utena-File-Missing"
)

var ErrNoLeaderWorkspace = errors.New("workspace has no leader workspace id")

type SourceReader interface {
	ReadSourceFile(ctx context.Context, ws Workspace, relPath string) ([]byte, fs.FileMode, error)
	ReadGlobalSetupScript(ctx context.Context) ([]byte, fs.FileMode, error)
}

type LocalPathResolver interface {
	SourcePath(ws Workspace, relPath string) string
	GlobalSetupScriptPath() string
}

func ValidateRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("path must be relative")
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("path must not contain '..'")
	}
	return nil
}

type LocalSourceReader struct {
	globalScript string
}

var (
	_ SourceReader      = (*LocalSourceReader)(nil)
	_ LocalPathResolver = (*LocalSourceReader)(nil)
)

func NewLocalSourceReader(configDir string) *LocalSourceReader {
	if configDir == "" {
		return &LocalSourceReader{}
	}
	return &LocalSourceReader{globalScript: filepath.Join(configDir, GlobalSetupScriptName)}
}

func (r *LocalSourceReader) SourcePath(ws Workspace, relPath string) string {
	return filepath.Join(ws.Path, relPath)
}

func (r *LocalSourceReader) GlobalSetupScriptPath() string {
	return r.globalScript
}

func (r *LocalSourceReader) ReadSourceFile(_ context.Context, ws Workspace, relPath string) ([]byte, fs.FileMode, error) {
	if err := ValidateRelPath(relPath); err != nil {
		return nil, 0, fmt.Errorf("source path %q: %w", relPath, err)
	}
	return readLocalFile(r.SourcePath(ws, relPath))
}

func (r *LocalSourceReader) ReadGlobalSetupScript(_ context.Context) ([]byte, fs.FileMode, error) {
	if r.globalScript == "" {
		return nil, 0, fmt.Errorf("global %s: %w", GlobalSetupScriptName, fs.ErrNotExist)
	}
	return readLocalFile(r.globalScript)
}

func readLocalFile(path string) ([]byte, fs.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	if info.IsDir() {
		return nil, 0, fmt.Errorf("%s is a directory", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	return data, info.Mode().Perm(), nil
}
