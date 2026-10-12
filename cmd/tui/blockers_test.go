package main

import (
	"strings"
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

func TestResolveSession_SkipsDeletedAndPrefersVisible(t *testing.T) {
	sessions := []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "foo", Status: session.StatusDeleted},
		{Model: gorm.Model{ID: 2}, Name: "foo", Status: session.StatusArchived},
		{Model: gorm.Model{ID: 3}, Name: "foo", Status: session.StatusActive},
		{Model: gorm.Model{ID: 4}, Name: "old", Status: session.StatusArchived},
		{Model: gorm.Model{ID: 5}, Name: "gone", Status: session.StatusDeleted},
	}

	s, err := resolveSession(sessions, "foo", "")
	if err != nil || s.ID != 3 {
		t.Fatalf("expected live foo (3), got %v, %v", s, err)
	}

	s, err = resolveSession(sessions, "old", "")
	if err != nil || s.ID != 4 {
		t.Fatalf("expected archived old (4) when it is the only match, got %v, %v", s, err)
	}

	if _, err := resolveSession(sessions, "gone", ""); err == nil {
		t.Fatal("expected deleted session to be unresolvable by name")
	}
}

func TestResolveSession_AmbiguousNameErrors(t *testing.T) {
	sessions := []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "foo", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "foo", Status: session.StatusInactive},
	}
	_, err := resolveSession(sessions, "foo", "")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "1") || !strings.Contains(err.Error(), "2") {
		t.Fatalf("error should list candidate ids: %v", err)
	}
}
