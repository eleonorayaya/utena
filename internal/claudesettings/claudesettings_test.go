package claudesettings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureWorkspaceRoot_CreatesFileWhenMissing(t *testing.T) {
	root := t.TempDir()

	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatalf("EnsureWorkspaceRoot: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings.local.json: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty settings.local.json")
	}
	want, err := defaultSettingsJSON(root)
	if err != nil {
		t.Fatalf("defaultSettingsJSON: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("contents do not match expected:\ngot:  %s\nwant: %s", got, want)
	}
}

func TestEnsureWorkspaceRoot_DoesNotOverwriteExisting(t *testing.T) {
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := []byte(`{"custom": true}`)
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.local.json"), custom, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatalf("EnsureWorkspaceRoot: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(claudeDir, "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("user file was overwritten: %s", got)
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
