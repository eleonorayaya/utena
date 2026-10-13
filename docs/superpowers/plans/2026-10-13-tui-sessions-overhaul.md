# TUI Sessions Screen Overhaul Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild the utena sessions screen as a sectioned, column-based list with a focused detail panel, standard footer/help chrome, clickable PR links and an `o` open-PR action, built on a new shared `internal/tui/ui` component package.

**Architecture:** A new `internal/tui/ui` package holds pure rendering helpers (glyphs, PR badges, section headers, a span/cell row layout, footer, PR picker, hyperlinks, time and truncation). The app owns a one-line footer fed by an optional `ui.StatusReporter` on the active view. `sessionlist` and `sessiondetail` are rebuilt on top of these. Grouping and sorting are pure functions over `[]session.Session`. No daemon or API changes.

**Tech Stack:** Go, bubbletea v1.3.10, bubbles v0.21.0 (`help`, `key`), lipgloss v1.1.0, testify.

**Spec:** `docs/superpowers/specs/2026-10-13-tui-sessions-overhaul-design.md`

## Global Constraints

- Run every build, test and lint through Task from the repo root: `task test`, `task fmt`, `task lint`. Never run `go` directly and never `cd` into subdirectories.
- Do not add code comments (repo CLAUDE.md rule).
- Every commit message ends with the trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Section order: `Needs you`, `Working`, `Idle`, `Blocked`, `Hidden`. Empty sections are omitted.
- Section membership: Hidden = `IsHidden()`; Blocked = `IsBlocked()` and not hidden; Needs you = `needs_attention` or `ready_for_review`; Working = `working`; Idle = `idle`, `done`, or no Claude sessions. Within a section, sort by `LastUsedAt` descending (stable).
- List columns and widths: gutter 2, status glyph 2, name flex, attached 2, PRs 7, last used 4 (right-aligned).
- Glyphs: `!` needs_attention, `✓` ready_for_review, `~` working, `✓` done (muted), `·` idle. Attached `◆`. Ready-PR count `●N`, draft count `◌N`. PR badges `● open`, `◌ draft`, `⊕ merged`, `✕ closed`.
- Claude status words: `Needs your input`, `Ready for review`, `Working`, `Idle`, `Done`.
- Short time: `now` (< 1m), `Nm`, `Nh`, `Nd`, `Nw`.
- Footer short help for the session list: `enter switch · o open PR · n new · b block · a archive · ? more`.
- Empty states: list `No sessions — press n to create one`; panel `Select a session`; workspace with no PRs `no pull requests`.
- New theme JSON keys: `status_attention`, `status_review`, `status_working`, `status_done`, `status_idle`.
- The debug view moves from `?` to `ctrl+g`; `?` toggles full help.

## Review Focus

1. **Long or wide session names / PR titles** (CJK, emoji, 80+ chars): no rendered line may exceed its pane width, or the layout wraps and pushes the footer off-screen. Pinned in Task 8 and Task 10.
2. **Tiny popups** (e.g. 40×6, or fewer rows than headers + sessions): no panic, the list never renders more lines than its height, and the selected row stays visible. Pinned in Task 7.
3. **Typing `?` in a text form** (new session name, blocker note): it must type a `?`, not open help. Pinned in Task 5.
4. **The selected session changes or disappears while the PR picker is open:** the picker closes rather than opening a stale PR. Pinned in Task 9.
5. **A PR with an empty `HTMLURL`:** never offered by `o`, and the PR number renders as plain text with no broken link escape. Pinned in Task 6 and Task 10.

---

## File Structure

| Path | Responsibility |
|------|----------------|
| `internal/tui/theme/theme.go` (modify) | Add five semantic status colour tokens |
| `internal/tui/ui/text.go` (create) | `Truncate`, `FitHeight`, `ShortAgo`, `Hyperlink` |
| `internal/tui/ui/status.go` (create) | `ClaudeGlyph`, `ClaudeStatusText`, `PRBadge`, `PRCounts`, `PRCountSpans`, `SectionHeader` |
| `internal/tui/ui/row.go` (create) | `Span`, `Cell`, `Align`, `Row` column layout |
| `internal/tui/ui/footer.go` (create) | `Status`, `StatusKind`, `StatusReporter`, `MoreKey`, `Footer` |
| `internal/tui/ui/prpicker.go` (create) | `OpenablePRs`, `PRPicker`, `OpenURL`, `OpenURLFailedMsg` |
| `internal/tui/router/*` (modify) | `Status()` on `ViewEntry`/adapter/router, `ActiveView()`, debug key → `ctrl+g` |
| `internal/tui/app.go`, `helpmodel.go`, `keys.go` (modify) | Footer, full-help toggle, height accounting |
| `internal/tui/sessionlist/sections.go` (create) | Section kinds, sort, row layout |
| `internal/tui/sessionlist/rows.go` (create) | Row rendering |
| `internal/tui/sessionlist/sessionlist.go`, `keys.go` (modify) | Model, keys, view, picker wiring |
| `internal/tui/sessiondetail/prgroups.go` (create) | Group PRs by workspace |
| `internal/tui/sessiondetail/panel.go` (create) | Panel rendering (moved out of `model.go`) |
| `internal/tui/sessiondetail/model.go`, `keys.go` (modify) | Model, keys, status, picker wiring |

---

### Task 1: Semantic status colour tokens

**Files:**
- Modify: `internal/tui/theme/theme.go`
- Create: `internal/tui/theme/theme_test.go`

**Interfaces:**
- Produces: `theme.Theme` fields `StatusAttention`, `StatusReview`, `StatusWorking`, `StatusDone`, `StatusIdle` (all `lipgloss.Color`).

`merge` copies fields **by index** between `Theme` and `themeFile`, so the new fields must be appended at the **end of both structs in the same order**.

- [ ] **Step 1: Write the failing test**

```go
package theme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefault_StatusTokensFallBackToExistingColours(t *testing.T) {
	d := Default()
	assert.Equal(t, d.Primary, d.StatusAttention)
	assert.Equal(t, d.AccentMint, d.StatusReview)
	assert.Equal(t, d.AccentLavender, d.StatusWorking)
	assert.Equal(t, d.TextMuted, d.StatusDone)
	assert.Equal(t, d.TextMuted, d.StatusIdle)
}

func TestLoad_OverridesStatusTokensAndKeepsOthers(t *testing.T) {
	t.Cleanup(func() { Current = Default() })
	path := filepath.Join(t.TempDir(), "theme.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"status_review":"#00ff00","primary":"#111111"}`), 0o644))

	require.NoError(t, Load(path))

	assert.Equal(t, lipgloss.Color("#00ff00"), Current.StatusReview)
	assert.Equal(t, lipgloss.Color("#111111"), Current.Primary)
	assert.Equal(t, Default().AccentLavender, Current.StatusWorking)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `d.StatusAttention undefined`.

- [ ] **Step 3: Implement**

Append to the end of `Theme`:

```go
	StatusAttention    lipgloss.Color
	StatusReview       lipgloss.Color
	StatusWorking      lipgloss.Color
	StatusDone         lipgloss.Color
	StatusIdle         lipgloss.Color
```

Append to the end of `Default()`'s literal:

```go
		StatusAttention:    lipgloss.Color("#e6537a"),
		StatusReview:       lipgloss.Color("#5fafa5"),
		StatusWorking:      lipgloss.Color("#a87bb7"),
		StatusDone:         lipgloss.Color("#8a7873"),
		StatusIdle:         lipgloss.Color("#8a7873"),
```

Append to the end of `themeFile`:

```go
	StatusAttention    *string `json:"status_attention"`
	StatusReview       *string `json:"status_review"`
	StatusWorking      *string `json:"status_working"`
	StatusDone         *string `json:"status_done"`
	StatusIdle         *string `json:"status_idle"`
```

Then fix the fallback semantics. A user theme that overrides `primary` but not `status_attention` should still get the new primary. In `Load`, replace `merge(&t, &f)` with:

```go
	merge(&t, &f)
	inheritStatusTokens(&t, &f)
```

and add:

```go
func inheritStatusTokens(t *Theme, f *themeFile) {
	if f.StatusAttention == nil {
		t.StatusAttention = t.Primary
	}
	if f.StatusReview == nil {
		t.StatusReview = t.AccentMint
	}
	if f.StatusWorking == nil {
		t.StatusWorking = t.AccentLavender
	}
	if f.StatusDone == nil {
		t.StatusDone = t.TextMuted
	}
	if f.StatusIdle == nil {
		t.StatusIdle = t.TextMuted
	}
}
```

Add a test for it to `theme_test.go`:

```go
func TestLoad_StatusTokensInheritOverriddenBaseColours(t *testing.T) {
	t.Cleanup(func() { Current = Default() })
	path := filepath.Join(t.TempDir(), "theme.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"primary":"#123456"}`), 0o644))

	require.NoError(t, Load(path))

	assert.Equal(t, lipgloss.Color("#123456"), Current.StatusAttention)
}
```

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/theme
git commit -m "feat(tui): add semantic status colour tokens to theme

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `ui` text helpers

**Files:**
- Create: `internal/tui/ui/text.go`
- Create: `internal/tui/ui/text_test.go`

**Interfaces:**
- Produces:
  - `func Truncate(s string, width int) string`
  - `func FitHeight(s string, h int) string` — exactly `h` lines (pads or cuts); returns `s` unchanged when `h <= 0`.
  - `func ShortAgo(t time.Time) string` — `""` for the zero time.
  - `func Hyperlink(url, text string) string` — OSC 8; returns `text` unchanged when `url == ""`.

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — package `ui` has no non-test files / undefined symbols.

- [ ] **Step 3: Implement**

```go
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > width-1 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func FitHeight(s string, h int) string {
	if h <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func ShortAgo(t time.Time) string {
	return shortAgo(t, time.Now())
}

func shortAgo(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	}
}

func Hyperlink(url, text string) string {
	if url == "" {
		return text
	}
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}
```

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS. If `TestHyperlink` width assertions fail, `lipgloss.Width` isn't stripping OSC 8. In that case, change both width checks in later tasks to measure plain text, and stop to report it before continuing.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ui
git commit -m "feat(tui): add ui text helpers (truncate, fit height, short time, hyperlink)

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: `ui` row layout

**Files:**
- Create: `internal/tui/ui/row.go`
- Create: `internal/tui/ui/row_test.go`

**Interfaces:**
- Consumes: `Truncate` (Task 2).
- Produces:
  ```go
  type Span struct { Text string; Style lipgloss.Style }
  func (s Span) Render() string
  type Align int
  const ( AlignLeft Align = iota; AlignRight )
  type Cell struct { Spans []Span; Width int; Align Align }
  func Row(cells []Cell, width int, bg lipgloss.TerminalColor) string
  ```
  `Width == 0` means flex. Flex cells share the remaining width equally, and the first flex cell gets any remainder. `bg == nil` means no background. Otherwise every span and padding gets that background, so the whole row is highlighted. Span `Text` must be plain text.

- [ ] **Step 1: Write the failing tests**

```go
package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func plain(text string, width int) Cell {
	return Cell{Spans: []Span{{Text: text}}, Width: width}
}

