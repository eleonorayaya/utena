package main

import (
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eleonorayaya/utena/internal/tui"
)

func main() {
	var (
		opts []tea.ProgramOption
	)

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
		log.Fatal(err)
	}
}
