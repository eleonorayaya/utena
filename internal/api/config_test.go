package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigAddrBindsLoopback(t *testing.T) {
	assert.Equal(t, "127.0.0.1:3333", Config{Port: 3333}.Addr())
}
