package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/aerospace"
	"github.com/eleonorayaya/utena/internal/picker"
	"github.com/eleonorayaya/utena/internal/shellinit"
	"github.com/eleonorayaya/utena/internal/tui"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tuios"
)

var (
	defaultPort   = "3333"
	defaultLogDir = filepath.Join(os.Getenv("HOME"), "Library", "Logs", "utena")
)

func main() {
	disableResponseCompression()
	os.Args = rewriteArgs(os.Args)
	rootCmd := &cobra.Command{
		Use:          "utena",
		Short:        "Utena workspace manager for tuios",
		SilenceUsage: true,
		RunE:         runTUI,
	}

	rootCmd.Flags().String("port", defaultPort, "daemon port")

	rootCmd.AddCommand(shellInitCmd())
	rootCmd.AddCommand(sessionsCmd())
	rootCmd.AddCommand(statusLineCmd())
	rootCmd.AddCommand(monitorCmd())
	rootCmd.AddCommand(attachCmd())
	rootCmd.AddCommand(workspaceCmd())
	rootCmd.AddCommand(hookCmd())
	rootCmd.AddCommand(topCmd())
	rootCmd.AddCommand(openCmd())
	rootCmd.AddCommand(pickerCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func disableResponseCompression() {
	http.DefaultTransport.(*http.Transport).DisableCompression = true
}

func setupLogging() {
	logfilePath := os.Getenv("BUBBLETEA_LOG")
	if logfilePath == "" {
		if err := os.MkdirAll(defaultLogDir, 0755); err != nil {
			log.Fatalf("failed to create log directory: %v", err)
		}
		logfilePath = filepath.Join(defaultLogDir, "tui.log")
	}
	if _, err := tea.LogToFile(logfilePath, "utena"); err != nil {
		log.Fatal(err)
	}
}

func loadTheme() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	themePath := filepath.Join(homeDir, ".config", "utena", "theme.json")
	if err := theme.Load(themePath); err != nil {
		log.Printf("warning: failed to load theme: %v", err)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	port, _ := cmd.Flags().GetString("port")
	setupLogging()
	loadTheme()

	p := tea.NewProgram(tui.NewApp(port, router.SessionListView, appOptions(os.Getenv)...), programOptions(os.Getenv)...)
	_, err := p.Run()
	if errors.Is(err, tea.ErrInterrupted) {
		err = nil
	}
	closeOwnPopup()
	returnPickerFocus(aerospace.NewDriver(), os.Getenv)
	return err
}

func appOptions(getenv func(string) string) []tui.Option {
	if getenv(picker.EnvPicker) == "" {
		return nil
	}
	opts := []tui.Option{tui.WithEscQuits()}
	if workspace := getenv(picker.EnvPark); workspace != "" {
		title := getenv(picker.EnvTitle)
		opts = append(opts, tui.WithPark(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), wmTimeout)
			defer cancel()
			return picker.Park(ctx, aerospace.NewDriver(), title, workspace, picker.DefaultReturnPath())
		}))
	}
	return opts
}

func programOptions(getenv func(string) string) []tea.ProgramOption {
	if getenv(picker.EnvPicker) == "" || getenv(picker.EnvPark) == "" {
		return nil
	}
	return []tea.ProgramOption{tea.WithFilter(tui.ParkFilter), tea.WithReportFocus()}
}

func closeOwnPopup() {
	window := os.Getenv("TUIOS_WINDOW_ID")
	if os.Getenv("UTENA_POPUP") == "" || window == "" {
		return
	}
	if err := tuios.CloseWindow(os.Getenv("TUIOS_SESSION"), window); err != nil {
		log.Printf("[ERROR] close popup window: %v", err)
	}
}

func shellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init",
		Short: "Print shell initialization script for eval",
		Long:  `Outputs a shell script that sets up the utena environment. Add to your .zshrc: eval "$(utena shell-init)"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(shellinit.Script())
			return nil
		},
	}
}
