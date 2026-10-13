package ui

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func OpenablePRs(prs []git.PullRequest) []git.PullRequest {
	var out []git.PullRequest
	for _, p := range prs {
		if p.HTMLURL == "" {
			continue
		}
		if p.State == git.PRStateOpen || p.State == git.PRStateDraft {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State == git.PRStateOpen
		}
		return out[i].Number > out[j].Number
	})
	return out
}

var (
	pickerUp     = key.NewBinding(key.WithKeys("up", "k"))
	pickerDown   = key.NewBinding(key.WithKeys("down", "j"))
	pickerChoose = key.NewBinding(key.WithKeys("enter"))
	pickerCancel = key.NewBinding(key.WithKeys("esc"))
)

type PRPicker struct {
	prs    []git.PullRequest
	cursor int
	open   bool
}

func NewPRPicker(prs []git.PullRequest) PRPicker {
	return PRPicker{prs: prs, open: len(prs) > 0}
}

func (p PRPicker) IsOpen() bool {
	return p.open
}

func (p PRPicker) Close() PRPicker {
	p.open = false
	return p
}

func (p PRPicker) Update(msg tea.KeyMsg) (PRPicker, string) {
	if !p.open {
		return p, ""
	}
	switch {
	case key.Matches(msg, pickerUp):
		if p.cursor > 0 {
			p.cursor--
		}
	case key.Matches(msg, pickerDown):
		if p.cursor < len(p.prs)-1 {
			p.cursor++
		}
	case key.Matches(msg, pickerChoose):
		p.open = false
		return p, p.prs[p.cursor].HTMLURL
	case key.Matches(msg, pickerCancel):
		p.open = false
	}
	return p, ""
}

func (p PRPicker) Height() int {
	if !p.open {
		return 0
	}
	return len(p.prs) + 1
}

func (p PRPicker) View(width int) string {
	if !p.open {
		return ""
	}
	lines := []string{""}
	for i, pr := range p.prs {
		gutter := "  "
		if i == p.cursor {
			gutter = fg(theme.Current.Primary).Render("▌ ")
		}
		num := fmt.Sprintf("#%-5d", pr.Number)
		badge := PRBadge(pr.State)
		titleW := max(width-2-lipgloss.Width(num)-2-lipgloss.Width(badge)-1, 1)
		title := Truncate(pr.Title, titleW)
		title += strings.Repeat(" ", max(titleW-lipgloss.Width(title), 0))
		line := gutter + num + "  " + title + " " + badge
		if lipgloss.Width(line) > width {
			line = gutter + Truncate(num+"  "+pr.Title, max(width-2, 1))
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

type OpenURLFailedMsg struct{ Err error }

func OpenURL(url string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("open", url)
		if err := cmd.Start(); err != nil {
			return OpenURLFailedMsg{Err: err}
		}
		go cmd.Wait()
		return nil
	}
}

func (p PRPicker) Sync(prs []git.PullRequest) PRPicker {
	if !p.open {
		return p
	}
	fresh := OpenablePRs(prs)
	if len(fresh) != len(p.prs) {
		return p.Close()
	}
	for i := range fresh {
		if fresh[i].HTMLURL != p.prs[i].HTMLURL || fresh[i].State != p.prs[i].State {
			return p.Close()
		}
	}
	return p
}