func TestRow_ExactWidth(t *testing.T) {
	out := Row([]Cell{plain("ab", 4), plain("name", 0), {Spans: []Span{{Text: "5m"}}, Width: 4, Align: AlignRight}}, 20, nil)
	assert.Equal(t, 20, lipgloss.Width(out))
	assert.Equal(t, "ab  name          5m", out)
}

func TestRow_TruncatesFlexCell(t *testing.T) {
	out := Row([]Cell{plain("x", 2), plain("a-very-long-session-name", 0), plain("1h", 3)}, 12, nil)
	assert.Equal(t, 12, lipgloss.Width(out))
	assert.Contains(t, out, "…")
}

func TestRow_MultipleSpansInOneCell(t *testing.T) {
	out := Row([]Cell{{Spans: []Span{{Text: "●1"}, {Text: " "}, {Text: "◌2"}}, Width: 7}}, 7, nil)
	assert.Equal(t, "●1 ◌2  ", out)
}

func TestRow_SpansTruncateAtCellWidth(t *testing.T) {
	out := Row([]Cell{{Spans: []Span{{Text: "abc"}, {Text: "defgh"}}, Width: 5}}, 5, nil)
	assert.Equal(t, 5, lipgloss.Width(out))
}

func TestRow_FixedCellsWiderThanRowDoNotPanic(t *testing.T) {
	out := Row([]Cell{plain("abcdef", 6), plain("name", 0), plain("xyz", 3)}, 5, nil)
	assert.LessOrEqual(t, lipgloss.Width(out), 9)
}

