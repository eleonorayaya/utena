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
)

const (
	projectFilePerm fs.FileMode = 0o644
	privateFilePerm fs.FileMode = 0o600
)

func editJSONObject(path string, perm fs.FileMode, createIfMissing bool, mutate func(raw map[string]any) error) error {
	name := filepath.Base(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("resolve %s: %w", name, err)
	}

	raw := map[string]any{}
	var original, before []byte
	data, err := os.ReadFile(path)
	exists := err == nil
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read %s: %w", name, err)
	default:
		original = data
		if raw, err = decodeJSONObject(data); err != nil {
			return fmt.Errorf("parse %s: %w", name, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s: %w", name, err)
		}
		perm = info.Mode().Perm()
		if before, err = encodeJSON(raw); err != nil {
			return fmt.Errorf("marshal %s: %w", name, err)
		}
	}

	if err := mutate(raw); err != nil {
		return err
	}
	out, err := encodeJSON(raw)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	if exists && (bytes.Equal(out, before) || bytes.Equal(out, original)) {
		return nil
	}
	if !exists && !createIfMissing && len(raw) == 0 {
		return nil
	}
	return writeFileAtomic(path, name, out, perm)
}

func decodeJSONObject(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after top-level object")
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, nil
}

func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeFileAtomic(path, name string, data []byte, perm fs.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s dir: %w", filepath.Base(dir), err)
	}
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return fmt.Errorf("create temp %s: %w", name, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write temp %s: %w", name, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp %s: %w", name, err)
	}
	if err = tmp.Chmod(perm); err != nil {
		return fmt.Errorf("chmod temp %s: %w", name, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp %s: %w", name, err)
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", name, err)
	}
	return nil
}

func childObject(parent map[string]any, key string) (map[string]any, error) {
	v, ok := parent[key]
	if !ok {
		child := map[string]any{}
		parent[key] = child
		return child, nil
	}
	child, isObj := v.(map[string]any)
	if !isObj {
		return nil, fmt.Errorf("%s is not an object", key)
	}
	return child, nil
}
