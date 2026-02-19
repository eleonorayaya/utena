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
)

func main() {
	rootCmd := &cobra.Command{
		Use:          "utena",
		Short:        "Utena workspace manager for Zellij",
		SilenceUsage: true,
		RunE:         runTUI,
	}

	rootCmd.AddCommand(shellInitCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	var opts []tea.ProgramOption

	var resolvedLogPath string
	logfilePath := os.Getenv("BUBBLETEA_LOG")
	if logfilePath != "" {
		if _, err := tea.LogToFile(logfilePath, "utena"); err != nil {
			log.Fatal(err)
		}
		resolvedLogPath, _ = filepath.Abs(logfilePath)
	}

	p := tea.NewProgram(tui.NewApp(resolvedLogPath), opts...)
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
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