func TestRow_BackgroundDoesNotChangeWidth(t *testing.T) {
	out := Row([]Cell{plain("a", 2), plain("b", 0)}, 10, lipgloss.Color("#ff0000"))
	assert.Equal(t, 10, lipgloss.Width(out))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `undefined: Row`.

- [ ] **Step 3: Implement**

```go
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Span struct {
	Text  string
	Style lipgloss.Style
}

func (s Span) Render() string {
	return s.Style.Render(s.Text)
}

type Align int

const (
	AlignLeft Align = iota
	AlignRight
)

type Cell struct {
	Spans []Span
	Width int
	Align Align
}

func Row(cells []Cell, width int, bg lipgloss.TerminalColor) string {
	widths := cellWidths(cells, width)
	var b strings.Builder
	for i, c := range cells {
		b.WriteString(renderCell(c, widths[i], bg))
	}
	return b.String()
}

func cellWidths(cells []Cell, width int) []int {
	widths := make([]int, len(cells))
	fixed, flex := 0, 0
	for i, c := range cells {
		if c.Width > 0 {
			widths[i] = c.Width
			fixed += c.Width
		} else {
			flex++
		}
	}
	if flex == 0 {
		return widths
	}
	remaining := max(width-fixed, 0)
	share, extra := remaining/flex, remaining%flex
	for i, c := range cells {
		if c.Width > 0 {
			continue
		}
		widths[i] = share + extra
		extra = 0
	}
	return widths
}

func renderCell(c Cell, width int, bg lipgloss.TerminalColor) string {
	if width <= 0 {
		return ""
	}
	withBg := func(s lipgloss.Style) lipgloss.Style {
		if bg == nil {
			return s
		}
		return s.Background(bg)
	}

	var parts []string
	used := 0
	for _, sp := range c.Spans {
		if used >= width {
			break
		}
		text := sp.Text
		if lipgloss.Width(text) > width-used {
			text = Truncate(text, width-used)
		}
		used += lipgloss.Width(text)
		parts = append(parts, withBg(sp.Style).Render(text))
	}

	pad := withBg(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(width-used, 0)))
	if c.Align == AlignRight {
		return pad + strings.Join(parts, "")
	}
	return strings.Join(parts, "") + pad
}
```

Note: `withBg(lipgloss.NewStyle()).Render("")` returns `""`, so a zero-width pad is safe.

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ui
git commit -m "feat(tui): add ui row/cell column layout

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `ui` status visuals (glyphs, PR badges, section header)

**Files:**
- Create: `internal/tui/ui/status.go`
- Create: `internal/tui/ui/status_test.go`

**Interfaces:**
- Consumes: `Span` (Task 3), theme tokens (Task 1).
- Produces:
  - `func ClaudeGlyph(st claude.ClaudeSessionStatus) Span` — a zero `Span` for `""`.
  - `func ClaudeStatusText(st claude.ClaudeSessionStatus) string` — `""` for `""`.
  - `func PRBadge(state git.PRState) string`
  - `func PRCounts(prs []git.PullRequest) (ready, draft int)`
  - `func PRCountSpans(ready, draft int) []Span`
  - `func SectionHeader(title string, count int) string`

- [ ] **Step 1: Write the failing tests**

```go
package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/assert"
)

func TestClaudeGlyph(t *testing.T) {
	assert.Equal(t, "!", ClaudeGlyph(claude.StatusNeedsAttention).Text)
	assert.Equal(t, "✓", ClaudeGlyph(claude.StatusReadyForReview).Text)
	assert.Equal(t, "~", ClaudeGlyph(claude.StatusWorking).Text)
	assert.Equal(t, "✓", ClaudeGlyph(claude.StatusDone).Text)
	assert.Equal(t, "·", ClaudeGlyph(claude.StatusIdle).Text)
	assert.Equal(t, "", ClaudeGlyph("").Text)
	assert.NotEqual(t,
		ClaudeGlyph(claude.StatusReadyForReview).Style.GetForeground(),
		ClaudeGlyph(claude.StatusDone).Style.GetForeground())
}

func TestClaudeStatusText(t *testing.T) {
	assert.Equal(t, "Needs your input", ClaudeStatusText(claude.StatusNeedsAttention))
	assert.Equal(t, "Ready for review", ClaudeStatusText(claude.StatusReadyForReview))
	assert.Equal(t, "Working", ClaudeStatusText(claude.StatusWorking))
	assert.Equal(t, "Idle", ClaudeStatusText(claude.StatusIdle))
	assert.Equal(t, "Done", ClaudeStatusText(claude.StatusDone))
	assert.Equal(t, "", ClaudeStatusText(""))
}

func TestPRBadge(t *testing.T) {
	assert.Contains(t, PRBadge(git.PRStateOpen), "● open")
	assert.Contains(t, PRBadge(git.PRStateDraft), "◌ draft")
	assert.Contains(t, PRBadge(git.PRStateMerged), "⊕ merged")
	assert.Contains(t, PRBadge(git.PRStateClosed), "✕ closed")
}

func TestPRCounts_IgnoresMergedAndClosed(t *testing.T) {
	ready, draft := PRCounts([]git.PullRequest{
		{State: git.PRStateOpen}, {State: git.PRStateOpen},
		{State: git.PRStateDraft},
		{State: git.PRStateMerged}, {State: git.PRStateClosed},
	})
	assert.Equal(t, 2, ready)
	assert.Equal(t, 1, draft)
}

func spansText(spans []Span) string {
	s := ""
	for _, sp := range spans {
		s += sp.Text
	}
	return s
}

func TestPRCountSpans(t *testing.T) {
	assert.Equal(t, "●1 ◌2", spansText(PRCountSpans(1, 2)))
	assert.Equal(t, "●3", spansText(PRCountSpans(3, 0)))
	assert.Equal(t, "◌1", spansText(PRCountSpans(0, 1)))
	assert.Empty(t, PRCountSpans(0, 0))
	assert.LessOrEqual(t, lipgloss.Width(spansText(PRCountSpans(12, 34))), 7)
}

func TestSectionHeader(t *testing.T) {
	assert.Equal(t, "Needs you 2", SectionHeader("Needs you", 2))
}
```

Tests run without a colour profile, so styles render as plain text. That's why `SectionHeader` compares equal to the plain string.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement**

```go
package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func fg(c lipgloss.Color) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(c)
}

func ClaudeGlyph(st claude.ClaudeSessionStatus) Span {
	switch st {
	case claude.StatusNeedsAttention:
		return Span{Text: "!", Style: fg(theme.Current.StatusAttention).Bold(true)}
	case claude.StatusReadyForReview:
		return Span{Text: "✓", Style: fg(theme.Current.StatusReview).Bold(true)}
	case claude.StatusWorking:
		return Span{Text: "~", Style: fg(theme.Current.StatusWorking)}
	case claude.StatusDone:
		return Span{Text: "✓", Style: fg(theme.Current.StatusDone)}
	case claude.StatusIdle:
		return Span{Text: "·", Style: fg(theme.Current.StatusIdle)}
	default:
		return Span{}
	}
}

func ClaudeStatusText(st claude.ClaudeSessionStatus) string {
	switch st {
	case claude.StatusNeedsAttention:
		return "Needs your input"
	case claude.StatusReadyForReview:
		return "Ready for review"
	case claude.StatusWorking:
		return "Working"
	case claude.StatusIdle:
		return "Idle"
	case claude.StatusDone:
		return "Done"
	default:
		return ""
	}
}

func PRBadge(state git.PRState) string {
	switch state {
	case git.PRStateOpen:
		return fg(theme.Current.StatusReady).Render("● open")
	case git.PRStateDraft:
		return fg(theme.Current.TextMuted).Render("◌ draft")
	case git.PRStateMerged:
		return fg(theme.Current.Tertiary).Render("⊕ merged")
	case git.PRStateClosed:
		return fg(theme.Current.Error).Render("✕ closed")
	default:
		return fg(theme.Current.TextMuted).Render(string(state))
	}
}

func PRCounts(prs []git.PullRequest) (ready, draft int) {
	for _, pr := range prs {
		switch pr.State {
		case git.PRStateOpen:
			ready++
		case git.PRStateDraft:
			draft++
		}
	}
	return ready, draft
}

func PRCountSpans(ready, draft int) []Span {
	var spans []Span
	if ready > 0 {
		spans = append(spans, Span{Text: fmt.Sprintf("●%d", ready), Style: fg(theme.Current.StatusReady)})
	}
	if draft > 0 {
		if len(spans) > 0 {
			spans = append(spans, Span{Text: " "})
		}
		spans = append(spans, Span{Text: fmt.Sprintf("◌%d", draft), Style: fg(theme.Current.TextMuted)})
	}
	return spans
}

func SectionHeader(title string, count int) string {
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render(title) +
		fg(theme.Current.TextMuted).Render(fmt.Sprintf(" %d", count))
}
```

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ui
git commit -m "feat(tui): add ui status glyphs, PR badges and section header

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: Footer, full help and status plumbing

**Files:**
- Create: `internal/tui/ui/footer.go`, `internal/tui/ui/footer_test.go`
- Modify: `internal/tui/router/adapter.go`, `internal/tui/router/router.go`, `internal/tui/router/keys.go`
- Modify: `internal/tui/helpmodel.go`, `internal/tui/keys.go`, `internal/tui/app.go`
- Create: `internal/tui/app_test.go`
- Modify: `internal/tui/sessionlist/sessionlist.go` (implement `Status()`, drop `renderFooter`)

**Interfaces:**
- Consumes: `Truncate` (Task 2).
- Produces:
  ```go
  type StatusKind int
  const ( StatusInfo StatusKind = iota; StatusWarning; StatusError )
  type Status struct { Text string; Kind StatusKind }
  func (s Status) Empty() bool
  type StatusReporter interface { Status() Status }
  var MoreKey key.Binding
  func Footer(bindings []key.Binding, status Status, width int, showMore bool) string
  ```
  - `router.ViewEntry` gains `Status() ui.Status`.
  - `func (r Router) Status() ui.Status`
  - `func (r Router) ActiveView() View`
  - `sessionlist.Model` gains a `statusKind ui.StatusKind` field and `func (m Model) Status() ui.Status`.
  - Views now receive `height = window - 2*padY - 2`. The 2 rows are a blank spacer plus the footer line. Views must render **exactly** their height and nothing below it.

- [ ] **Step 1: Write the failing footer tests**

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `undefined: Footer`.

- [ ] **Step 3: Implement `ui/footer.go`**

```go
package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type StatusKind int

const (
	StatusInfo StatusKind = iota
	StatusWarning
	StatusError
)

type Status struct {
	Text string
	Kind StatusKind
}

func (s Status) Empty() bool {
	return s.Text == ""
}

type StatusReporter interface {
	Status() Status
}

var MoreKey = key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "more"))

const footerSep = " · "

func statusStyle(k StatusKind) lipgloss.Style {
	switch k {
	case StatusWarning:
		return fg(theme.Current.StatusActive)
	case StatusError:
		return fg(theme.Current.Error)
	default:
		return fg(theme.Current.TextMuted)
	}
}

func renderBinding(b key.Binding) string {
	h := b.Help()
	return fg(theme.Current.Primary).Render(h.Key) + " " + fg(theme.Current.TextMuted).Render(h.Desc)
}

func Footer(bindings []key.Binding, status Status, width int, showMore bool) string {
	if !status.Empty() {
		return statusStyle(status.Kind).Render(Truncate(status.Text, width))
	}

	var tail string
	used := 0
	if showMore {
		tail = renderBinding(MoreKey)
		used = lipgloss.Width(tail)
	}

	var parts []string
	for _, b := range bindings {
		if !b.Enabled() || b.Help().Key == "" {
			continue
		}
		p := renderBinding(b)
		w := lipgloss.Width(p)
		if used > 0 {
			w += len(footerSep)
		}
		if used+w > width {
			break
		}
		parts = append(parts, p)
		used += w
	}
	if showMore {
		parts = append(parts, tail)
	}
	return strings.Join(parts, fg(theme.Current.TextMuted).Render(footerSep))
}
```

- [ ] **Step 4: Run footer tests**

Run: `task test`
Expected: the `ui` tests PASS.

- [ ] **Step 5: Router plumbing**

In `internal/tui/router/adapter.go`, add `Status() ui.Status` to `ViewEntry` and implement it on the adapter (import `github.com/eleonorayaya/utena/internal/tui/ui`):

```go
type ViewEntry interface {
	Init() tea.Cmd
	Update(tea.Msg) tea.Cmd
	View() string
	Keys() help.KeyMap
	Status() ui.Status
}

func (a *ViewAdapter[T]) Status() ui.Status {
	if sr, ok := any(a.Model).(ui.StatusReporter); ok {
		return sr.Status()
	}
	return ui.Status{}
}
```

In `router.go` add:

```go
func (r Router) Status() ui.Status {
	return r.views[r.activeView].Status()
}

func (r Router) ActiveView() View {
	return r.activeView
}
```

In `router/keys.go` change the debug binding and expose it in full help:

```go
func (k routerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Debug}}
}

var routerKeys = routerKeyMap{
	Debug: key.NewBinding(key.WithKeys("ctrl+g"), key.WithHelp("ctrl+g", "debug")),
}
```

- [ ] **Step 6: Write the failing app tests**

`internal/tui/app_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func sized(t *testing.T, view router.View, w, h int) App {
	t.Helper()
	m, _ := NewApp("", "0", view).Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(App)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, " \n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestApp_FooterIsVisibleAndViewFitsWindow(t *testing.T) {
	a := sized(t, router.SessionListView, 200, 40)
	m, _ := a.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "one", Status: session.StatusActive},
	}})
	out := m.View()
	assert.Equal(t, 40, strings.Count(out, "\n")+1)
	assert.Contains(t, lastLine(out), "? more")
}

func TestApp_QuestionMarkTogglesFullHelp(t *testing.T) {
	a := sized(t, router.SessionListView, 120, 30)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.Contains(t, m.View(), "ctrl+g")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotContains(t, m.View(), "ctrl+g")
}

func TestApp_QuestionMarkIsTypedInTextForms(t *testing.T) {
	a := sized(t, router.SessionFormView, 120, 30)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.False(t, m.(App).help.full)
}
```

- [ ] **Step 7: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `help.full` undefined, and the footer is missing or the line count is wrong.

- [ ] **Step 8: Implement app chrome**

Replace `internal/tui/helpmodel.go`:

```go
package tui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type helpModel struct {
	inner   help.Model
	visible bool
	full    bool
}

func newHelpModel(visible bool) helpModel {
	h := help.New()
	h.Styles.FullKey = h.Styles.FullKey.Foreground(theme.Current.Primary)
	h.Styles.FullDesc = h.Styles.FullDesc.Foreground(theme.Current.Text)
	h.Styles.FullSeparator = h.Styles.FullSeparator.Foreground(theme.Current.TextMuted)
	return helpModel{inner: h, visible: visible}
}

func (h helpModel) Update(msg tea.Msg) helpModel {
	switch msg := msg.(type) {
	case router.SetHelpVisibleMsg:
		h.visible = msg.Visible
		if !msg.Visible {
			h.full = false
		}
	}
	return h
}

func (h helpModel) SetFull(full bool) helpModel {
	h.full = full
	return h
}

func (h helpModel) Footer(keymap help.KeyMap, status ui.Status, width int, showMore bool) string {
	if !h.visible {
		return ""
	}
	return ui.Footer(keymap.ShortHelp(), status, width, showMore)
}

func (h helpModel) FullView(keymap help.KeyMap, width int) string {
	h.inner.Width = width
	title := lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("Keys")
	hint := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("? or esc to close")
	return title + "\n\n" + h.inner.FullHelpView(keymap.FullHelp()) + "\n\n" + hint
}

func (h helpModel) HeightCost() int {
	if !h.visible {
		return 0
	}
	return 2
}
```

In `internal/tui/keys.go`, make the app's quit key full-help only, so it doesn't use up footer space:

```go
func (k appKeyMap) ShortHelp() []key.Binding {
	return nil
}
```

In `internal/tui/app.go`:
- add imports `github.com/charmbracelet/bubbles/key` and `github.com/eleonorayaya/utena/internal/tui/ui`
- add the text-entry set
- replace the `tea.KeyMsg` case in `Update`
- replace `View`

```go
var textEntryViews = map[router.View]bool{
	router.SessionFormView:        true,
	router.TodoFormView:           true,
	router.BlockerFormView:        true,
	router.WorkspaceCloneFormView: true,
}
```

```go
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if a.help.full {
			if key.Matches(msg, ui.MoreKey) || msg.Type == tea.KeyEsc {
				a.help = a.help.SetFull(false)
			}
			return a, nil
		}
		if a.help.visible && key.Matches(msg, ui.MoreKey) && !textEntryViews[a.router.ActiveView()] {
			a.help = a.help.SetFull(true)
			return a, nil
		}
```

```go
func (a App) View() string {
	width := max(a.width-padX*2, 0)
	pad := lipgloss.NewStyle().Padding(padY, padX)
	if a.help.full {
		return pad.Render(a.help.FullView(a.keys(), width))
	}
	content := a.router.View()
	showMore := !textEntryViews[a.router.ActiveView()]
	if footer := a.help.Footer(a.keys(), a.router.Status(), width, showMore); footer != "" {
		content += "\n\n" + footer
	}
	return pad.Render(content)
}
```

`OnWindowSizeMsg` already subtracts `a.help.HeightCost()`, which is now 2. Leave it as is.

- [ ] **Step 9: Move the session list status into the footer**

In `internal/tui/sessionlist/sessionlist.go`:
- add the field `statusKind ui.StatusKind` to `Model` (import `github.com/eleonorayaya/utena/internal/tui/ui`)
- add `setStatus` and `Status`
- make `confirmTwoPress` and the repair prompt warnings and `ErrMsg` an error
- delete `renderFooter` and its call
- make the view fill exactly `m.height`

```go
func (m *Model) setStatus(text string, kind ui.StatusKind) {
	m.statusMsg = text
	m.statusKind = kind
}

func (m Model) Status() ui.Status {
	return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
}
```

- Replace every `m.statusMsg = X` assignment with the matching `m.setStatus` call:
  - in `confirmTwoPress`: `m.setStatus(prompt, ui.StatusWarning)`
  - repair prompt: `m.setStatus(errMsg+" — press enter again to repair", ui.StatusWarning)`
  - `ErrMsg`: `m.setStatus(msg.Err.Error(), ui.StatusError)`
  - every other message (`already attached…`, `cannot archive…`, etc.): `ui.StatusInfo`
- `clearPending` and the non-nav reset keep clearing with `m.setStatus("", ui.StatusInfo)`.

Change `bodyHeight` to stop reserving the footer row (the old `-3` was 2 for the "Sessions" header plus 1 for the footer; the header stays until Task 7):

```go
func (m Model) bodyHeight() int {
	h := m.height - 2
	if m.firstBlockedIndex() >= 0 {
		h -= 2
	}
	if h < 1 {
		return 1
	}
	return h
}
```

Replace `View`:

```go
func (m Model) View() string {
	listW := m.width
	if m.isSplit() {
		listW = m.listWidth()
	}
	listStr := ui.FitHeight(m.renderList(listW), m.height)
	if !m.isSplit() {
		return listStr
	}

	panelW := m.width - listW - 2
	divStyle := lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
	divLines := make([]string, m.height)
	for i := range divLines {
		divLines[i] = divStyle.Render("│ ")
	}
	divider := strings.Join(divLines, "\n")

	panelStr := ui.FitHeight(sessiondetail.RenderPanel(m.selectedSession(), panelW, m.height), m.height)
	return lipgloss.JoinHorizontal(lipgloss.Top, listStr, divider, panelStr)
}
```

`renderList` must pad every line to `listW` so `JoinHorizontal` lines the panel up. Wrap its return in `lipgloss.NewStyle().Width(width).Render(...)`.

- [ ] **Step 10: Check for other `ViewEntry` implementers**

Run: `grep -rn "ViewEntry" internal cmd`
Expected: only `router/adapter.go`, `router/router.go`, `app.go`. If anything else implements `ViewEntry`, add a `Status() ui.Status { return ui.Status{} }` method to it.

- [ ] **Step 11: Run all tests**

Run: `task test`
Expected: PASS. Existing `sessionlist` tests that read `result.statusMsg` still pass, because the field name is unchanged.

- [ ] **Step 12: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): app-owned footer with status line and ? full help

