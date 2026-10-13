package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestTruncate(t *testing.T) {
	assert.Equal(t, "hello", Truncate("hello", 5))
	assert.Equal(t, "hell…", Truncate("hello world", 5))
	assert.Equal(t, "", Truncate("hello", 0))
	assert.LessOrEqual(t, lipgloss.Width(Truncate("日本語のセッション名", 7)), 7)
	assert.LessOrEqual(t, lipgloss.Width(Truncate("🚀🚀🚀🚀", 5)), 5)
}

func TestFitHeight(t *testing.T) {
	assert.Equal(t, 3, strings.Count(FitHeight("a", 3), "\n")+1)
	assert.Equal(t, "a\nb", FitHeight("a\nb\nc\nd", 2))
	assert.Equal(t, "a\nb", FitHeight("a\nb", 0))
}

func TestShortAgo(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	cases := map[time.Duration]string{
		10 * time.Second:    "now",
		-5 * time.Second:    "now",
		59 * time.Minute:    "59m",
		time.Hour:           "1h",
		23 * time.Hour:      "23h",
		24 * time.Hour:      "1d",
		6 * 24 * time.Hour:  "6d",
		7 * 24 * time.Hour:  "1w",
		30 * 24 * time.Hour: "4w",
	}
	for d, want := range cases {
		assert.Equal(t, want, shortAgo(now.Add(-d), now), d.String())
	}
	assert.Equal(t, "", ShortAgo(time.Time{}))
}

func TestHyperlink(t *testing.T) {
	link := Hyperlink("https://github.com/o/r/pull/1", "#1")
	assert.Equal(t, "\x1b]8;;https://github.com/o/r/pull/1\x1b\\#1\x1b]8;;\x1b\\", link)
	assert.Equal(t, 2, lipgloss.Width(link))
	assert.Equal(t, "#1", Hyperlink("", "#1"))

	styled := Hyperlink("https://x/1", lipgloss.NewStyle().Bold(true).Render("#1"))
	assert.Equal(t, 2, lipgloss.Width(styled))
}
