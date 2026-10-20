package tuios

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withRunUserRoot(t *testing.T, root string) {
	t.Helper()
	old := runUserRoot
	runUserRoot = root
	t.Cleanup(func() { runUserRoot = old })
}

func TestSocketPath_PrefersTuiosSocketAndXDG(t *testing.T) {
	t.Setenv("TUIOS_SOCKET", "/x/tuios.sock")
	assert.Equal(t, "/x/tuios.sock", socketPath())

	t.Setenv("TUIOS_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "/rt")
	assert.Equal(t, "/rt/tuios/tuios.sock", socketPath())
}

func TestSocketPath_UsesPrivateRunUserDir(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS has no /run/user")
	}
	t.Setenv("TUIOS_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	root := t.TempDir()
	withRunUserRoot(t, root)
	dir := filepath.Join(root, strconv.Itoa(os.Getuid()))
	require.NoError(t, os.Mkdir(dir, 0o700))

	assert.Equal(t, filepath.Join(dir, "tuios", "tuios.sock"), socketPath())

	require.NoError(t, os.Chmod(dir, 0o755))
	assert.Equal(t, filepath.Join("/tmp", fmt.Sprintf("tuios-%d", os.Getuid()), "tuios.sock"), socketPath())
}

func TestSocketPath_FallsBackToTmpWithoutRunUserDir(t *testing.T) {
	t.Setenv("TUIOS_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	withRunUserRoot(t, t.TempDir())

	assert.Equal(t, filepath.Join("/tmp", fmt.Sprintf("tuios-%d", os.Getuid()), "tuios.sock"), socketPath())
}
