package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisableResponseCompression_SendsNoAcceptEncoding(t *testing.T) {
	transport := http.DefaultTransport.(*http.Transport)
	t.Cleanup(func() { transport.DisableCompression = false })

	var acceptEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding = r.Header.Get("Accept-Encoding")
	}))
	t.Cleanup(srv.Close)

	disableResponseCompression()
	res, err := http.Get(srv.URL)
	require.NoError(t, err)
	res.Body.Close()
	assert.Empty(t, acceptEncoding)
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestAppOptions_PickerModeEnablesEscQuit(t *testing.T) {
	assert.Len(t, appOptions(envOf(map[string]string{"UTENA_PICKER": "1"})), 1)
	assert.Empty(t, appOptions(envOf(nil)))
}

func TestAppOptions_ParkWorkspaceEnablesParking(t *testing.T) {
	env := envOf(map[string]string{"UTENA_PICKER": "1", "UTENA_PICKER_PARK": "P"})

	assert.Len(t, appOptions(env), 2)
	assert.Len(t, programOptions(env), 2)
	assert.Empty(t, programOptions(envOf(map[string]string{"UTENA_PICKER": "1"})))
}

func TestReturnPickerFocus_PopupModeIsNoop(t *testing.T) {
	d := wm.NewFakeDriver()

	returnPickerFocus(d, envOf(map[string]string{"UTENA_POPUP": "1"}))

	assert.Empty(t, d.Focused())
}

func TestPickerCmd_RequiresTerminalCommand(t *testing.T) {
	cmd := pickerCmd()
	cmd.SetArgs([]string{"--app-id", "x"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	assert.Error(t, cmd.Execute())
}
