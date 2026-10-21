package leader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{"http://127.0.0.1:3334", "http://127.0.0.1:41000/", "http://[::1]:5000"} {
		assert.NoError(t, ValidateURL(ok), ok)
	}
	for _, bad := range []string{"", "127.0.0.1:3334", "https://127.0.0.1:3334", "http://127.0.0.1", "http://example.com:80", "http://10.0.0.1:3334", "http://127.0.0.1:3334/api"} {
		assert.Error(t, ValidateURL(bad), bad)
	}
}

func TestAddress_SetTrimsSlash(t *testing.T) {
	a := NewAddress("http://127.0.0.1:1/")
	assert.Equal(t, "http://127.0.0.1:1", a.URL())

	a.Set("http://127.0.0.1:2/")
	assert.Equal(t, "http://127.0.0.1:2", a.URL())
}
