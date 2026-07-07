package main

import (
	"slices"
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
)

func TestFilterSessions(t *testing.T) {
	sessions := []session.Session{
		{Name: "act", Status: session.StatusActive},
		{Name: "arch", Status: session.StatusArchived},
		{Name: "broke", Status: session.StatusBroken},
		{Name: "del", Status: session.StatusDeleted},
	}

	names := func(ss []session.Session) []string {
		out := make([]string, len(ss))
		for i, s := range ss {
			out[i] = s.Name
		}
		return out
	}

	def := names(filterSessions(sessions, false))
	if want := []string{"act"}; !slices.Equal(def, want) {
		t.Fatalf("default: got %v, want %v", def, want)
	}

	all := names(filterSessions(sessions, true))
	if want := []string{"act", "arch", "broke"}; !slices.Equal(all, want) {
		t.Fatalf("--all: got %v, want %v (deleted must never appear)", all, want)
	}
}
