package main

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/claude"
)

func TestFormatStatusLine(t *testing.T) {
	if got := formatStatusLine(nil); got != "" {
		t.Fatalf("empty input: got %q, want empty string", got)
	}

	rows := []barRow{
		{Name: "review", Attention: claude.StatusReadyForReview},
		{Name: "busy", Attention: claude.StatusWorking},
		{Name: "urgent", Attention: claude.StatusNeedsAttention},
		{Name: "sleeping", Attention: claude.StatusIdle},
		{Name: "finished", Attention: claude.StatusDone},
	}

	want := "#[fg=red,bold]! urgent#[default] #[fg=green]✓ review#[default]"
	if got := formatStatusLine(rows); got != want {
		t.Fatalf("needs_attention must sort before ready_for_review regardless of input order: got %q, want %q", got, want)
	}
}
