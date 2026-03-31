package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/shellinit"
	"github.com/eleonorayaya/utena/internal/tui"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

var (
	defaultPort   = "3333"
	defaultLogDir = filepath.Join(os.Getenv("HOME"), "Library", "Logs", "utena")
)

func main() {
	rootCmd := &cobra.Command{
		Use:          "utena",
		Short:        "Utena workspace manager for tmux",
		SilenceUsage: true,
		RunE:         runTUI,
	}

	rootCmd.Flags().String("port", defaultPort, "daemon port")

	rootCmd.AddCommand(shellInitCmd())
	rootCmd.AddCommand(todosCmd())
	rootCmd.AddCommand(newTaskCmd())
	rootCmd.AddCommand(statusCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func setupLogging() string {
	logfilePath := os.Getenv("BUBBLETEA_LOG")
	if logfilePath == "" {
		os.MkdirAll(defaultLogDir, 0755)
		logfilePath = filepath.Join(defaultLogDir, "tui.log")
	}
	if _, err := tea.LogToFile(logfilePath, "utena"); err != nil {
		log.Fatal(err)
	}
	resolvedLogPath, _ := filepath.Abs(logfilePath)
	return resolvedLogPath
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
	resolvedLogPath := setupLogging()
	loadTheme()

	p := tea.NewProgram(tui.NewApp(resolvedLogPath, port, router.SessionListView))
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}

func todosCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "todos",
		Short:        "Open todo list",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			resolvedLogPath := setupLogging()
			loadTheme()

			p := tea.NewProgram(tui.NewApp(resolvedLogPath, port, router.TodoListView))
			if _, err := p.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "new-task",
		Short:        "Open new task form with current workspace preselected",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			resolvedLogPath := setupLogging()
			loadTheme()

			p := tea.NewProgram(tui.NewApp(
				resolvedLogPath, port, router.TodoListView,
				tui.WithNavigateTo(router.TodoFormView),
			))
			if _, err := p.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "status",
		Short:        "Open status bar view",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			resolvedLogPath := setupLogging()
			loadTheme()

			p := tea.NewProgram(
				tui.NewApp(resolvedLogPath, port, router.StatusView),
				tea.WithReportFocus(),
			)
			if _, err := p.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
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
