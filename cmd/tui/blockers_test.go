package main

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
	"gorm.io/gorm"
)

func TestResolveSession(t *testing.T) {
	sessions := []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "alpha"},
		{Model: gorm.Model{ID: 2}, Name: "beta"},
	}

	s, err := resolveSession(sessions, "beta", "1")
	if err != nil || s.ID != 2 {
		t.Fatalf("by name: got %v, %v", s, err)
	}

	s, err = resolveSession(sessions, "", "1")
	if err != nil || s.ID != 1 {
		t.Fatalf("by env: got %v, %v", s, err)
	}

	if _, err := resolveSession(sessions, "", ""); err == nil {
		t.Fatal("expected error with no name and no UTENA_SESSION_ID")
	}
	if _, err := resolveSession(sessions, "nope", ""); err == nil {
		t.Fatal("expected error for unknown name")
	}
	if _, err := resolveSession(sessions, "", "99"); err == nil {
		t.Fatal("expected error for unknown env id")
	}
}