Debug view moves from ? to ctrl+g.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: PR picker and URL opening

**Files:**
- Create: `internal/tui/ui/prpicker.go`, `internal/tui/ui/prpicker_test.go`

**Interfaces:**
- Consumes: `PRBadge`, `Truncate`, `Hyperlink`.
- Produces:
  ```go
  func OpenablePRs(prs []git.PullRequest) []git.PullRequest
  type PRPicker struct { /* unexported */ }
  func NewPRPicker(prs []git.PullRequest) PRPicker
  func (p PRPicker) IsOpen() bool
  func (p PRPicker) Update(msg tea.KeyMsg) (PRPicker, string)
  func (p PRPicker) Height() int
  func (p PRPicker) View(width int) string
  func (p PRPicker) Close() PRPicker
  type OpenURLFailedMsg struct{ Err error }
  func OpenURL(url string) tea.Cmd
  ```
  - `OpenablePRs` keeps open and draft PRs with a non-empty `HTMLURL`, ordered open-then-draft, and number descending within each.
  - `Update` returns the chosen URL on `enter`, and closes on `enter` or `esc`. `↑/k` and `↓/j` move the cursor. Other keys are ignored.
  - `Height()` is `len(prs) + 1` when open (a blank spacer plus one row per PR), `0` when closed.

- [ ] **Step 1: Write the failing tests**

```go
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
	assert.Equal(t, 3, p.Height())

	p, url := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, "", url)
	p, url = p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "u3", url)
	assert.False(t, p.IsOpen())
	assert.Equal(t, 0, p.Height())
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
	out := p.View(40)
	assert.Equal(t, p.Height(), strings.Count(out, "\n")+1)
	for _, line := range strings.Split(out, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(line), 40)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Implement**

```go
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
```

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ui
git commit -m "feat(tui): add PR picker and URL opener

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: Session list sections, sorting and scrolling

**Files:**
- Create: `internal/tui/sessionlist/sections.go`, `internal/tui/sessionlist/sections_test.go`
- Modify: `internal/tui/sessionlist/sessionlist.go`
- Modify: `internal/tui/sessionlist/sessionlist_test.go`

**Interfaces:**
- Consumes: `ui.SectionHeader`, `ui.FitHeight`.
- Produces (package-internal):
  ```go
  type sectionKind int
  const ( sectionNeedsYou sectionKind = iota; sectionWorking; sectionIdle; sectionBlocked; sectionHidden )
  func (k sectionKind) title() string
  func sectionOf(s session.Session) sectionKind
  func sortSessions(ss []session.Session)
  type rowKind int
  const ( rowSession rowKind = iota; rowHeader; rowGap )
  type listRow struct { kind rowKind; section sectionKind; count int; index int }
  func layoutRows(ss []session.Session) []listRow
  func rowOfSession(rows []listRow, index int) int
  ```
  `m.filtered` stays a flat, sorted `[]session.Session`, and `m.cursor` stays an index into it. Only `m.offset` changes meaning: it is now an index into `layoutRows(m.filtered)`.

- [ ] **Step 1: Write the failing tests**

`sections_test.go`:

```go
package sessionlist

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withClaude(id uint, name string, st claude.ClaudeSessionStatus) session.Session {
	return session.Session{
		Model:          gorm.Model{ID: id},
		Name:           name,
		Status:         session.StatusActive,
		ClaudeSessions: []claude.ClaudeSession{{Status: st}},
	}
}

func names(ss []session.Session) []string {
	var out []string
	for _, s := range ss {
		out = append(out, s.Name)
	}
	return out
}

func TestSectionOf(t *testing.T) {
	assert.Equal(t, sectionNeedsYou, sectionOf(withClaude(1, "a", claude.StatusNeedsAttention)))
	assert.Equal(t, sectionNeedsYou, sectionOf(withClaude(1, "a", claude.StatusReadyForReview)))
	assert.Equal(t, sectionWorking, sectionOf(withClaude(1, "a", claude.StatusWorking)))
	assert.Equal(t, sectionIdle, sectionOf(withClaude(1, "a", claude.StatusIdle)))
	assert.Equal(t, sectionIdle, sectionOf(withClaude(1, "a", claude.StatusDone)))
	assert.Equal(t, sectionIdle, sectionOf(session.Session{Status: session.StatusActive}))

	blocked := withClaude(1, "a", claude.StatusNeedsAttention)
	blocked.Blockers = []session.SessionBlocker{openBlocker(session.BlockerKindOther)}
	assert.Equal(t, sectionBlocked, sectionOf(blocked))

	hidden := blocked
	hidden.Status = session.StatusArchived
	assert.Equal(t, sectionHidden, sectionOf(hidden))
}

func TestSortSessions_SectionThenRecency(t *testing.T) {
	now := time.Now()
	idleOld := withClaude(1, "idle-old", claude.StatusIdle)
	idleOld.LastUsedAt = now.Add(-2 * time.Hour)
	idleNew := withClaude(2, "idle-new", claude.StatusIdle)
	idleNew.LastUsedAt = now.Add(-time.Minute)
	working := withClaude(3, "working", claude.StatusWorking)
	review := withClaude(4, "review", claude.StatusReadyForReview)
	review.LastUsedAt = now.Add(-time.Hour)
	attention := withClaude(5, "attention", claude.StatusNeedsAttention)
	attention.LastUsedAt = now

	ss := []session.Session{idleOld, working, review, idleNew, attention}
	sortSessions(ss)
	assert.Equal(t, []string{"attention", "review", "working", "idle-new", "idle-old"}, names(ss))
}

func TestSortSessions_AttachedStaysInItsSection(t *testing.T) {
	attached := withClaude(1, "attached", claude.StatusWorking)
	attached.IsAttached = true
	ss := []session.Session{attached, withClaude(2, "needs", claude.StatusNeedsAttention)}
	sortSessions(ss)
	assert.Equal(t, []string{"needs", "attached"}, names(ss))
}

func TestLayoutRows_HeadersGapsAndCounts(t *testing.T) {
	ss := []session.Session{
		withClaude(1, "a", claude.StatusNeedsAttention),
		withClaude(2, "b", claude.StatusReadyForReview),
		withClaude(3, "c", claude.StatusIdle),
	}
	rows := layoutRows(ss)
	kinds := []rowKind{}
	for _, r := range rows {
		kinds = append(kinds, r.kind)
	}
	assert.Equal(t, []rowKind{rowHeader, rowSession, rowSession, rowGap, rowHeader, rowSession}, kinds)
	assert.Equal(t, 2, rows[0].count)
	assert.Equal(t, sectionIdle, rows[4].section)
	assert.Equal(t, 1, rows[4].count)
	assert.Equal(t, 5, rowOfSession(rows, 2))
}

func TestSessionList_RendersSectionHeadersWithCounts(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		withClaude(1, "needs", claude.StatusNeedsAttention),
		withClaude(2, "busy", claude.StatusWorking),
	}})
	out := m.View()
	assert.Contains(t, out, "Needs you 1")
	assert.Contains(t, out, "Working 1")
	assert.NotContains(t, out, "Idle")
	assert.Less(t, strings.Index(out, "needs"), strings.Index(out, "Working 1"))
}

func TestSessionList_CursorFollowsSessionAcrossRegroup(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		withClaude(1, "a", claude.StatusNeedsAttention),
		withClaude(2, "b", claude.StatusWorking),
	}})
	m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	require.Equal(t, "b", m.selectedSession().Name)

	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		withClaude(1, "a", claude.StatusIdle),
		withClaude(2, "b", claude.StatusNeedsAttention),
	}})
	assert.Equal(t, "b", m.selectedSession().Name)
}

func TestSessionList_TinyHeightNeverOverflowsAndKeepsCursorVisible(t *testing.T) {
	var ss []session.Session
	statuses := []claude.ClaudeSessionStatus{claude.StatusNeedsAttention, claude.StatusWorking, claude.StatusIdle}
	for i := uint(1); i <= 12; i++ {
		ss = append(ss, withClaude(i, "s"+string(rune('a'+i)), statuses[i%3]))
	}
	for _, h := range []int{1, 2, 3, 6} {
		m, _ := New().Update(tea.WindowSizeMsg{Width: 40, Height: h})
		m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: ss})
		for range 11 {
			m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
			out := m.View()
			assert.LessOrEqual(t, strings.Count(out, "\n")+1, h)
			assert.Contains(t, out, m.selectedSession().Name)
		}
		for range 11 {
			m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
			assert.Contains(t, m.View(), m.selectedSession().Name)
		}
	}
}

func TestSessionList_EmptyState(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{})
	assert.Contains(t, m.View(), "No sessions — press n to create one")
}
```

Update `sessionlist_test.go`:
- Rename `TestSessionList_RendersBlockedHeaderAndTag` to `TestSessionList_RendersBlockedSection`. Replace `assert.Contains(t, out, "Blocked")` with `assert.Contains(t, out, "Blocked 1")`, and replace the `"⏸ slack ×2"` assertion with `assert.NotContains(t, out, "⏸")`. Keep the two ordering assertions, but compare against `"Blocked 1"`.
- `TestSessionList_ResolvedBlockersAreNotBlocked`: keep as is.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `undefined: sectionOf` and similar.

- [ ] **Step 3: Implement `sections.go`**

```go
package sessionlist

