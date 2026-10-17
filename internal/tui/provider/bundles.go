package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/bundles"
)

type BundlesLoadedMsg struct {
	Bundles []bundles.Entry
	Err     error
}

type DefaultBundlesLoadedMsg struct {
	WorkspaceIDs []uint
	Bundles      []string
	Err          error
}

type SessionBundlesSetMsg struct {
	SessionID uint
}

type fetchBundlesIntentMsg struct{}

type fetchDefaultBundlesIntentMsg struct{ workspaceIDs []uint }

type setSessionBundlesIntentMsg struct {
	sessionID uint
	names     []string
}

func FetchBundles() tea.Cmd {
	return func() tea.Msg { return fetchBundlesIntentMsg{} }
}

func FetchDefaultBundles(workspaceIDs []uint) tea.Cmd {
	return func() tea.Msg { return fetchDefaultBundlesIntentMsg{workspaceIDs: workspaceIDs} }
}

func SetSessionBundles(sessionID uint, names []string) tea.Cmd {
	return func() tea.Msg { return setSessionBundlesIntentMsg{sessionID: sessionID, names: names} }
}
