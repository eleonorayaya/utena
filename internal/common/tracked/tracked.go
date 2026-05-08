// Package tracked describes the shape shared by domain types that own a
// status enum tracking a small state machine (e.g. tmux session lifecycle,
// worktree presence, branch existence).
//
// The interface is a description, not yet a runtime tool. No generic helpers
// live here — they will be added when the same call site genuinely repeats.
package tracked

// Statuser is implemented by domain types that carry a typed-string Status
// field representing a state-machine value.
type Statuser[S ~string] interface {
	GetStatus() S
	SetStatus(S)
}

// StatusDeriver is for types whose Status is computable from sibling fields
// rather than only being driven by external observation. Implementations must
// be pure: DeriveStatus may not mutate the receiver.
type StatusDeriver[S ~string] interface {
	Statuser[S]
	DeriveStatus() S
}
