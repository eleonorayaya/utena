package ui

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

var (
	kEnter = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch"))
	kNew   = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new"))
	kBlock = key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block"))
)

func TestFooter_RendersHintsAndMore(t *testing.T) {
	out := Footer([]key.Binding{kEnter, kNew}, Status{}, 80, true)
	assert.Equal(t, "enter switch · n new · ? more", out)
}

func TestFooter_DropsHintsThatDoNotFitButKeepsMore(t *testing.T) {
	out := Footer([]key.Binding{kEnter, kNew, kBlock}, Status{}, 22, true)
	assert.Equal(t, "enter switch · ? more", out)
	assert.LessOrEqual(t, lipgloss.Width(out), 22)
}

func TestFooter_WithoutMore(t *testing.T) {
	assert.Equal(t, "enter switch", Footer([]key.Binding{kEnter}, Status{}, 80, false))
}

func TestFooter_StatusReplacesHints(t *testing.T) {
	out := Footer([]key.Binding{kEnter}, Status{Text: "press d again to delete x", Kind: StatusWarning}, 80, true)
	assert.Equal(t, "press d again to delete x", out)
}

func TestFooter_TruncatesLongStatus(t *testing.T) {
	out := Footer(nil, Status{Text: "a very long error message that will not fit", Kind: StatusError}, 10, true)
	assert.Equal(t, 10, lipgloss.Width(out))
}

func TestFooter_SkipsDisabledAndHelpless(t *testing.T) {
	disabled := key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "gone"), key.WithDisabled())
	noHelp := key.NewBinding(key.WithKeys("y"))
	assert.Equal(t, "? more", Footer([]key.Binding{disabled, noHelp}, Status{}, 80, true))
}

func TestFooter_FlattensMultiLineStatus(t *testing.T) {
	out := Footer(nil, Status{Text: "first line\nsecond line", Kind: StatusError}, 80, true)
	assert.Equal(t, "first line second line", out)
}

func TestFooter_MoreFitsTinyWidth(t *testing.T) {
	assert.LessOrEqual(t, lipgloss.Width(Footer([]key.Binding{kEnter}, Status{}, 4, true)), 4)
}
