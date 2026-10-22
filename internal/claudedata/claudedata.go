package claudedata

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxRecordsForCWD = 200

func DataDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

type Project struct {
	Dir        string
	CWD        string
	SessionIDs []string
}

func Projects(dataDir string) ([]Project, error) {
	root := filepath.Join(dataDir, "projects")
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var projects []Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := readProject(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func readProject(dir string) (Project, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Project{}, err
	}
	p := Project{Dir: dir}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok || e.IsDir() || !validSessionID(id) {
			continue
		}
		p.SessionIDs = append(p.SessionIDs, id)
		if p.CWD == "" {
			p.CWD = transcriptCWD(filepath.Join(dir, e.Name()))
		}
	}
	return p, nil
}

func transcriptCWD(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for range maxRecordsForCWD {
		var rec struct {
			CWD string `json:"cwd"`
		}
		if err := dec.Decode(&rec); err != nil {
			return ""
		}
		if rec.CWD != "" {
			return rec.CWD
		}
	}
	return ""
}

func validSessionID(id string) bool {
	return id != "" && !strings.ContainsAny(id, `/\.`)
}

func Remove(dataDir string, p Project) error {
	var errs []error
	for _, id := range p.SessionIDs {
		for _, sub := range []string{"file-history", "session-env"} {
			if err := os.RemoveAll(filepath.Join(dataDir, sub, id)); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := os.RemoveAll(p.Dir); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func Within(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
