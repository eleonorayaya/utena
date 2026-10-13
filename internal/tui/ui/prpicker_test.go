package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/assert"
)

func pr(n int, state git.PRState, url string) git.PullRequest {
	return git.PullRequest{Number: n, Title: "title", State: state, HTMLURL: url}
}

func TestOpenablePRs_FiltersAndOrders(t *testing.T) {
	got := OpenablePRs([]git.PullRequest{
		pr(1, git.PRStateDraft, "u1"),
		pr(2, git.PRStateMerged, "u2"),
		pr(3, git.PRStateOpen, "u3"),
		pr(4, git.PRStateOpen, ""),
		pr(5, git.PRStateOpen, "u5"),
	})
	var nums []int
	for _, p := range got {
		nums = append(nums, p.Number)
	}
	assert.Equal(t, []int{5, 3, 1}, nums)
}

func TestPRPicker_NavigateAndChoose(t *testing.T) {
	p := NewPRPicker([]git.PullRequest{pr(5, git.PRStateOpen, "u5"), pr(3, git.PRStateOpen, "u3")})
	assert.True(t, p.IsOpen())
	assert.Equal(t, 3, p.Height(20))

	p, url := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, "", url)
	p, url = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "u3", url)
	assert.False(t, p.IsOpen())
	assert.Equal(t, 0, p.Height(20))
}

func TestPRPicker_EscCancels(t *testing.T) {
	p := NewPRPicker([]git.PullRequest{pr(5, git.PRStateOpen, "u5"), pr(3, git.PRStateOpen, "u3")})
	p, url := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, "", url)
	assert.False(t, p.IsOpen())
}

func TestPRPicker_CursorClamps(t *testing.T) {
	p := NewPRPicker([]git.PullRequest{pr(5, git.PRStateOpen, "u5"), pr(3, git.PRStateOpen, "u3")})
	p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	for range 5 {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	_, url := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "u3", url)
}

func TestPRPicker_ViewFitsWidth(t *testing.T) {
	long := git.PullRequest{Number: 4847, Title: strings.Repeat("long title ", 20), State: git.PRStateOpen, HTMLURL: "u"}
	p := NewPRPicker([]git.PullRequest{long, pr(1, git.PRStateDraft, "u1")})
	out := p.View(40, 20)
	assert.Equal(t, p.Height(20), strings.Count(out, "\n")+1)
	for _, line := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 40)
	}
}

func TestPRPicker_SyncClosesWhenPRsChange(t *testing.T) {
	prs := []git.PullRequest{pr(5, git.PRStateOpen, "u5"), pr(3, git.PRStateOpen, "u3")}
	p := NewPRPicker(prs)
	assert.True(t, p.Sync(prs).IsOpen())

	merged := []git.PullRequest{pr(5, git.PRStateOpen, "u5"), pr(3, git.PRStateMerged, "u3")}
	assert.False(t, p.Sync(merged).IsOpen())
	assert.False(t, p.Sync(nil).IsOpen())
}

func TestPRPicker_WindowsAroundCursorWhenRowsAreShort(t *testing.T) {
	prs := []git.PullRequest{pr(9, git.PRStateOpen, "u9"), pr(8, git.PRStateOpen, "u8"), pr(7, git.PRStateOpen, "u7"), pr(6, git.PRStateOpen, "u6")}
	p := NewPRPicker(prs)
	assert.Equal(t, 2, p.Height(2))
	assert.Equal(t, 0, p.Height(0))

	out := p.View(40, 2)
	assert.Equal(t, 2, strings.Count(out, "\n")+1)
	assert.Contains(t, out, "#9")
	assert.NotContains(t, out, "#7")

	for range 3 {
		p, _ = p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	out = p.View(40, 2)
	assert.Contains(t, out, "#6")
	assert.NotContains(t, out, "#9")
}
