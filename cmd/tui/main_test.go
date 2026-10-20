package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
