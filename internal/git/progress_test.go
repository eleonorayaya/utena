package git

import (
	"bufio"
	"strings"
	"testing"
)

func TestProgressSplit_SplitsOnCarriageReturnAndNewline(t *testing.T) {
	input := "Counting objects:  10%\rCounting objects: 100%\rResolving deltas: done.\nWriting marker\n"
	scanner := bufio.NewScanner(strings.NewReader(input))
	scanner.Split(progressSplit)

	var got []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			got = append(got, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	want := []string{
		"Counting objects:  10%",
		"Counting objects: 100%",
		"Resolving deltas: done.",
		"Writing marker",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines %q, want %d %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestProgressSplit_HandlesTrailingTokenWithoutTerminator(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("no newline at end"))
	scanner.Split(progressSplit)

	var got []string
	for scanner.Scan() {
		got = append(got, scanner.Text())
	}
	if len(got) != 1 || got[0] != "no newline at end" {
		t.Fatalf("got %q, want [\"no newline at end\"]", got)
	}
}
