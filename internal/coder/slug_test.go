package coder

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlug(t *testing.T) {
	tests := []struct {
		name  string
		input string
		taken []string
		want  string
	}{
		{"simple", "fix-login", nil, "fix-login"},
		{"lowercases", "Fix-Login", nil, "fix-login"},
		{"underscores become hyphens", "fix_login_bug", nil, "fix-login-bug"},
		{"collapses runs", "a__--b..c", nil, "a-b-c"},
		{"trims hyphens", "--abc--", nil, "abc"},
		{"strips leading digits", "123-abc", nil, "abc"},
		{"strips leading digit run with letters after", "9lives", nil, "lives"},
		{"keeps inner digits", "pr-1234", nil, "pr-1234"},
		{"spaces and slashes", "eqt/My Feature", nil, "eqt-my-feature"},
		{"non ascii", "café-ü", nil, "caf"},
		{"empty", "", nil, "session"},
		{"only digits", "1234", nil, "session"},
		{"only symbols", "___", nil, "session"},
		{"truncates", strings.Repeat("a", 40), nil, strings.Repeat("a", 32)},
		{"truncation drops trailing hyphen", strings.Repeat("a", 31) + "-bbbb", nil, strings.Repeat("a", 31)},
		{"collision gets suffix", "fix", []string{"fix"}, "fix-2"},
		{"collision skips taken suffixes", "fix", []string{"fix", "fix-2", "fix-3"}, "fix-4"},
		{"unrelated taken names ignored", "fix", []string{"other"}, "fix"},
		{"collision on long name keeps length", strings.Repeat("a", 32), []string{strings.Repeat("a", 32)}, strings.Repeat("a", 30) + "-2"},
		{"collision truncation drops trailing hyphen", strings.Repeat("a", 29) + "-bb", []string{strings.Repeat("a", 29) + "-bb"}, strings.Repeat("a", 29) + "-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Slug(tt.input, tt.taken)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.True(t, ValidSlug(got), "slug %q must be valid", got)
		})
	}
}

func TestSlug_AlwaysValid(t *testing.T) {
	inputs := []string{"a", "-", "a-", "-a", "a--b", "A B C", "x" + strings.Repeat("-y", 30), "0a0", "Ünïcødé", "tab\tname", "a.b/c:d"}
	for _, in := range inputs {
		got, err := Slug(in, nil)
		require.NoError(t, err)
		assert.True(t, ValidSlug(got), "Slug(%q) = %q", in, got)
		again, err := Slug(in, []string{got})
		require.NoError(t, err)
		assert.True(t, ValidSlug(again), "Slug(%q) with collision = %q", in, again)
		assert.NotEqual(t, got, again)
	}
}

func TestValidSlug(t *testing.T) {
	assert.True(t, ValidSlug("a"))
	assert.True(t, ValidSlug("a1-b2"))
	assert.False(t, ValidSlug(""))
	assert.False(t, ValidSlug("1a"))
	assert.False(t, ValidSlug("-a"))
	assert.False(t, ValidSlug("a-"))
	assert.False(t, ValidSlug("a--b"))
	assert.False(t, ValidSlug("A"))
	assert.False(t, ValidSlug(strings.Repeat("a", 33)))
}
