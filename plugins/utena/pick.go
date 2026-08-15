package main

// The picker is the sidebar in a popup. Keeping a second model meant every
// change had to be made twice, and they drifted: the picker gained filtering
// the sidebar lacked, while archive, delete and new only ever worked in the
// sidebar. There is one model now; only the pane placement differs.

func runPick() error { return runSessionUI(true) }