import (
	"sort"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

type sectionKind int

const (
	sectionNeedsYou sectionKind = iota
	sectionWorking
	sectionIdle
	sectionBlocked
	sectionHidden
)

func (k sectionKind) title() string {
	switch k {
	case sectionNeedsYou:
		return "Needs you"
	case sectionWorking:
		return "Working"
	case sectionIdle:
		return "Idle"
	case sectionBlocked:
		return "Blocked"
	default:
		return "Hidden"
	}
}

func sectionOf(s session.Session) sectionKind {
	switch {
	case s.IsHidden():
		return sectionHidden
	case s.IsBlocked():
		return sectionBlocked
	}
	switch s.AttentionStatus() {
	case claude.StatusNeedsAttention, claude.StatusReadyForReview:
		return sectionNeedsYou
	case claude.StatusWorking:
		return sectionWorking
	default:
		return sectionIdle
	}
}

func sortSessions(ss []session.Session) {
	sort.SliceStable(ss, func(i, j int) bool {
		si, sj := sectionOf(ss[i]), sectionOf(ss[j])
		if si != sj {
			return si < sj
		}
		return ss[i].LastUsedAt.After(ss[j].LastUsedAt)
	})
}

type rowKind int

const (
	rowSession rowKind = iota
	rowHeader
	rowGap
)

type listRow struct {
	kind    rowKind
	section sectionKind
	count   int
	index   int
}

func layoutRows(ss []session.Session) []listRow {
	counts := map[sectionKind]int{}
	for _, s := range ss {
		counts[sectionOf(s)]++
	}
	var rows []listRow
	for i, s := range ss {
		sec := sectionOf(s)
		if i == 0 || sectionOf(ss[i-1]) != sec {
			if i > 0 {
				rows = append(rows, listRow{kind: rowGap})
			}
			rows = append(rows, listRow{kind: rowHeader, section: sec, count: counts[sec]})
		}
		rows = append(rows, listRow{kind: rowSession, section: sec, index: i})
	}
	return rows
}

func rowOfSession(rows []listRow, index int) int {
	for i, r := range rows {
		if r.kind == rowSession && r.index == index {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 4: Rewire `sessionlist.go`**

- Delete `listGroupKind`, `groupLive`/`groupBlocked`/`groupHidden`, `listGroup`, `firstBlockedIndex`, `blockedTag`, `sectionHeader`.
- In `rebuildFiltered`, replace the `sort.SliceStable(...)` call with `sortSessions(m.filtered)`, and drop the `sort` import if it's now unused.
- Replace `bodyHeight`, `clampOffset` and `renderList`:

```go
func (m Model) bodyHeight() int {
	return max(m.height, 1)
}

func (m *Model) clampOffset() {
	rows := layoutRows(m.filtered)
	bh := m.bodyHeight()
	cr := rowOfSession(rows, m.cursor)
	if cr < 0 {
		m.offset = 0
		return
	}
	top := cr
	if cr > 0 && rows[cr-1].kind == rowHeader && bh > 1 {
		top = cr - 1
	}
	if top < m.offset {
		m.offset = top
	}
	if cr >= m.offset+bh {
		m.offset = cr - bh + 1
	}
	m.offset = max(min(m.offset, len(rows)-bh), 0)
}

func (m Model) renderList(width int) string {
	if len(m.filtered) == 0 {
		empty := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("  No sessions — press n to create one")
		return lipgloss.NewStyle().Width(width).Render(empty)
	}
	rows := layoutRows(m.filtered)
	end := min(m.offset+m.bodyHeight(), len(rows))
	var lines []string
	for _, r := range rows[m.offset:end] {
		switch r.kind {
		case rowGap:
			lines = append(lines, "")
		case rowHeader:
			lines = append(lines, "  "+ui.SectionHeader(r.section.title(), r.count))
		default:
			lines = append(lines, m.renderRow(m.filtered[r.index], r.index == m.cursor, width))
		}
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}
```

- Keep the existing `renderRow` for now; Task 8 replaces it. Remove its `blockedTag` branch: always use `statusview.NewStatusBadge(...)`.
- `clampOffset` must be called after every cursor move and in `OnWindowSizeMsg` (add `m.clampOffset()` there, before `return`).

- [ ] **Step 5: Run tests**

Run: `task test`
Expected: PASS, including the existing ordering tests (`SortsHiddenAfterActive`, `GroupsBlockedBetweenActiveAndHidden`, `ViewFitsHeightWithBlockedHeaderMidWindow`, `CursorMovesAcrossHeader`).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/sessionlist
git commit -m "feat(tui): section the session list by Claude state

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Session list row rendering and keys

**Files:**
- Create: `internal/tui/sessionlist/rows.go`, `internal/tui/sessionlist/rows_test.go`
- Modify: `internal/tui/sessionlist/sessionlist.go` (delete the old `renderRow`, `truncateWidth`, and the `statusview` import)
- Modify: `internal/tui/sessionlist/keys.go`

**Interfaces:**
- Consumes: `ui.Row`, `ui.Cell`, `ui.Span`, `ui.ClaudeGlyph`, `ui.PRCounts`, `ui.PRCountSpans`, `ui.ShortAgo`.
- Produces: `func (m Model) renderRow(s session.Session, selected bool, width int) string`. Key bindings: `keys.OpenPR` (`o`, `open PR`), plus `Info` now also matches `right`.

- [ ] **Step 1: Write the failing tests**

```go
package sessionlist

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/assert"
)

func TestRenderRow_Columns(t *testing.T) {
	s := withClaude(1, "checkout-flow-redesign", claude.StatusReadyForReview)
	s.IsAttached = true
	s.LastUsedAt = time.Now().Add(-45 * time.Minute)
	s.PullRequests = []git.PullRequest{{State: git.PRStateOpen}, {State: git.PRStateDraft}, {State: git.PRStateMerged}}

	out := New().renderRow(s, false, 50)
	assert.Equal(t, 50, lipgloss.Width(out))
	assert.True(t, strings.HasPrefix(out, "  ✓ checkout-flow-redesign"))
	assert.Contains(t, out, "◆")
	assert.Contains(t, out, "●1 ◌1")
	assert.True(t, strings.HasSuffix(out, " 45m"))
}

func TestRenderRow_SelectedHasGutterBar(t *testing.T) {
	out := New().renderRow(withClaude(1, "x", claude.StatusIdle), true, 40)
	assert.True(t, strings.HasPrefix(out, "▌ "))
	assert.Equal(t, 40, lipgloss.Width(out))
}

func TestRenderRow_LongAndWideNamesFitWidth(t *testing.T) {
	for _, name := range []string{strings.Repeat("very-long-session-name-", 6), "日本語のセッション名がとても長い場合", "🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀🚀"} {
		for _, w := range []int{20, 36, 52} {
			out := New().renderRow(withClaude(1, name, claude.StatusWorking), true, w)
			assert.LessOrEqual(t, lipgloss.Width(out), w, "%q at %d", name, w)
		}
	}
}

func TestSessionList_ShortHelp(t *testing.T) {
	var got []string
	for _, b := range keys.ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	assert.Equal(t, []string{"enter switch", "o open PR", "n new", "b block", "a archive"}, got)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — wrong prefix (`› ` caret / no glyph), `keys.ShortHelp` mismatch.

- [ ] **Step 3: Implement `rows.go`**

```go
package sessionlist

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const (
	gutterColW   = 2
	glyphColW    = 2
	attachedColW = 2
	prColW       = 7
	timeColW     = 4
)

func nameStyle(s session.Session, selected bool) lipgloss.Style {
	switch s.Status {
	case session.StatusBroken:
		return lipgloss.NewStyle().Foreground(theme.Current.Error)
	case session.StatusCreating:
		return lipgloss.NewStyle().Foreground(theme.Current.StatusActive)
	}
	if selected {
		return lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
	}
	return lipgloss.NewStyle().Foreground(theme.Current.Text)
}

func (m Model) renderRow(s session.Session, selected bool, width int) string {
	var bg lipgloss.TerminalColor
	gutter := ui.Span{Text: "  "}
	if selected {
		bg = theme.Current.Selection
		gutter = ui.Span{Text: "▌ ", Style: lipgloss.NewStyle().Foreground(theme.Current.Primary)}
	}

	var attached ui.Span
	if s.IsAttached {
		attached = ui.Span{Text: "◆", Style: lipgloss.NewStyle().Foreground(theme.Current.Primary)}
	}

	ready, draft := ui.PRCounts(s.PullRequests)
	muted := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)

	return ui.Row([]ui.Cell{
		{Spans: []ui.Span{gutter}, Width: gutterColW},
		{Spans: []ui.Span{ui.ClaudeGlyph(s.AttentionStatus())}, Width: glyphColW},
		{Spans: []ui.Span{{Text: sessionDisplayName(s), Style: nameStyle(s, selected)}}},
		{Spans: []ui.Span{attached}, Width: attachedColW},
		{Spans: ui.PRCountSpans(ready, draft), Width: prColW},
		{Spans: []ui.Span{{Text: ui.ShortAgo(s.LastUsedAt), Style: muted}}, Width: timeColW, Align: ui.AlignRight},
	}, width, bg)
}
```

In `sessionlist.go`:
- delete the old `renderRow`
- replace `truncateWidth` uses with `ui.Truncate`, then delete `truncateWidth`
- remove the `statusview` and `common` imports if they're now unused

- [ ] **Step 4: Update `keys.go`**

```go
type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	Select       key.Binding
	OpenPR       key.Binding
	Archive      key.Binding
	Delete       key.Binding
	Block        key.Binding
	New          key.Binding
	Info         key.Binding
	Todos        key.Binding
	Workspaces   key.Binding
	ToggleHidden key.Binding
	Quit         key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.OpenPR, k.New, k.Block, k.Archive}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Select, k.Info, k.OpenPR},
		{k.New, k.Block, k.Archive, k.Delete},
		{k.Todos, k.Workspaces, k.ToggleHidden, k.Quit},
	}
}

var keys = keyMap{
	Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Select:       key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "switch")),
	OpenPR:       key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open PR")),
	Archive:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
	Delete:       key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
	Block:        key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block")),
	New:          key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Info:         key.NewBinding(key.WithKeys("i", "right"), key.WithHelp("i/→", "details")),
	Todos:        key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "todos")),
	Workspaces:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "workspaces")),
	ToggleHidden: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "hidden")),
	Quit:         key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
}
```

`OpenPR` has no handler until Task 9. That's fine, because unhandled keys fall through.

- [ ] **Step 5: Run tests**

Run: `task test`
Expected: PASS. If `TestSessionList_PreviewShowsEmbeddedPRsForMultiWorkspaceSession` fails on `"#116 utena pr"`, leave it for Task 10, which rewrites that assertion. To keep this commit green, change it now to `assert.Contains(t, out, "utena pr")` and `assert.Contains(t, out, "dotfiles pr")`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/sessionlist
git commit -m "feat(tui): column-based session rows with soft selection

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: `o` opens PRs from the session list

**Files:**
- Modify: `internal/tui/sessionlist/sessionlist.go`
- Create: `internal/tui/sessionlist/openpr_test.go`

**Interfaces:**
- Consumes: `ui.OpenablePRs`, `ui.NewPRPicker`, `ui.PRPicker`, `ui.OpenURL`, `ui.OpenURLFailedMsg`, `keys.OpenPR`.
- Produces: the field `picker ui.PRPicker` on `Model`. `Status()` returns the picker hint while the picker is open.

- [ ] **Step 1: Write the failing tests**

```go
package sessionlist

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pressO(m Model) (Model, tea.Cmd, bool) {
	return m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
}

