package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	value, suffix := float64(n)/unit, "KiB"
	for _, s := range []string{"MiB", "GiB", "TiB"} {
		if value < unit {
			break
		}
		value, suffix = value/unit, s
	}
	return fmt.Sprintf("%.1f%s", value, suffix)
}

var meterEighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

func Meter(frac float64, width int, style lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	frac = math.Max(0, math.Min(frac, 1))
	eighths := int(math.Round(frac * float64(width*8)))
	full, part := eighths/8, eighths%8
	filled := strings.Repeat("█", full) + meterEighths[part]
	track := strings.Repeat("·", width-lipgloss.Width(filled))
	return style.Render(filled) + fg(theme.Current.SurfaceVariant).Render(track)
}

var sparkLevels = []rune("▁▂▃▄▅▆▇█")

func Sparkline(values []float64, width int) string {
	if width <= 0 || len(values) == 0 {
		return ""
	}
	if len(values) > width {
		values = values[len(values)-width:]
	}
	peak := 25.0
	for _, v := range values {
		peak = math.Max(peak, v)
	}
	out := make([]rune, len(values))
	for i, v := range values {
		level := int(math.Round(math.Max(v, 0) / peak * float64(len(sparkLevels)-1)))
		out[i] = sparkLevels[level]
	}
	return string(out)
}
