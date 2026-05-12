package tracked

type Statuser[S ~string] interface {
	GetStatus() S
	SetStatus(S)
}

// StatusDeriver: implementations must not mutate the receiver in DeriveStatus —
// callers may invoke it speculatively before deciding whether to persist.
type StatusDeriver[S ~string] interface {
	Statuser[S]
	DeriveStatus() S
}
