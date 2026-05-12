package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureWorkspaceRoot_CreatesFileWhenMissing(t *testing.T) {
	root := t.TempDir()

	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatalf("EnsureWorkspaceRoot: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	allowWrite, _ := fs["allowWrite"].([]any)
	wantPaths := []string{filepath.Join(root, ".git"), filepath.Join(root, ".bare")}
	for _, want := range wantPaths {
		found := false
		for _, v := range allowWrite {
			if v == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("allowWrite missing %q; got %v", want, allowWrite)
		}
	}
}

func TestEnsureWorkspaceRoot_MergesIntoExistingSettings(t *testing.T) {
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), []byte(`{"custom": true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatalf("EnsureWorkspaceRoot: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out["custom"] != true {
		t.Fatalf("existing key 'custom' was removed")
	}
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	allowWrite, _ := fs["allowWrite"].([]any)
	wantPaths := []string{filepath.Join(root, ".git"), filepath.Join(root, ".bare")}
	for _, want := range wantPaths {
		found := false
		for _, v := range allowWrite {
			if v == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("allowWrite missing %q; got %v", want, allowWrite)
		}
	}
}

func TestEnsureWorkspaceRoot_DoesNotDuplicateAllowWrite(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatal(err)
	}
	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatalf("second call: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	allowWrite, _ := fs["allowWrite"].([]any)
	wantPaths := []string{filepath.Join(root, ".git"), filepath.Join(root, ".bare")}
	for _, want := range wantPaths {
		count := 0
		for _, v := range allowWrite {
			if v == want {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected %q once in allowWrite, got %d times", want, count)
		}
	}
}

func TestEnsureWorkspaceRoot_AddsNewDefaultsToExistingSettings(t *testing.T) {
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(claudeDir, "settings.local.json")
	gitPath := filepath.Join(root, ".git")
	barePath := filepath.Join(root, ".bare")
	initial := map[string]any{
		"custom": true,
		"sandbox": map[string]any{
			"filesystem": map[string]any{
				"allowWrite": []any{gitPath},
			},
		},
	}
	initialBytes, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settingsPath, initialBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatalf("EnsureWorkspaceRoot: %v", err)
	}

	raw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if out["custom"] != true {
		t.Fatalf("existing key 'custom' was removed")
	}
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	allowWrite, _ := fs["allowWrite"].([]any)

	gitCount, bareCount := 0, 0
	for _, v := range allowWrite {
		switch v {
		case gitPath:
			gitCount++
		case barePath:
			bareCount++
		}
	}
	if gitCount != 1 {
		t.Fatalf("expected %q once in allowWrite, got %d times: %v", gitPath, gitCount, allowWrite)
	}
	if bareCount != 1 {
		t.Fatalf("expected %q once in allowWrite (newly added), got %d times: %v", barePath, bareCount, allowWrite)
	}
}

func TestEnsureWorkspaceRoot_CreatesClaudeDir(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatalf("EnsureWorkspaceRoot: %v", err)
	}
	info, err := os.Stat(filepath.Join(root, ".claude"))
	if err != nil {
		t.Fatalf("stat .claude: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf(".claude is not a directory")
	}
}

func TestLinkWorktree_CreatesRelativeSymlink(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "feature-x")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := LinkWorktree(root, worktree); err != nil {
		t.Fatalf("LinkWorktree: %v", err)
	}

	linkPath := filepath.Join(worktree, ".claude", "settings.local.json")
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if filepath.IsAbs(target) {
		t.Fatalf("target should be relative, got %q", target)
	}
	resolved := filepath.Join(filepath.Dir(linkPath), target)
	rootSettings := filepath.Join(root, ".claude", "settings.local.json")
	gotAbs, _ := filepath.EvalSymlinks(resolved)
	wantAbs, _ := filepath.EvalSymlinks(rootSettings)
	if gotAbs != wantAbs {
		t.Fatalf("symlink resolves to %q, want %q", gotAbs, wantAbs)
	}
}

func TestLinkWorktree_PreservesExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "feature-y")
	wtClaude := filepath.Join(worktree, ".claude")
	if err := os.MkdirAll(wtClaude, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := []byte(`{"per-worktree": true}`)
	settingsPath := filepath.Join(wtClaude, "settings.local.json")
	if err := os.WriteFile(settingsPath, custom, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := LinkWorktree(root, worktree); err != nil {
		t.Fatalf("LinkWorktree: %v", err)
	}

	got, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("regular file was clobbered")
	}
}

func TestEnsureSessionRoot_CreatesFileWithGitAndBareForEachWorkspace(t *testing.T) {
	root := t.TempDir()
	workspaces := []string{
		filepath.Join(root, "repo-a"),
		filepath.Join(root, "repo-b"),
	}

	if err := EnsureSessionRoot(root, workspaces); err != nil {
		t.Fatalf("EnsureSessionRoot: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse: %v", err)
	}
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	allowWrite, _ := fs["allowWrite"].([]any)
	if len(allowWrite) != 4 {
		t.Fatalf("expected 4 allowWrite entries (.git + .bare per workspace), got %d: %v", len(allowWrite), allowWrite)
	}
	want := []string{
		filepath.Join(workspaces[0], ".git"),
		filepath.Join(workspaces[0], ".bare"),
		filepath.Join(workspaces[1], ".git"),
		filepath.Join(workspaces[1], ".bare"),
	}
	for _, w := range want {
		found := false
		for _, v := range allowWrite {
			if v == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("allowWrite missing %q; got %v", w, allowWrite)
		}
	}
}

func TestEnsureSessionRoot_IsIdempotent(t *testing.T) {
	root := t.TempDir()
	workspaces := []string{filepath.Join(root, "a")}
	if err := EnsureSessionRoot(root, workspaces); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSessionRoot(root, workspaces); err != nil {
		t.Fatalf("second call: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	allowWrite, _ := fs["allowWrite"].([]any)
	gitPath := filepath.Join(workspaces[0], ".git")
	count := 0
	for _, v := range allowWrite {
		if v == gitPath {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected git dir once after re-run, got %d times", count)
	}
}

func TestLinkWorktree_IdempotentOnExistingCorrectSymlink(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(root, "feature-z")
	if err := os.Mkdir(worktree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := LinkWorktree(root, worktree); err != nil {
		t.Fatal(err)
	}
	if err := LinkWorktree(root, worktree); err != nil {
		t.Fatalf("second call should be no-op: %v", err)
	}
}