func withPRs(s session.Session, prs ...git.PullRequest) session.Session {
	s.PullRequests = prs
	return s
}

func listWith(t *testing.T, ss ...session.Session) Model {
	t.Helper()
	m, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: ss})
	return m
}

func TestOpenPR_NoneShowsStatus(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle), git.PullRequest{Number: 1, State: git.PRStateMerged, HTMLURL: "u"}))
	m, cmd, handled := pressO(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, "no open pull requests", m.Status().Text)
}

func TestOpenPR_SingleOpensDirectly(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle), git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"}))
	m, cmd, handled := pressO(m)
	assert.True(t, handled)
	assert.NotNil(t, cmd)
	assert.False(t, m.picker.IsOpen())
}

func TestOpenPR_EmptyURLIsNotOpenable(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle), git.PullRequest{Number: 1, State: git.PRStateOpen}))
	m, cmd, _ := pressO(m)
	assert.Nil(t, cmd)
	assert.Equal(t, "no open pull requests", m.Status().Text)
}

func TestOpenPR_SeveralOpensPickerThenChooses(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle),
		git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, State: git.PRStateDraft, HTMLURL: "u2"}))
	m, cmd, _ := pressO(m)
	assert.Nil(t, cmd)
	require.True(t, m.picker.IsOpen())
	assert.Contains(t, m.Status().Text, "enter open")

	m, _, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.True(t, handled)
	assert.Equal(t, "a", m.selectedSession().Name)

	m, cmd, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd)
	assert.False(t, m.picker.IsOpen())
}

func TestOpenPR_PickerClosesWhenSelectionChanges(t *testing.T) {
	a := withPRs(withClaude(1, "a", claude.StatusIdle),
		git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, State: git.PRStateOpen, HTMLURL: "u2"})
	b := withClaude(2, "b", claude.StatusIdle)
	m := listWith(t, a, b)
	m, _, _ = pressO(m)
	require.True(t, m.picker.IsOpen())

	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{b}})
	assert.False(t, m.picker.IsOpen())
}

func TestOpenPR_PickerReservesRowsInView(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle),
		git.PullRequest{Number: 4847, Title: "first", State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, Title: "second", State: git.PRStateDraft, HTMLURL: "u2"}))
	m, _, _ = pressO(m)
	out := m.View()
	assert.Contains(t, out, "#4847")
	assert.LessOrEqual(t, len(splitLines(out)), 20)
}

func TestOpenURLFailed_ShowsError(t *testing.T) {
	m := listWith(t, withClaude(1, "a", claude.StatusIdle))
	m, _ = m.Update(ui.OpenURLFailedMsg{Err: errors.New("boom")})
	assert.Equal(t, ui.Status{Text: "could not open PR: boom", Kind: ui.StatusError}, m.Status())
}
```

Add the helper to `openpr_test.go`:

```go
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}
```

(and add `"strings"` to the imports).

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `m.picker` undefined.

- [ ] **Step 3: Implement**

In `sessionlist.go`:
- add `picker ui.PRPicker` to `Model`
- in `clearPending`, add `m.picker = m.picker.Close()`
- in `Update`, handle `ui.OpenURLFailedMsg`:

```go
	case ui.OpenURLFailedMsg:
		m.setStatus("could not open PR: "+msg.Err.Error(), ui.StatusError)
		return m, nil
```

At the very top of `OnKeyMsg`, before the pending-reset logic:

```go
	if m.picker.IsOpen() {
		var url string
		m.picker, url = m.picker.Update(msg)
		if url != "" {
			return m, ui.OpenURL(url), true
		}
		return m, nil, true
	}
```

Add the `OpenPR` case in the `switch`:

```go
	case key.Matches(msg, keys.OpenPR):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		prs := ui.OpenablePRs(sel.PullRequests)
		switch len(prs) {
		case 0:
			m.setStatus("no open pull requests", ui.StatusInfo)
			return m, nil, true
		case 1:
			return m, ui.OpenURL(prs[0].HTMLURL), true
		default:
			m.picker = ui.NewPRPicker(prs)
			m.clampOffset()
			return m, nil, true
		}
```

Update `Status`:

```go
func (m Model) Status() ui.Status {
	if m.picker.IsOpen() {
		return ui.Status{Text: "↑/↓ choose · enter open · esc cancel", Kind: ui.StatusInfo}
	}
	return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
}
```

Reserve the picker's rows: change `bodyHeight` to

```go
func (m Model) bodyHeight() int {
	return max(m.height-m.picker.Height(), 1)
}
```

and in `View`, render the picker under the list column:

```go
	listStr := m.renderList(listW)
	if m.picker.IsOpen() {
		listStr += "\n" + lipgloss.NewStyle().Width(listW).Render(m.picker.View(listW))
	}
	listStr = ui.FitHeight(listStr, m.height)
```

`SessionsStateUpdatedMsg` already calls `clearPending()` when the selected ID changes, which now also closes the picker.

- [ ] **Step 4: Run tests**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/sessionlist
git commit -m "feat(tui): open session PRs with o from the session list

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: Detail panel rewrite

**Files:**
- Create: `internal/tui/sessiondetail/prgroups.go`, `internal/tui/sessiondetail/prgroups_test.go`
- Create: `internal/tui/sessiondetail/panel.go`, `internal/tui/sessiondetail/panel_test.go`
- Modify: `internal/tui/sessiondetail/model.go` (move `View` and the style helpers out to `panel.go`)
- Modify: `internal/tui/sessiondetail/model_test.go`, `internal/tui/sessionlist/sessionlist_test.go`

**Interfaces:**
- Consumes: `ui.SectionHeader`, `ui.ClaudeGlyph`, `ui.ClaudeStatusText`, `ui.PRBadge`, `ui.Hyperlink`, `ui.Truncate`, `ui.FitHeight`, `ui.ShortAgo`.
- Produces:
  ```go
  type prGroup struct { name string; prs []git.PullRequest }
  func groupPRs(s session.Session) []prGroup
  func (m Model) View() string
  func RenderPanel(s *session.Session, width, height int) string
  ```
  `RenderPanel` keeps its current signature.

- [ ] **Step 1: Write the failing grouping tests**

`prgroups_test.go`:

```go
package sessiondetail

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func wt(pos int, name string, repoID, branchID uint) session.SessionWorktree {
	return session.SessionWorktree{
		Position:  pos,
		Worktree:  &git.Worktree{RepoID: repoID, BranchID: branchID},
		Workspace: &workspace.Workspace{Model: gorm.Model{ID: repoID * 10}, Name: name},
	}
}

func groupSummary(gs []prGroup) map[string][]int {
	out := map[string][]int{}
	for _, g := range gs {
		nums := []int{}
		for _, p := range g.prs {
			nums = append(nums, p.Number)
		}
		out[g.name] = nums
	}
	return out
}

func TestGroupPRs_ByRepoIncludingEmptyWorkspacesInPositionOrder(t *testing.T) {
	past := uint(99)
	s := session.Session{
		Worktrees: []session.SessionWorktree{wt(1, "web-app-api", 2, 20), wt(0, "api-service", 1, 10), wt(2, "data-sync", 3, 30)},
		PullRequests: []git.PullRequest{
			{Number: 4847, RepoID: 1, State: git.PRStateDraft},
			{Number: 2236, RepoID: 2, State: git.PRStateMerged},
			{Number: 4426, RepoID: 2, State: git.PRStateMerged, HeadBranchID: &past},
			{Number: 5000, RepoID: 2, State: git.PRStateOpen},
		},
	}
	gs := groupPRs(s)
	assert.Equal(t, []string{"api-service", "web-app-api", "data-sync"}, []string{gs[0].name, gs[1].name, gs[2].name})
	assert.Equal(t, map[string][]int{
		"api-service": {4847},
		"web-app-api": {5000, 4426, 2236},
		"data-sync":   {},
	}, groupSummary(gs))
}

func TestGroupPRs_FallsBackToHeadBranchWhenRepoUnknown(t *testing.T) {
	b := uint(10)
	s := session.Session{
		Worktrees:    []session.SessionWorktree{wt(0, "utena", 0, 10)},
		PullRequests: []git.PullRequest{{Number: 1, HeadBranchID: &b, State: git.PRStateOpen}},
	}
	assert.Equal(t, map[string][]int{"utena": {1}}, groupSummary(groupPRs(s)))
}

func TestGroupPRs_UnmatchedGoToOther(t *testing.T) {
	s := session.Session{
		Worktrees:    []session.SessionWorktree{wt(0, "utena", 1, 10)},
		PullRequests: []git.PullRequest{{Number: 7, RepoID: 42, State: git.PRStateOpen}},
	}
	gs := groupPRs(s)
	assert.Equal(t, "Other", gs[len(gs)-1].name)
	assert.Equal(t, map[string][]int{"utena": {}, "Other": {7}}, groupSummary(gs))
}

func TestGroupPRs_NoWorktreesUsesWorkspaceDisplay(t *testing.T) {
	s := session.Session{PullRequests: []git.PullRequest{{Number: 3, State: git.PRStateOpen}}}
	assert.Equal(t, map[string][]int{"no workspace": {3}}, groupSummary(groupPRs(s)))
}
```

- [ ] **Step 2: Write the failing panel tests**

`panel_test.go`:

```go
package sessiondetail

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func richSession() session.Session {
	return session.Session{
		Model:          gorm.Model{ID: 1},
		Name:           "checkout-flow-redesign",
		Status:         session.StatusActive,
		IsAttached:     true,
		LastUsedAt:     time.Now(),
		SessionRoot:    "/Users/x/utena-sessions/checkout-flow-redesign",
		ClaudeSessions: []claude.ClaudeSession{{Status: claude.StatusReadyForReview}},
		Worktrees:      []session.SessionWorktree{wt(0, "api-service", 1, 10), wt(1, "data-sync", 3, 30)},
		PullRequests: []git.PullRequest{
			{Number: 4847, RepoID: 1, Title: "fix(ABC-123): stop scheduled jobs from running twice per window", State: git.PRStateDraft, HTMLURL: "https://github.com/o/api-service/pull/4847"},
		},
	}
}

func TestPanel_HeaderAndStatusLine(t *testing.T) {
	s := richSession()
	out := RenderPanel(&s, 80, 20)
	lines := strings.Split(out, "\n")
	assert.True(t, strings.HasPrefix(lines[0], "checkout-flow-redesign"))
	assert.True(t, strings.HasSuffix(strings.TrimRight(lines[0], " "), "◆ attached"))
	assert.Equal(t, "✓ Ready for review · used just now", strings.TrimRight(lines[1], " "))
	assert.NotContains(t, lines[0], "active")
}

