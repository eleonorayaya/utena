package router

import (
	"testing"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

type capturingModel struct{ capturing bool }

func (m capturingModel) Init() (capturingModel, tea.Cmd)          { return m, nil }
func (m capturingModel) Update(tea.Msg) (capturingModel, tea.Cmd) { return m, nil }
func (m capturingModel) View() string                             { return "" }
func (m capturingModel) Keys() help.KeyMap                        { return nil }
func (m capturingModel) CapturesText() bool                       { return m.capturing }

type plainModel struct{}

func (m plainModel) Init() (plainModel, tea.Cmd)          { return m, nil }
func (m plainModel) Update(tea.Msg) (plainModel, tea.Cmd) { return m, nil }
func (m plainModel) View() string                         { return "" }
func (m plainModel) Keys() help.KeyMap                    { return nil }

func TestViewAdapter_CapturesText(t *testing.T) {
	assert.True(t, (&ViewAdapter[capturingModel]{Model: capturingModel{capturing: true}}).CapturesText())
	assert.False(t, (&ViewAdapter[capturingModel]{Model: capturingModel{}}).CapturesText())
	assert.False(t, (&ViewAdapter[plainModel]{Model: plainModel{}}).CapturesText())
}
