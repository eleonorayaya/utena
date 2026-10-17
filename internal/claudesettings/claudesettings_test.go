package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestEnsureMultiSessionGuide_CreatesFileWhenMissing(t *testing.T) {
	root := t.TempDir()
	checkouts := []SessionCheckout{
		{Subdir: "utena", WorkspaceName: "utena"},
		{Subdir: "other", WorkspaceName: "other-project"},
	}
	if err := EnsureMultiSessionGuide(root, checkouts); err != nil {
		t.Fatalf("EnsureMultiSessionGuide: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	for _, want := range []string{"`utena/`", "`other/`", "other-project", "only**", "canonical"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("CLAUDE.md missing %q; got:\n%s", want, body)
		}
	}
}

func TestEnsureMultiSessionGuide_PreservesUserCLAUDEMd(t *testing.T) {
	root := t.TempDir()
	userContent := []byte("# my project notes\n")
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), userContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMultiSessionGuide(root, []SessionCheckout{{Subdir: "a", WorkspaceName: "a"}}); err != nil {
		t.Fatalf("EnsureMultiSessionGuide: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(userContent) {
		t.Fatalf("user CLAUDE.md was overwritten; got:\n%s", got)
	}
}

func TestEnsureMultiSessionGuide_RewritesUtenaAuthoredFile(t *testing.T) {
	root := t.TempDir()
	if err := EnsureMultiSessionGuide(root, []SessionCheckout{{Subdir: "a", WorkspaceName: "a"}}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureMultiSessionGuide(root, []SessionCheckout{
		{Subdir: "a", WorkspaceName: "a"},
		{Subdir: "b", WorkspaceName: "b"},
	}); err != nil {
		t.Fatalf("second call: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "`b/`") {
		t.Fatalf("expected rewrite to include new checkout; got:\n%s", body)
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

func readSettings(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return out
}

func writeSettings(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.local.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyBundles_Plugins(t *testing.T) {
	tests := []struct {
		name       string
		existing   string
		managed    []string
		enabled    []string
		want       map[string]any
		wantAbsent bool
	}{
		{
			name:    "creates file with enabled plugins",
			managed: []string{"go@m", "pdf@m"},
			enabled: []string{"go@m"},
			want:    map[string]any{"go@m": true},
		},
		{
			name:     "removes unselected managed plugins",
			existing: `{"enabledPlugins":{"go@m":true,"pdf@m":true}}`,
			managed:  []string{"go@m", "pdf@m"},
			enabled:  []string{"pdf@m"},
			want:     map[string]any{"pdf@m": true},
		},
		{
			name:     "leaves unmanaged plugins alone",
			existing: `{"enabledPlugins":{"mine@m":false,"go@m":true}}`,
			managed:  []string{"go@m"},
			enabled:  nil,
			want:     map[string]any{"mine@m": false},
		},
		{
			name:       "drops the key when nothing remains",
			existing:   `{"enabledPlugins":{"go@m":true}}`,
			managed:    []string{"go@m"},
			enabled:    nil,
			want:       nil,
			wantAbsent: true,
		},
		{
			name:     "treats a null file as empty",
			existing: "null",
			managed:  []string{"go@m"},
			enabled:  []string{"go@m"},
			want:     map[string]any{"go@m": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.existing != "" {
				writeSettings(t, root, tt.existing)
			}
			if err := ApplyBundles(root, BundleSettings{ManagedPlugins: tt.managed, Plugins: tt.enabled}); err != nil {
				t.Fatalf("ApplyBundles: %v", err)
			}
			settings := readSettings(t, root)
			if _, ok := settings["enabledPlugins"]; ok && tt.wantAbsent {
				t.Fatalf("enabledPlugins present, want absent: %v", settings)
			}
			got, _ := settings["enabledPlugins"].(map[string]any)
			if len(got) != len(tt.want) {
				t.Fatalf("enabledPlugins = %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("enabledPlugins = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestApplyBundles_PluginsPreservesSandbox(t *testing.T) {
	root := t.TempDir()
	if err := EnsureSessionRoot(root, []string{"/ws"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyBundles(root, BundleSettings{ManagedPlugins: []string{"go@m"}, Plugins: []string{"go@m"}}); err != nil {
		t.Fatal(err)
	}
	out := readSettings(t, root)
	sandbox, _ := out["sandbox"].(map[string]any)
	fs, _ := sandbox["filesystem"].(map[string]any)
	if allow, _ := fs["allowWrite"].([]any); len(allow) != 2 {
		t.Fatalf("allowWrite = %v, want 2 entries", allow)
	}
}

func TestApplyBundles_PluginsNoFileAndNothingEnabledWritesNothing(t *testing.T) {
	root := t.TempDir()
	if err := ApplyBundles(root, BundleSettings{ManagedPlugins: []string{"go@m"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "settings.local.json")); !os.IsNotExist(err) {
		t.Fatalf("expected no settings file, stat err = %v", err)
	}
}

func writeMcpJSON(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".mcp.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMcpJSON(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		t.Fatalf("read .mcp.json: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse .mcp.json: %v", err)
	}
	return out
}

func TestApplyBundles_McpServers(t *testing.T) {
	zoo := json.RawMessage(`{"command":"zoo","args":["mcp"]}`)
	ha := json.RawMessage(`{"type":"http","url":"https://ha"}`)
	tests := []struct {
		name         string
		mcp          string
		settings     string
		managed      []string
		selected     map[string]json.RawMessage
		wantServers  []string
		wantApproved []any
		wantMcpKeys  []string
		wantSetKeys  []string
	}{
		{
			name:         "writes selected servers to both files",
			managed:      []string{"zoo", "ha"},
			selected:     map[string]json.RawMessage{"zoo": zoo, "ha": ha},
			wantServers:  []string{"ha", "zoo"},
			wantApproved: []any{"ha", "zoo"},
			wantMcpKeys:  []string{"mcpServers"},
			wantSetKeys:  []string{"enabledMcpjsonServers"},
		},
		{
			name:         "removes managed servers that are not selected",
			mcp:          `{"mcpServers":{"zoo":{"command":"zoo"},"ha":{"type":"http","url":"https://ha"}}}`,
			settings:     `{"enabledMcpjsonServers":["zoo","ha"]}`,
			managed:      []string{"zoo", "ha"},
			selected:     map[string]json.RawMessage{"ha": ha},
			wantServers:  []string{"ha"},
			wantApproved: []any{"ha"},
			wantMcpKeys:  []string{"mcpServers"},
			wantSetKeys:  []string{"enabledMcpjsonServers"},
		},
		{
			name:         "leaves unmanaged servers and approvals alone",
			mcp:          `{"mcpServers":{"mine":{"command":"mine"},"zoo":{"command":"old"}}}`,
			settings:     `{"enabledMcpjsonServers":["mine","zoo","other"]}`,
			managed:      []string{"zoo", "ha"},
			selected:     map[string]json.RawMessage{"zoo": zoo},
			wantServers:  []string{"mine", "zoo"},
			wantApproved: []any{"mine", "other", "zoo"},
			wantMcpKeys:  []string{"mcpServers"},
			wantSetKeys:  []string{"enabledMcpjsonServers"},
		},
		{
			name:        "drops emptied keys and keeps the rest",
			mcp:         `{"mcpServers":{"zoo":{"command":"zoo"}},"other":1}`,
			settings:    `{"enabledMcpjsonServers":["zoo"],"enabledPlugins":{"go@m":true},"sandbox":{"filesystem":{"allowWrite":["/x"]}}}`,
			managed:     []string{"zoo"},
			wantMcpKeys: []string{"other"},
			wantSetKeys: []string{"enabledPlugins", "sandbox"},
		},
		{
			name:         "treats a null .mcp.json as empty",
			mcp:          "null",
			managed:      []string{"zoo"},
			selected:     map[string]json.RawMessage{"zoo": zoo},
			wantServers:  []string{"zoo"},
			wantApproved: []any{"zoo"},
			wantMcpKeys:  []string{"mcpServers"},
			wantSetKeys:  []string{"enabledMcpjsonServers"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.mcp != "" {
				writeMcpJSON(t, root, tt.mcp)
			}
			if tt.settings != "" {
				writeSettings(t, root, tt.settings)
			}
			if err := ApplyBundles(root, BundleSettings{ManagedServers: tt.managed, Servers: tt.selected}); err != nil {
				t.Fatalf("ApplyBundles: %v", err)
			}
			mcp := readMcpJSON(t, root)
			if got := sortedKeys(mcp); !slices.Equal(got, tt.wantMcpKeys) {
				t.Fatalf(".mcp.json keys = %v, want %v", got, tt.wantMcpKeys)
			}
			servers, _ := mcp["mcpServers"].(map[string]any)
			if got := sortedKeys(servers); !slices.Equal(got, tt.wantServers) {
				t.Fatalf("mcpServers = %v, want %v", got, tt.wantServers)
			}
			for name, def := range tt.selected {
				var want any
				if err := json.Unmarshal(def, &want); err != nil {
					t.Fatal(err)
				}
				gotDef, _ := json.Marshal(servers[name])
				wantDef, _ := json.Marshal(want)
				if string(gotDef) != string(wantDef) {
					t.Fatalf("mcpServers[%s] = %s, want %s", name, gotDef, wantDef)
				}
			}
			settings := readSettings(t, root)
			if got := sortedKeys(settings); !slices.Equal(got, tt.wantSetKeys) {
				t.Fatalf("settings keys = %v, want %v", got, tt.wantSetKeys)
			}
			approved, _ := settings["enabledMcpjsonServers"].([]any)
			if len(approved) != len(tt.wantApproved) {
				t.Fatalf("enabledMcpjsonServers = %v, want %v", approved, tt.wantApproved)
			}
			for i := range approved {
				if approved[i] != tt.wantApproved[i] {
					t.Fatalf("enabledMcpjsonServers = %v, want %v", approved, tt.wantApproved)
				}
			}
		})
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestApplyBundles_NothingSelectedCreatesNothing(t *testing.T) {
	root := t.TempDir()
	if err := ApplyBundles(root, BundleSettings{ManagedServers: []string{"zoo"}}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{".mcp.json", ".claude"} {
		if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Fatalf("expected no %s, stat err = %v", p, err)
		}
	}
}

func TestApplyBundles_MalformedMcpJSONSkipsSettings(t *testing.T) {
	root := t.TempDir()
	writeMcpJSON(t, root, "{not json")
	err := ApplyBundles(root, BundleSettings{ManagedServers: []string{"zoo"}, Servers: map[string]json.RawMessage{"zoo": json.RawMessage(`{"command":"zoo"}`)}})
	if err == nil || !strings.HasPrefix(err.Error(), "parse .mcp.json: ") {
		t.Fatalf("err = %v, want parse .mcp.json error", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("expected no .claude dir, stat err = %v", err)
	}
}

func TestApplyBundles_MalformedSettingsErrors(t *testing.T) {
	root := t.TempDir()
	writeSettings(t, root, "{not json")
	err := ApplyBundles(root, BundleSettings{ManagedServers: []string{"zoo"}, Servers: map[string]json.RawMessage{"zoo": json.RawMessage(`{"command":"zoo"}`)}})
	if err == nil || !strings.HasPrefix(err.Error(), "parse settings.local.json: ") {
		t.Fatalf("err = %v, want parse settings error", err)
	}
}

func TestApplyBundles_UnchangedRunDoesNotRewrite(t *testing.T) {
	root := t.TempDir()
	selected := map[string]json.RawMessage{"zoo": json.RawMessage(`{"command":"zoo"}`)}
	if err := ApplyBundles(root, BundleSettings{ManagedServers: []string{"zoo"}, Servers: selected}); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, ".mcp.json"), filepath.Join(root, ".claude", "settings.local.json")}
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	before := make([][]byte, len(paths))
	for i, p := range paths {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		before[i] = data
	}
	if err := ApplyBundles(root, BundleSettings{ManagedServers: []string{"zoo"}, Servers: selected}); err != nil {
		t.Fatal(err)
	}
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(before[i]) {
			t.Fatalf("%s changed:\n%s\nwant:\n%s", p, data, before[i])
		}
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if !info.ModTime().Equal(old) {
			t.Fatalf("%s was rewritten: mtime %v, want %v", p, info.ModTime(), old)
		}
	}
}

func TestApplyBundles_DefinitionRoundTrips(t *testing.T) {
	root := t.TempDir()
	def := json.RawMessage(`{"type":"http","url":"https://x","headers":{"Authorization":"Bearer ${TOKEN}"}}`)
	if err := ApplyBundles(root, BundleSettings{ManagedServers: []string{"x"}, Servers: map[string]json.RawMessage{"x": def}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "mcpServers": {
    "x": {
      "type": "http",
      "url": "https://x",
      "headers": {
        "Authorization": "Bearer ${TOKEN}"
      }
    }
  }
}
`
	if string(raw) != want {
		t.Fatalf(".mcp.json =\n%s\nwant:\n%s", raw, want)
	}
}

func TestApplyBundles_LargeIntegerSurvives(t *testing.T) {
	root := t.TempDir()
	def := json.RawMessage(`{"command":"x","timeout":12345678901234567890}`)
	if err := ApplyBundles(root, BundleSettings{ManagedServers: []string{"x"}, Servers: map[string]json.RawMessage{"x": def}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"timeout": 12345678901234567890`) {
		t.Fatalf(".mcp.json lost precision:\n%s", raw)
	}
}

func TestApplyBundles_RejectsNonObjectDefinitions(t *testing.T) {
	for _, def := range []string{"", "null", " null ", "[]", `"x"`, "1", "{not json"} {
		t.Run(def, func(t *testing.T) {
			root := t.TempDir()
			selected := map[string]json.RawMessage{
				"good": json.RawMessage(`{"command":"good"}`),
				"bad":  json.RawMessage(def),
			}
			err := ApplyBundles(root, BundleSettings{ManagedPlugins: []string{"go@m"}, Plugins: []string{"go@m"}, ManagedServers: []string{"good", "bad"}, Servers: selected})
			if err == nil || !strings.Contains(err.Error(), "bad") {
				t.Fatalf("err = %v, want error naming bad", err)
			}
			for _, p := range []string{".mcp.json", ".claude"} {
				if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
					t.Fatalf("expected no %s, stat err = %v", p, err)
				}
			}
		})
	}
}

func TestApplyBundles_MalformedMcpJSONStillWritesPlugins(t *testing.T) {
	root := t.TempDir()
	writeMcpJSON(t, root, "{not json")
	err := ApplyBundles(root, BundleSettings{
		ManagedPlugins: []string{"go@m"},
		Plugins:        []string{"go@m"},
		ManagedServers: []string{"zoo"},
		Servers:        map[string]json.RawMessage{"zoo": json.RawMessage(`{"command":"zoo"}`)},
	})
	if err == nil || !strings.HasPrefix(err.Error(), "parse .mcp.json: ") {
		t.Fatalf("err = %v, want parse .mcp.json error", err)
	}
	settings := readSettings(t, root)
	if got := sortedKeys(settings); !slices.Equal(got, []string{"enabledPlugins"}) {
		t.Fatalf("settings keys = %v, want [enabledPlugins]", got)
	}
}

func TestApplyBundles_BothFilesFailingJoinsOnOneLine(t *testing.T) {
	root := t.TempDir()
	writeMcpJSON(t, root, "{not json")
	writeSettings(t, root, "{not json")
	err := ApplyBundles(root, BundleSettings{ManagedPlugins: []string{"go@m"}, Plugins: []string{"go@m"}})
	if err == nil || !strings.Contains(err.Error(), ".mcp.json") || !strings.Contains(err.Error(), "settings.local.json") {
		t.Fatalf("err = %v, want both files named", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("err has a newline: %q", err)
	}
}

func TestEnsureWorkspaceRoot_UnchangedFileIsNotReformatted(t *testing.T) {
	root := t.TempDir()
	body := `{"sandbox":{"filesystem":{"allowWrite":["` + filepath.Join(root, ".git") + `","` + filepath.Join(root, ".bare") + `"]}}}`
	writeSettings(t, root, body)

	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(root, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("file rewritten:\n%s", got)
	}
}

func TestEnsureWorkspaceRoot_WritesThroughSymlinkAndKeepsMode(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "shared.json")
	if err := os.WriteFile(target, []byte(`{"custom":true}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".claude", "settings.local.json")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := EnsureWorkspaceRoot(root); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced by %v", info.Mode())
	}
	info, err = os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v, want 0640", info.Mode().Perm())
	}
	out := readSettings(t, root)
	if out["custom"] != true || out["sandbox"] == nil {
		t.Fatalf("settings = %v", out)
	}
}
