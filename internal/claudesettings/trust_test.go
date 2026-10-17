package claudesettings

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadGlobalConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return raw
}

func projectEntry(t *testing.T, raw map[string]any, project string) map[string]any {
	t.Helper()
	projects, ok := raw["projects"].(map[string]any)
	if !ok {
		t.Fatalf("projects missing or not an object: %v", raw["projects"])
	}
	entry, ok := projects[project].(map[string]any)
	if !ok {
		t.Fatalf("entry for %s missing: %v", project, projects)
	}
	return entry
}

func TestTrustProject_CreatesMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	raw := loadGlobalConfig(t, path)
	if got := projectEntry(t, raw, "/sessions/a")["hasTrustDialogAccepted"]; got != true {
		t.Fatalf("hasTrustDialogAccepted = %v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestTrustProject_PreservesOtherKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	initial := `{
  "oauthAccount": {"email": "x@example.com"},
  "url": "https://a.example/?x=1&y=<2>",
  "projects": {
    "/other": {"hasTrustDialogAccepted": false, "allowedTools": ["Bash"]},
    "/sessions/a": {"history": [1, 2], "hasTrustDialogAccepted": false}
  }
}`
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"https://a.example/?x=1&y=<2>"`)) {
		t.Fatalf("url was escaped or lost: %s", data)
	}
	if !bytes.Contains(data, []byte("\n  \"projects\"")) {
		t.Fatalf("expected 2-space indent: %s", data)
	}
	raw := loadGlobalConfig(t, path)
	if email := raw["oauthAccount"].(map[string]any)["email"]; email != "x@example.com" {
		t.Fatalf("oauthAccount lost: %v", raw["oauthAccount"])
	}
	other := projectEntry(t, raw, "/other")
	if other["hasTrustDialogAccepted"] != false || len(other["allowedTools"].([]any)) != 1 {
		t.Fatalf("other project changed: %v", other)
	}
	entry := projectEntry(t, raw, "/sessions/a")
	if entry["hasTrustDialogAccepted"] != true {
		t.Fatalf("not trusted: %v", entry)
	}
	if len(entry["history"].([]any)) != 2 {
		t.Fatalf("entry fields lost: %v", entry)
	}
}

func TestTrustProject_CreatesProjectsMapAndEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(`{"numStartups": 3}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	raw := loadGlobalConfig(t, path)
	if raw["numStartups"] != float64(3) {
		t.Fatalf("numStartups lost: %v", raw)
	}
	if projectEntry(t, raw, "/sessions/a")["hasTrustDialogAccepted"] != true {
		t.Fatalf("not trusted: %v", raw)
	}
}

func TestTrustProject_AlreadyTrustedIsNotRewritten(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	initial := []byte(`{"projects":{"/sessions/a":{"hasTrustDialogAccepted":true}},"z":1}`)
	if err := os.WriteFile(path, initial, 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, initial) {
		t.Fatalf("file rewritten: %s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(old) {
		t.Fatalf("mtime changed: %v != %v", info.ModTime(), old)
	}
}

func TestTrustProject_PreservesPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", info.Mode().Perm())
	}
}

func TestTrustProject_LargeIntegerRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(`{"big": 12345678901234567890, "frac": 1.50}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("12345678901234567890")) {
		t.Fatalf("large integer changed: %s", data)
	}
	if !bytes.Contains(data, []byte("1.50")) {
		t.Fatalf("number text changed: %s", data)
	}
}

func TestTrustProject_MalformedLeavesFileUntouched(t *testing.T) {
	cases := map[string]string{
		"syntax":   `{"projects": `,
		"trailing": `{} junk`,
		"empty":    ``,
		"array":    `[]`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".claude.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			if err := TrustProject(path, "/sessions/a"); err == nil {
				t.Fatal("expected error")
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != content {
				t.Fatalf("file changed: %q", data)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("stray files left: %v", entries)
			}
		})
	}
}

func TestTrustProject_NullFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(path, []byte(`null`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	if projectEntry(t, loadGlobalConfig(t, path), "/sessions/a")["hasTrustDialogAccepted"] != true {
		t.Fatal("not trusted")
	}
}

func TestTrustProject_WritesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".claude.json")
	if err := os.Symlink("real.json", path); err != nil {
		t.Fatal(err)
	}

	if err := TrustProject(path, "/sessions/a"); err != nil {
		t.Fatalf("TrustProject: %v", err)
	}

	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced by %v", info.Mode())
	}
	if projectEntry(t, loadGlobalConfig(t, target), "/sessions/a")["hasTrustDialogAccepted"] != true {
		t.Fatal("target not updated")
	}
}

func TestTrustProject_NonObjectProjectsErrors(t *testing.T) {
	cases := map[string]string{
		"projects": `{"projects": []}`,
		"entry":    `{"projects": {"/sessions/a": "yes"}}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), ".claude.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}

			err := TrustProject(path, "/sessions/a")
			if err == nil || !strings.Contains(err.Error(), "not an object") {
				t.Fatalf("err = %v", err)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != content {
				t.Fatalf("file changed: %q", data)
			}
		})
	}
}

func TestTrustProject_ConcurrentCallsKeepEveryEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".claude.json")
	projects := []string{"/s/1", "/s/2", "/s/3", "/s/4", "/s/5", "/s/6", "/s/7", "/s/8"}
	errs := make(chan error, len(projects))
	for _, p := range projects {
		go func() { errs <- TrustProject(path, p) }()
	}
	for range projects {
		if err := <-errs; err != nil {
			t.Fatalf("TrustProject: %v", err)
		}
	}

	raw := loadGlobalConfig(t, path)
	for _, p := range projects {
		if projectEntry(t, raw, p)["hasTrustDialogAccepted"] != true {
			t.Fatalf("%s not trusted", p)
		}
	}
}

func TestClaudeGlobalConfigPath_UsesConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	got, err := ClaudeGlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, ".claude.json"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestClaudeGlobalConfigPath_DefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", home)

	got, err := ClaudeGlobalConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".claude.json"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
