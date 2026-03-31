package theme

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestDefault(t *testing.T) {
	d := Default()
	v := reflect.ValueOf(d)
	for i := 0; i < v.NumField(); i++ {
		name := v.Type().Field(i).Name
		val := v.Field(i).String()
		if val == "" {
			t.Errorf("Default().%s is empty", name)
		}
	}
}

func TestLoadNonExistent(t *testing.T) {
	Current = Default()
	err := Load("/tmp/nonexistent-theme-file.json")
	if err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
	if Current.Primary != Default().Primary {
		t.Error("Current should remain default when file is missing")
	}
}

func TestLoadPartialOverride(t *testing.T) {
	Current = Default()
	dir := t.TempDir()
	p := filepath.Join(dir, "theme.json")
	os.WriteFile(p, []byte(`{"primary": "#ff0000", "text": "#111111"}`), 0644)

	if err := Load(p); err != nil {
		t.Fatal(err)
	}
	if Current.Primary != lipgloss.Color("#ff0000") {
		t.Errorf("Primary = %q, want #ff0000", Current.Primary)
	}
	if Current.Text != lipgloss.Color("#111111") {
		t.Errorf("Text = %q, want #111111", Current.Text)
	}
	if Current.Secondary != Default().Secondary {
		t.Error("Secondary should remain default")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	Current = Default()
	dir := t.TempDir()
	p := filepath.Join(dir, "theme.json")
	os.WriteFile(p, []byte(`{invalid`), 0644)

	err := Load(p)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
