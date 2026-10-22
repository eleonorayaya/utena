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

func writeTranscript(t *testing.T, dataDir, id, cwd string, leading ...any) string {
	t.Helper()
	dir := filepath.Join(dataDir, "projects", ProjectDirName(cwd))
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

func TestProjectDirName(t *testing.T) {
	assert.Equal(t, "-Users-me-utena-sessions-a-b-c", ProjectDirName("/Users/me/utena-sessions/a.b_c"))
}

func TestProjectsUnder_ReadsCWDPastLeadingRecordsAndLongLines(t *testing.T) {
	data := t.TempDir()
	long := map[string]string{"type": "tool_result", "content": strings.Repeat("x", 200<<10)}
	writeTranscript(t, data, "abc", "/s/foo", map[string]string{"type": "mode"}, long)

	projects, err := ProjectsUnder(data, "/s/foo")
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "/s/foo", projects[0].CWD)
	assert.Equal(t, []string{"abc"}, projects[0].SessionIDs)
}

func TestProjectsUnder_MatchesCheckoutsButNotNameSiblings(t *testing.T) {
	data := t.TempDir()
	writeTranscript(t, data, "own", "/s/foo")
	writeTranscript(t, data, "sub", "/s/foo/repo")
	writeTranscript(t, data, "sib", "/s/foo-bar")
	writeTranscript(t, data, "other", "/s/bar")
	require.NoError(t, os.MkdirAll(filepath.Join(data, "projects", ProjectDirName("/s/foo")+"-mem", "memory"), 0o755))

	projects, err := ProjectsUnder(data, "/s/foo")
	require.NoError(t, err)
	var cwds []string
	for _, p := range projects {
		cwds = append(cwds, p.CWD)
	}
	assert.ElementsMatch(t, []string{"/s/foo", "/s/foo/repo"}, cwds)
}

func TestProjectsUnder_MissingDataDir(t *testing.T) {
	projects, err := ProjectsUnder(filepath.Join(t.TempDir(), "nope"), "/s/foo")
	require.NoError(t, err)
	assert.Empty(t, projects)
}

func TestRemove_DeletesProjectAndSessionKeyedData(t *testing.T) {
	data := t.TempDir()
	dir := writeTranscript(t, data, "abc", "/s/foo")
	for _, p := range []string{"file-history/abc", "session-env/abc", "file-history/other"} {
		require.NoError(t, os.MkdirAll(filepath.Join(data, p), 0o755))
	}

	projects, err := ProjectsUnder(data, "/s/foo")
	require.NoError(t, err)
	require.NoError(t, Remove(data, projects[0]))

	assert.NoDirExists(t, dir)
	assert.NoDirExists(t, filepath.Join(data, "file-history", "abc"))
	assert.NoDirExists(t, filepath.Join(data, "session-env", "abc"))
	assert.DirExists(t, filepath.Join(data, "file-history", "other"))
}