func TestPanel_PillOnlyWhenNotActive(t *testing.T) {
	s := richSession()
	s.Status = session.StatusBroken
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, strings.Split(out, "\n")[0], "broken")
}

func TestPanel_WorkspacesAndPRsWithLinks(t *testing.T) {
	s := richSession()
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, out, "api-service")
	assert.Contains(t, out, "\x1b]8;;https://github.com/o/api-service/pull/4847\x1b\\")
	assert.Contains(t, out, "◌ draft")
	assert.Contains(t, out, "data-sync")
	assert.Contains(t, out, "no pull requests")
}

func TestPanel_PRWithoutURLHasNoLinkEscape(t *testing.T) {
	s := richSession()
	s.PullRequests[0].HTMLURL = ""
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, out, "#4847")
	assert.NotContains(t, out, "\x1b]8;;")
}

func TestPanel_RemovedSections(t *testing.T) {
	s := richSession()
	out := RenderPanel(&s, 80, 30)
	for _, gone := range []string{"Tmux", "Root", "Past", "◆ Claude", "utena-sessions/"} {
		assert.NotContains(t, out, gone)
	}
}

func TestPanel_LinesFitWidthAndHeight(t *testing.T) {
	s := richSession()
	s.Name = strings.Repeat("日本語", 30)
	s.Status = session.StatusCreating
	s.StatusError = strings.Repeat("something went badly wrong ", 10)
	for _, w := range []int{30, 60, 120} {
		out := RenderPanel(&s, w, 12)
		lines := strings.Split(out, "\n")
		assert.Len(t, lines, 12)
		for _, l := range lines {
			assert.LessOrEqual(t, lipgloss.Width(l), w, "width %d: %q", w, l)
		}
	}
}

func TestPanel_EmptyState(t *testing.T) {
	assert.Contains(t, RenderPanel(nil, 60, 10), "Select a session")
}

