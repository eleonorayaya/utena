package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	assert.Equal(t, "512B", FormatBytes(512))
	assert.Equal(t, "1.5KiB", FormatBytes(1536))
	assert.Equal(t, "10.0MiB", FormatBytes(10<<20))
	assert.Equal(t, "3.0GiB", FormatBytes(3<<30))
}

func TestMeter(t *testing.T) {
	assert.Equal(t, "··········", ansi.Strip(Meter(0, 10, lipgloss.NewStyle())))
	assert.Equal(t, "█████·····", ansi.Strip(Meter(0.5, 10, lipgloss.NewStyle())))
	assert.Equal(t, "██▌·", ansi.Strip(Meter(0.625, 4, lipgloss.NewStyle())))
	assert.Equal(t, "████", ansi.Strip(Meter(3, 4, lipgloss.NewStyle())))
}

func TestSparkline(t *testing.T) {
	assert.Equal(t, "", Sparkline(nil, 5))
	assert.Equal(t, "▁▁▁", Sparkline([]float64{0, 0, 0}, 5))
	assert.Equal(t, "▁▅█", Sparkline([]float64{0, 50, 100}, 5))
	assert.Equal(t, "▅█", Sparkline([]float64{0, 50, 100}, 2))
}
