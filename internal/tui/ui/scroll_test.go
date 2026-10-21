package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScrollOffset(t *testing.T) {
	assert.Equal(t, 3, ScrollOffset(5, 3, 4, 20, 5))
	assert.Equal(t, 5, ScrollOffset(0, 9, 9, 20, 5))
	assert.Equal(t, 2, ScrollOffset(2, 3, 4, 20, 5))
	assert.Equal(t, 15, ScrollOffset(18, 18, 19, 20, 5))
	assert.Equal(t, 0, ScrollOffset(4, 4, 4, 3, 5))
}
