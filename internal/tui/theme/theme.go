package theme

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"reflect"

	"github.com/charmbracelet/lipgloss"
)

var Current = Default()

type Theme struct {
	SurfaceVariant lipgloss.Color
	SurfaceActive  lipgloss.Color
	Selection      lipgloss.Color
	Text           lipgloss.Color
	TextEmphasis   lipgloss.Color
	TextMuted      lipgloss.Color
	TextOnPrimary  lipgloss.Color
	Primary        lipgloss.Color
	PrimaryVariant lipgloss.Color
	Secondary      lipgloss.Color
	Tertiary       lipgloss.Color
	AccentBlue     lipgloss.Color
	AccentLavender lipgloss.Color
	AccentMint     lipgloss.Color
	Error          lipgloss.Color
	Path           lipgloss.Color
	StatusReady    lipgloss.Color
	StatusPending  lipgloss.Color
	StatusActive   lipgloss.Color
	ListTitle          lipgloss.Color
	ListDesc           lipgloss.Color
	ListSelectedTitle  lipgloss.Color
	ListSelectedDesc   lipgloss.Color
	ListSelectedBorder lipgloss.Color
}

func Default() Theme {
	return Theme{
		SurfaceVariant: lipgloss.Color("#dfd6cc"),
		SurfaceActive:  lipgloss.Color("#f0e8e0"),
		Selection:      lipgloss.Color("#ffd5d5"),
		Text:           lipgloss.Color("#4a4a4a"),
		TextEmphasis:   lipgloss.Color("#2a2a2a"),
		TextMuted:      lipgloss.Color("#8a7873"),
		TextOnPrimary:  lipgloss.Color("#fef6f0"),
		Primary:        lipgloss.Color("#e6537a"),
		PrimaryVariant: lipgloss.Color("#d16577"),
		Secondary:      lipgloss.Color("#d97c6e"),
		Tertiary:       lipgloss.Color("#9370b9"),
		AccentBlue:     lipgloss.Color("#6a9bc3"),
		AccentLavender: lipgloss.Color("#a87bb7"),
		AccentMint:     lipgloss.Color("#5fafa5"),
		Error:          lipgloss.Color("#cc3333"),
		Path:           lipgloss.Color("#6a9bc3"),
		StatusReady:    lipgloss.Color("#5faf5f"),
		StatusPending:  lipgloss.Color("#808080"),
		StatusActive:   lipgloss.Color("#d7a74f"),
		ListTitle:          lipgloss.Color("#303030"),
		ListDesc:           lipgloss.Color("#6c6c6c"),
		ListSelectedTitle:  lipgloss.Color("#e6537a"),
		ListSelectedDesc:   lipgloss.Color("#d16577"),
		ListSelectedBorder: lipgloss.Color("#d16577"),
	}
}

type themeFile struct {
	SurfaceVariant *string `json:"surface_variant"`
	SurfaceActive  *string `json:"surface_active"`
	Selection      *string `json:"selection"`
	Text           *string `json:"text"`
	TextEmphasis   *string `json:"text_emphasis"`
	TextMuted      *string `json:"text_muted"`
	TextOnPrimary  *string `json:"text_on_primary"`
	Primary        *string `json:"primary"`
	PrimaryVariant *string `json:"primary_variant"`
	Secondary      *string `json:"secondary"`
	Tertiary       *string `json:"tertiary"`
	AccentBlue     *string `json:"accent_blue"`
	AccentLavender *string `json:"accent_lavender"`
	AccentMint     *string `json:"accent_mint"`
	Error          *string `json:"error"`
	Path           *string `json:"path"`
	StatusReady    *string `json:"status_ready"`
	StatusPending  *string `json:"status_pending"`
	StatusActive   *string `json:"status_active"`
	ListTitle          *string `json:"list_title"`
	ListDesc           *string `json:"list_desc"`
	ListSelectedTitle  *string `json:"list_selected_title"`
	ListSelectedDesc   *string `json:"list_selected_desc"`
	ListSelectedBorder *string `json:"list_selected_border"`
}

func Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	var f themeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return err
	}

	t := Default()
	merge(&t, &f)
	Current = t
	return nil
}

func merge(t *Theme, f *themeFile) {
	tv := reflect.ValueOf(t).Elem()
	fv := reflect.ValueOf(f).Elem()

	for i := 0; i < fv.NumField(); i++ {
		ptr := fv.Field(i)
		if ptr.IsNil() {
			continue
		}
		tv.Field(i).Set(reflect.ValueOf(lipgloss.Color(ptr.Elem().String())))
	}
}
