package claudedata

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/eleonorayaya/utena/internal/common"
)

const maxRecordsForCWD = 200

type Project struct {
	Dir        string
	CWD        string
	SessionIDs []string
}

func ProjectDirName(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, cwd)
}

func ProjectsUnder(dataDir, root string) ([]Project, error) {
	projectsDir := filepath.Join(dataDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prefix := ProjectDirName(root)
	var projects []Project
	for _, e := range entries {
		if !e.IsDir() || (e.Name() != prefix && !strings.HasPrefix(e.Name(), prefix+"-")) {
			continue
		}
		p, err := readProject(filepath.Join(projectsDir, e.Name()))
		if err != nil || !common.PathWithin(p.CWD, root) {
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
