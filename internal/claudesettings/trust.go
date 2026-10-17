package claudesettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

const globalConfigFileName = ".claude.json"

func ClaudeGlobalConfigPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, globalConfigFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, globalConfigFileName), nil
}

var trustMu sync.Mutex

func TrustProject(globalConfigPath, projectPath string) error {
	trustMu.Lock()
	defer trustMu.Unlock()
	raw, mode, err := readGlobalConfig(globalConfigPath)
	if err != nil {
		return err
	}

	projects := map[string]any{}
	if existing, ok := raw["projects"]; ok {
		obj, isObj := existing.(map[string]any)
		if !isObj {
			return fmt.Errorf("%s: projects is not an object", globalConfigFileName)
		}
		projects = obj
	}
	entry := map[string]any{}
	if existing, ok := projects[projectPath]; ok {
		obj, isObj := existing.(map[string]any)
		if !isObj {
			return fmt.Errorf("%s: project %s is not an object", globalConfigFileName, projectPath)
		}
		entry = obj
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); trusted {
		return nil
	}

	entry["hasTrustDialogAccepted"] = true
	projects[projectPath] = entry
	raw["projects"] = projects
	return writeGlobalConfig(globalConfigPath, raw, mode)
}

func readGlobalConfig(path string) (map[string]any, fs.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, 0o600, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", globalConfigFileName, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("stat %s: %w", globalConfigFileName, err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, 0, fmt.Errorf("parse %s: %w", globalConfigFileName, err)
	}
	if raw == nil {
		return nil, 0, fmt.Errorf("parse %s: top-level value is not an object", globalConfigFileName)
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, 0, fmt.Errorf("parse %s: unexpected data after top-level object", globalConfigFileName)
	}
	return raw, info.Mode().Perm(), nil
}

func writeGlobalConfig(path string, raw map[string]any, mode fs.FileMode) (err error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(raw); err != nil {
		return fmt.Errorf("marshal %s: %w", globalConfigFileName, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), globalConfigFileName+".*")
	if err != nil {
		return fmt.Errorf("create temp %s: %w", globalConfigFileName, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("write temp %s: %w", globalConfigFileName, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp %s: %w", globalConfigFileName, err)
	}
	if err = tmp.Chmod(mode); err != nil {
		return fmt.Errorf("chmod temp %s: %w", globalConfigFileName, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp %s: %w", globalConfigFileName, err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", globalConfigFileName, err)
	}
	return nil
}
