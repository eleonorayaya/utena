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

func TestAppOptions_PickerModeEnablesEscQuit(t *testing.T) {
	assert.Len(t, appOptions(func(k string) string {
		if k == "UTENA_PICKER" {
			return "1"
		}
		return ""
	}), 1)
	assert.Empty(t, appOptions(func(string) string { return "" }))
}

func TestReturnPickerFocus_FocusesRecordedWindow(t *testing.T) {
	d := wm.NewFakeDriver()
	env := map[string]string{"UTENA_PICKER": "1", "UTENA_PICKER_RETURN_WINDOW": "77"}

	returnPickerFocus(d, func(k string) string { return env[k] })

	assert.Equal(t, []string{"77"}, d.Focused())
}

func TestReturnPickerFocus_PopupModeIsNoop(t *testing.T) {
	d := wm.NewFakeDriver()
	env := map[string]string{"UTENA_POPUP": "1", "UTENA_PICKER_RETURN_WINDOW": "77"}

	returnPickerFocus(d, func(k string) string { return env[k] })

	assert.Empty(t, d.Focused())
}

func TestPickerCmd_RequiresTerminalCommand(t *testing.T) {
	cmd := pickerCmd()
	cmd.SetArgs([]string{"--app-id", "x"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	assert.Error(t, cmd.Execute())
}