func TestPanel_ProblemCallout(t *testing.T) {
	s := richSession()
	s.SessionActions = []session.SessionAction{{Type: "pull", Error: "dirty tree"}}
	out := RenderPanel(&s, 80, 20)
	assert.Contains(t, out, "⚠ action pull failed: dirty tree")
}
```

If `session.SessionAction.Type` isn't a `string`-kinded type, check its definition with `grep -n "type SessionAction struct" -A8 internal/session/*.go` and adapt the literal.

Update `model_test.go`:
- `TestSessionDetail_ShowsEmbeddedPRsFromEveryWorkspace`: assert `"utena pr"` and `"dotfiles pr"`, plus `"#116"` and `"#96"`.
- `TestSessionDetail_RefreshUpdatesEmbeddedPRs`: assert `"⊕ merged"`.
- `TestSessionDetail_View_ShowsWarning`: `"[!]"` → `"⚠"`. `TestSessionDetail_View_NoWarningWhenClean`: `"[!]"` → `"⚠"`.
- `TestSessionDetail_RendersOpenBlockers`: `"Blockers"` → `"Blocked 2"`.
- `TestSessionDetail_RefreshClampsCursorWhenLastBlockerResolved`: `"Blockers"` → `"Blocked"`.
- `TestRenderPanel_ShowsBlockersWithoutCursor`: `"›"` → `"▌"`.
- Replace `TestRenderPanel_ShowsPastBranchesAndTheirPRs` with:

```go
func TestRenderPanel_PastBranchPRsStillShowUnderTheirRepo(t *testing.T) {
	current, past := uint(2), uint(1)
	s := session.Session{
		Model:     gorm.Model{ID: 9},
		Name:      "stacked",
		Status:    session.StatusActive,
		Worktrees: []session.SessionWorktree{wt(0, "utena", 5, current)},
		PullRequests: []git.PullRequest{
			{Number: 1, Title: "base", RepoID: 5, HeadBranchID: &past, State: git.PRStateOpen},
			{Number: 2, Title: "top", RepoID: 5, HeadBranchID: &current, State: git.PRStateDraft},
		},
	}
	out := RenderPanel(&s, 80, 0)
	assert.Contains(t, out, "base")
	assert.Contains(t, out, "top")
	assert.NotContains(t, out, "eqt/")
	assert.NotContains(t, out, "Other")
}
```

In `sessionlist_test.go`, `TestSessionList_PreviewShowsEmbeddedPRsForMultiWorkspaceSession` should assert `"utena pr"` and `"dotfiles pr"` (if Task 8 didn't already change it).

- [ ] **Step 3: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `undefined: groupPRs`, old panel output.

- [ ] **Step 4: Implement `prgroups.go`**

```go
package sessiondetail

import (
	"sort"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
)

type prGroup struct {
	name string
	prs  []git.PullRequest
}

var prStateRank = map[git.PRState]int{
	git.PRStateOpen:   0,
	git.PRStateDraft:  1,
	git.PRStateMerged: 2,
	git.PRStateClosed: 3,
}

func sortPRs(prs []git.PullRequest) {
	sort.SliceStable(prs, func(i, j int) bool {
		ri, rj := prStateRank[prs[i].State], prStateRank[prs[j].State]
		if ri != rj {
			return ri < rj
		}
		return prs[i].Number > prs[j].Number
	})
}

func prMatchesWorktree(pr git.PullRequest, wt *git.Worktree) bool {
	if wt == nil {
		return false
	}
	if pr.RepoID != 0 && wt.RepoID != 0 {
		return pr.RepoID == wt.RepoID
	}
	return pr.HeadBranchID != nil && *pr.HeadBranchID == wt.BranchID
}

func groupPRs(s session.Session) []prGroup {
	if len(s.Worktrees) == 0 {
		prs := append([]git.PullRequest(nil), s.PullRequests...)
		sortPRs(prs)
		return []prGroup{{name: s.WorkspaceDisplay(), prs: prs}}
	}

	wts := append([]session.SessionWorktree(nil), s.Worktrees...)
	sort.SliceStable(wts, func(i, j int) bool { return wts[i].Position < wts[j].Position })

	assigned := make([]bool, len(s.PullRequests))
	var groups []prGroup
	for _, sw := range wts {
		name := "workspace"
		if sw.Workspace != nil && sw.Workspace.Name != "" {
			name = sw.Workspace.Name
		}
		g := prGroup{name: name}
		for i, pr := range s.PullRequests {
			if !assigned[i] && prMatchesWorktree(pr, sw.Worktree) {
				g.prs = append(g.prs, pr)
				assigned[i] = true
			}
		}
		sortPRs(g.prs)
		groups = append(groups, g)
	}

	other := prGroup{name: "Other"}
	for i, pr := range s.PullRequests {
		if !assigned[i] {
			other.prs = append(other.prs, pr)
		}
	}
	if len(other.prs) > 0 {
		sortPRs(other.prs)
		groups = append(groups, other)
	}
	return groups
}
```

- [ ] **Step 5: Implement `panel.go`**

Move `View`, `RenderPanel`, `padToHeight` and every style helper (`titleStyle`, `labelStyle`, `valueStyle`, `warningStyle`, `sectionStyle`, `ruleStyle`, `statusColor`, `statusPill`, `prStateColor`) out of `model.go`. Delete `labelStyle`, `sectionStyle`, `prStateColor` and `padToHeight`; they're replaced below. `panel.go`:

```go
package sessiondetail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const prNumW = 6

func titleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.TextEmphasis)
}

func valueStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Text)
}

func mutedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
}

func warningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.AccentLavender)
}

func ruleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
}

func workspaceStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.Tertiary).Bold(true)
}

func statusColor(status session.SessionStatus) lipgloss.Color {
	switch status {
	case session.StatusCreating:
		return theme.Current.StatusActive
	case session.StatusBroken:
		return theme.Current.Error
	case session.StatusPending:
		return theme.Current.StatusPending
	default:
		return theme.Current.TextMuted
	}
}

func statusPill(status session.SessionStatus) string {
	return lipgloss.NewStyle().
		Foreground(theme.Current.TextOnPrimary).
		Background(statusColor(status)).
		Bold(true).
		Render(" " + string(status) + " ")
}

func RenderPanel(s *session.Session, width, height int) string {
	m := Model{sess: s, width: width, height: height}
	return m.View()
}

func (m Model) View() string {
	width := m.width
	if width < 1 {
		width = 60
	}
	if m.sess == nil {
		return ui.FitHeight(mutedStyle().Render("Select a session"), m.height)
	}
	s := *m.sess

	var lines []string
	lines = append(lines, titleLine(s, width), statusLine(s, width), ruleStyle().Render(strings.Repeat("─", width)), "")

	if problems := problemLines(s); len(problems) > 0 {
		for _, p := range problems {
			lines = append(lines, warningStyle().Render(ui.Truncate("⚠ "+p, width)))
		}
		lines = append(lines, "")
	}

	if open := s.OpenBlockers(); len(open) > 0 {
		lines = append(lines, ui.SectionHeader("Blocked", len(open)))
		for i, bl := range open {
			gutter := "  "
			if m.interactive && i == m.blockerCursor {
				gutter = lipgloss.NewStyle().Foreground(theme.Current.Primary).Render("▌ ")
			}
			text := fmt.Sprintf("%s · %s · %s", bl.Kind, bl.Label(), common.TimeAgo(bl.CreatedAt))
			lines = append(lines, gutter+valueStyle().Render(ui.Truncate(text, max(width-2, 1))))
		}
		lines = append(lines, "")
	}

	for _, g := range groupPRs(s) {
		lines = append(lines, workspaceStyle().Render(ui.Truncate(g.name, width)))
		if len(g.prs) == 0 {
			lines = append(lines, "  "+mutedStyle().Render("no pull requests"))
			continue
		}
		for _, pr := range g.prs {
			lines = append(lines, prLine(pr, width))
		}
	}

	return ui.FitHeight(strings.Join(lines, "\n"), m.height)
}

func titleLine(s session.Session, width int) string {
	var right []string
	if s.IsAttached {
		right = append(right, lipgloss.NewStyle().Foreground(theme.Current.Primary).Render("◆ attached"))
	}
	if s.Status != session.StatusActive && s.Status != "" {
		right = append(right, statusPill(s.Status))
	}
	rightStr := strings.Join(right, " ")
	rightW := lipgloss.Width(rightStr)
	if rightW >= width {
		rightStr, rightW = "", 0
	}
	nameW := max(width-rightW-1, 1)
	name := titleStyle().Render(ui.Truncate(s.DisplayLabel(), nameW))
	gap := max(width-lipgloss.Width(name)-rightW, 0)
	return name + strings.Repeat(" ", gap) + rightStr
}

func statusLine(s session.Session, width int) string {
	var parts []string
	if st := s.AttentionStatus(); st != "" {
		g := ui.ClaudeGlyph(st)
		parts = append(parts, g.Render()+" "+valueStyle().Render(ui.ClaudeStatusText(st)))
	}
	if ago := ui.ShortAgo(s.LastUsedAt); ago != "" {
		used := "used " + ago + " ago"
		if ago == "now" {
			used = "used just now"
		}
		parts = append(parts, mutedStyle().Render(used))
	}
	line := strings.Join(parts, mutedStyle().Render(" · "))
	if lipgloss.Width(line) > width {
		return ui.Truncate(ui.ClaudeStatusText(s.AttentionStatus()), width)
	}
	return line
}

func problemLines(s session.Session) []string {
	var out []string
	if s.StatusError != "" {
		out = append(out, s.StatusError)
	}
	for _, a := range s.SessionActions {
		if a.Error != "" {
			out = append(out, fmt.Sprintf("action %s failed: %s", a.Type, a.Error))
		}
	}
	return out
}

func prLine(pr git.PullRequest, width int) string {
	numText := fmt.Sprintf("#%d", pr.Number)
	num := ui.Hyperlink(pr.HTMLURL, lipgloss.NewStyle().Foreground(theme.Current.AccentBlue).Render(numText))
	numPad := strings.Repeat(" ", max(prNumW-len(numText), 0))
	badge := ui.PRBadge(pr.State)
	badgeW := lipgloss.Width(badge)

	fixed := 2 + max(prNumW, len(numText)) + 2 + 2 + badgeW
	if fixed+1 > width {
		return "  " + num + numPad
	}
	titleW := width - fixed
	title := ui.Truncate(pr.Title, titleW)
	title += strings.Repeat(" ", max(titleW-lipgloss.Width(title), 0))
	return "  " + num + numPad + "  " + valueStyle().Render(title) + "  " + badge
}
```

In `model.go`, delete the moved and unused helpers and the now-unused imports (`fmt`, `strings`, `lipgloss`, `common`, `git` if unused). Keep `Model`, `New`, `Init`, `Update`, `onKeyMsg`, `Keys`, `clampBlockerCursor`, `selectedBlocker`.

The inline `press x again to delete` text in the blocker row is gone. Task 11 moves that prompt to the footer.

- [ ] **Step 6: Run tests**

Run: `task test`
Expected: PASS. If the `"used just now"` assertion is flaky, it's because `LastUsedAt: time.Now()` is under a minute old by construction, so it shouldn't be.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/sessiondetail internal/tui/sessionlist
git commit -m "feat(tui): focused session detail panel with PRs grouped by workspace

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: Full-screen detail interactions (status, `o`, keys)

**Files:**
- Modify: `internal/tui/sessiondetail/model.go`, `internal/tui/sessiondetail/keys.go`
- Create: `internal/tui/sessiondetail/interactions_test.go`

**Interfaces:**
- Consumes: `ui.StatusReporter`, `ui.Status`, `ui.OpenablePRs`, `ui.NewPRPicker`, `ui.OpenURL`, `ui.OpenURLFailedMsg`.
- Produces:
  - `func (m Model) Status() ui.Status`
  - fields `picker ui.PRPicker`, `statusMsg string`, `statusKind ui.StatusKind`
  - `keys.OpenPR`

- [ ] **Step 1: Write the failing tests**

```go
package sessiondetail

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetail_DeletePromptInFooter(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeArchivedSession()})
	m, _ = m.Update(runeKey("d"))
	assert.Equal(t, ui.Status{Text: "press d again to delete archived (removes worktrees)", Kind: ui.StatusWarning}, m.Status())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, m.Status().Empty())
}

func TestDetail_BlockerDeletePromptInFooter(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, _ = m.Update(runeKey("x"))
	assert.Equal(t, ui.StatusWarning, m.Status().Kind)
	assert.Contains(t, m.Status().Text, "press x again")
}

func TestDetail_OpenPR(t *testing.T) {
	s := richSession()
	m, _ := New().Update(SelectMsg{Session: s})
	_, cmd := m.Update(runeKey("o"))
	assert.NotNil(t, cmd)

	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ = New().Update(SelectMsg{Session: s})
	m, cmd = m.Update(runeKey("o"))
	assert.Nil(t, cmd)
	require.True(t, m.picker.IsOpen())
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, 0, m.blockerCursor)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.NotNil(t, cmd)
}

func TestDetail_EscClosesPickerBeforeGoingBack(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ := New().Update(SelectMsg{Session: s})
	m, _ = m.Update(runeKey("o"))
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, cmd)
	assert.False(t, m.picker.IsOpen())
}

func TestDetail_PickerRendersInView(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, Title: "second", State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ := New().Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = m.Update(SelectMsg{Session: s})
	m, _ = m.Update(runeKey("o"))
	out := m.View()
	assert.Len(t, strings.Split(out, "\n"), 20)
	assert.Contains(t, out, "second")
}

func TestDetail_OpenURLFailed(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	m, _ = m.Update(ui.OpenURLFailedMsg{Err: errors.New("boom")})
	assert.Equal(t, ui.StatusError, m.Status().Kind)
}

func TestDetail_ShortHelp(t *testing.T) {
	var got []string
	for _, b := range keys.ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	assert.Equal(t, []string{"enter switch", "o open PR", "b block", "a archive", "esc back"}, got)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `m.Status` / `m.picker` undefined.

- [ ] **Step 3: Implement `keys.go`**

```go
type keyMap struct {
	Activate      key.Binding
	OpenPR        key.Binding
	Repair        key.Binding
	Archive       key.Binding
	Delete        key.Binding
	AddWorkspace  key.Binding
	Up            key.Binding
	Down          key.Binding
	Block         key.Binding
	Resolve       key.Binding
	RemoveBlocker key.Binding
	Back          key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Activate, k.OpenPR, k.Block, k.Archive, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Activate, k.OpenPR, k.Back},
		{k.Block, k.Up, k.Down, k.Resolve, k.RemoveBlocker},
		{k.Archive, k.Delete, k.Repair, k.AddWorkspace},
	}
}
```

Add `OpenPR: key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open PR")),` to `keys`, and change `Activate`'s help to `key.WithHelp("enter", "switch")`. Leave the other bindings unchanged.

- [ ] **Step 4: Implement `model.go` changes**

Add these fields to `Model`:

```go
	picker     ui.PRPicker
	statusMsg  string
	statusKind ui.StatusKind
```

Add:

```go
func (m *Model) setStatus(text string, kind ui.StatusKind) {
	m.statusMsg = text
	m.statusKind = kind
}

func (m Model) Status() ui.Status {
	if m.picker.IsOpen() {
		return ui.Status{Text: "↑/↓ choose · enter open · esc cancel", Kind: ui.StatusInfo}
	}
	return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
}
```

- In `Update`, in the `SelectMsg` case: add `m.picker = m.picker.Close()` and `m.setStatus("", ui.StatusInfo)`.
- Add a case:

```go
	case ui.OpenURLFailedMsg:
		m.setStatus("could not open PR: "+msg.Err.Error(), ui.StatusError)
		return m, nil
```

At the top of `onKeyMsg`, after the `m.sess == nil` guard:

```go
	if m.picker.IsOpen() {
		var url string
		m.picker, url = m.picker.Update(msg)
		if url != "" {
			return m, ui.OpenURL(url)
		}
		return m, nil
	}
	m.setStatus("", ui.StatusInfo)
```

In the `Delete` case, replace `m.pendingDeleteID = m.sess.ID; return m, nil` with:

```go
		m.pendingDeleteID = m.sess.ID
		m.setStatus(fmt.Sprintf("press d again to delete %s (removes worktrees)", m.sess.DisplayLabel()), ui.StatusWarning)
		return m, nil
```

In the `RemoveBlocker` case, replace `m.pendingBlockerDeleteID = b.ID; return m, nil` with:

```go
		m.pendingBlockerDeleteID = b.ID
		m.setStatus("press x again to delete this blocker", ui.StatusWarning)
		return m, nil
```

Add the `OpenPR` case:

```go
	case key.Matches(msg, keys.OpenPR):
		prs := ui.OpenablePRs(m.sess.PullRequests)
		switch len(prs) {
		case 0:
			m.setStatus("no open pull requests", ui.StatusInfo)
			return m, nil
		case 1:
			return m, ui.OpenURL(prs[0].HTMLURL)
		default:
			m.picker = ui.NewPRPicker(prs)
			return m, nil
		}
```

Make the full-screen view reserve the picker's rows. In `panel.go`'s `View`, before the final `FitHeight`:

```go
	body := strings.Join(lines, "\n")
	if m.picker.IsOpen() && m.height > 0 {
		body = ui.FitHeight(body, max(m.height-m.picker.Height(), 1)) + "\n" + m.picker.View(width)
	}
	return ui.FitHeight(body, m.height)
```

(and replace the existing final return with this block).

`fmt` is needed again in `model.go` for the delete prompt, so re-add it to the imports.

- [ ] **Step 5: Run tests**

Run: `task test`
Expected: PASS, including the existing `TestSessionDetail_Delete_RequiresDoublePress` and `Delete_ResetsByInterveningKey`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/sessiondetail
git commit -m "feat(tui): footer prompts and o open-PR in full-screen session detail

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 12: Verification and cleanup

**Files:**
- Possibly modify: anything flagged by lint.

- [ ] **Step 1: Format, lint, test**

Run: `task fmt && task lint && task test`
Expected: no lint findings, all tests PASS. Fix anything reported, keeping to the no-comments rule.

- [ ] **Step 2: Remove dead code**

Run: `grep -rn "truncateWidth\|firstBlockedIndex\|listGroup\|blockedTag\|renderFooter" internal/tui`
Expected: no matches. `statusview.StatusBadge` must still compile and be used by `statusview`. Do not delete it.

- [ ] **Step 3: Manual check in tuios**

Ask the user to run the TUI from the tuios popup:

```bash
task tui:run
```

and check the following at roughly 200×40 and 80×24:
- Sections appear in order with counts. The attached session shows `◆` in its own section.
- The selected row has a soft full-row background and the `▌` gutter.
- The footer is visible on the last line. `?` opens full help; `?`/`esc` closes it. `ctrl+g` opens debug.
- ⌘-clicking a PR number opens it. `o` opens a single PR directly and shows the picker for several.
- At 80 columns the panel disappears and `i`/`→` opens the full detail view.
- Typing `?` in the new-session name field types a `?`.

- [ ] **Step 4: Commit any fixes**

```bash
git add -A internal/tui
git commit -m "chore(tui): lint and cleanup after sessions overhaul

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

Skip this commit if there's nothing to commit.
