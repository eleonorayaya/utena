package claudedata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeTranscript(t *testing.T, dataDir, project, id, cwd string, leading ...any) string {
	t.Helper()
	dir := filepath.Join(dataDir, "projects", project)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	var lines []string
	for _, rec := range append(leading, map[string]string{"type": "user", "cwd": cwd}) {
		b, err := json.Marshal(rec)
		require.NoError(t, err)
		lines = append(lines, string(b))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644))
	return dir
}

func TestProjects_ReadsCWDPastLeadingRecordsAndLongLines(t *testing.T) {
	data := t.TempDir()
	long := map[string]string{"type": "tool_result", "content": strings.Repeat("x", 200<<10)}
	writeTranscript(t, data, "-s-foo", "abc", "/s/foo", map[string]string{"type": "mode"}, long)

	projects, err := Projects(data)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "/s/foo", projects[0].CWD)
	assert.Equal(t, []string{"abc"}, projects[0].SessionIDs)
	assert.False(t, projects[0].ModTime.IsZero())
}

func TestProjects_MemoryOnlyDirHasNoCWD(t *testing.T) {
	data := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(data, "projects", "-s-mem", "memory"), 0o755))

	projects, err := Projects(data)
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Empty(t, projects[0].CWD)
}

func TestProjects_MissingDataDir(t *testing.T) {
	projects, err := Projects(filepath.Join(t.TempDir(), "nope"))
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestRemove_DeletesProjectAndSessionKeyedData(t *testing.T) {
	data := t.TempDir()
	dir := writeTranscript(t, data, "-s-foo", "abc", "/s/foo")
	for _, p := range []string{"file-history/abc", "session-env/abc", "file-history/other"} {
		require.NoError(t, os.MkdirAll(filepath.Join(data, p), 0o755))
	}

	projects, err := Projects(data)
	require.NoError(t, err)
	require.NoError(t, Remove(data, projects[0]))

	assert.NoDirExists(t, dir)
	assert.NoDirExists(t, filepath.Join(data, "file-history", "abc"))
	assert.NoDirExists(t, filepath.Join(data, "session-env", "abc"))
	assert.DirExists(t, filepath.Join(data, "file-history", "other"))
}

func TestWithin(t *testing.T) {
	assert.True(t, Within("/s/foo", "/s/foo"))
	assert.True(t, Within("/s/foo/repo", "/s/foo"))
	assert.False(t, Within("/s/foo-bar", "/s/foo"))
	assert.False(t, Within("", "/s/foo"))
}
