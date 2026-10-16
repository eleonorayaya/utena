package api

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eleonorayaya/utena/internal/aerospace"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigAddrBindsLoopback(t *testing.T) {
	assert.Equal(t, "127.0.0.1:3333", Config{Port: 3333}.Addr())
}

func clearWMEnv(t *testing.T) {
	t.Helper()
	t.Setenv("UTENA_WM", "")
	t.Setenv("UTENA_BROWSER_WORKSPACE", "")
	t.Setenv("UTENA_PARK_WORKSPACE", "")
	t.Setenv("UTENA_BROWSER", "")
	t.Setenv("UTENA_DATA_DIR", t.TempDir())
}

func TestLoadConfig_WindowManagerDefaults(t *testing.T) {
	clearWMEnv(t)

	cfg, err := LoadConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "aerospace", cfg.WM)
	assert.Equal(t, "2", cfg.BrowserWorkspace)
	assert.Equal(t, "P", cfg.ParkWorkspace)
}

func TestLoadConfig_WindowManagerFlags(t *testing.T) {
	clearWMEnv(t)

	cfg, err := LoadConfig([]string{"--wm", "none", "--browser-workspace", "B", "--park-workspace", "Z"})
	require.NoError(t, err)
	assert.Equal(t, "none", cfg.WM)
	assert.Equal(t, "B", cfg.BrowserWorkspace)
	assert.Equal(t, "Z", cfg.ParkWorkspace)
}

func TestLoadConfig_WindowManagerEnvOverridesFlags(t *testing.T) {
	t.Setenv("UTENA_WM", "none")
	t.Setenv("UTENA_BROWSER_WORKSPACE", "3")
	t.Setenv("UTENA_PARK_WORKSPACE", "Q")

	cfg, err := LoadConfig([]string{"--wm", "aerospace", "--browser-workspace", "B", "--park-workspace", "Z"})
	require.NoError(t, err)
	assert.Equal(t, "none", cfg.WM)
	assert.Equal(t, "3", cfg.BrowserWorkspace)
	assert.Equal(t, "Q", cfg.ParkWorkspace)
}

func TestLoadConfig_RejectsUnknownWM(t *testing.T) {
	clearWMEnv(t)

	_, err := LoadConfig([]string{"--wm", "yabai"})
	assert.ErrorContains(t, err, "yabai")
}

func TestLoadConfig_RejectsUnknownWMFromEnv(t *testing.T) {
	clearWMEnv(t)
	t.Setenv("UTENA_WM", "i3")

	_, err := LoadConfig(nil)
	assert.ErrorContains(t, err, "i3")
}

func TestNewWMDriver(t *testing.T) {
	assert.Equal(t, wm.None{}, newWMDriver(""))
	assert.Equal(t, wm.None{}, newWMDriver("none"))
	assert.IsType(t, &aerospace.Driver{}, newWMDriver("aerospace"))
}

func TestLoadConfig_BrowserDefaultsToChrome(t *testing.T) {
	clearWMEnv(t)

	cfg, err := LoadConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "chrome", cfg.Browser)
}

func TestLoadConfig_BrowserFromDaemonFile(t *testing.T) {
	clearWMEnv(t)
	dir := t.TempDir()
	t.Setenv("UTENA_DATA_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "daemon.json"), []byte(`{"browser":"firefox"}`), 0o644))

	cfg, err := LoadConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "firefox", cfg.Browser)

	cfg, err = LoadConfig([]string{"--browser", "chrome"})
	require.NoError(t, err)
	assert.Equal(t, "chrome", cfg.Browser)

	t.Setenv("UTENA_BROWSER", "chrome")
	cfg, err = LoadConfig(nil)
	require.NoError(t, err)
	assert.Equal(t, "chrome", cfg.Browser)
}

func TestLoadConfig_RejectsBadDaemonFile(t *testing.T) {
	clearWMEnv(t)
	dir := t.TempDir()
	t.Setenv("UTENA_DATA_DIR", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "daemon.json"), []byte(`{`), 0o644))

	_, err := LoadConfig(nil)
	assert.ErrorContains(t, err, "daemon.json")
}

func TestLoadConfig_RejectsUnknownBrowser(t *testing.T) {
	clearWMEnv(t)

	_, err := LoadConfig([]string{"--browser", "safari"})
	assert.ErrorContains(t, err, "safari")
}

func TestNewBrowserDriver(t *testing.T) {
	assert.Equal(t, "chrome", newBrowserDriver("chrome").(browser.HTTPDriver).Name())
	assert.Equal(t, "firefox", newBrowserDriver("firefox").(browser.HTTPDriver).Name())
}
