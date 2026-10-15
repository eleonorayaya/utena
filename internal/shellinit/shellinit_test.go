package shellinit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScript_ExportsBrowserInsideTuios(t *testing.T) {
	s := Script()
	idx := strings.Index(s, `export BROWSER="$HOME/.local/bin/utena-open"`)
	assert.Greater(t, idx, strings.Index(s, `if [ -n "$TUIOS_ENV" ]; then`))
	assert.Less(t, idx, strings.LastIndex(s, "fi"))
	assert.Contains(t, s, `[ -x "$HOME/.local/bin/utena-open" ]`)
}
