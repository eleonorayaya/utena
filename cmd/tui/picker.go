package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/aerospace"
	"github.com/eleonorayaya/utena/internal/picker"
	"github.com/eleonorayaya/utena/internal/wm"
)

const (
	defaultPickerTitle = "utena-picker"
	wmTimeout          = 2 * time.Second
)

func pickerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "picker [flags] -- <terminal command>...",
		Short: "Focus the utena picker window, or open it in a new terminal window",
		Long: `Focuses the open picker window (matched by its exact title among the terminal app's windows through AeroSpace), or runs the terminal command with the utena TUI appended in picker mode.
The terminal command must give its window the --title value and run its trailing arguments as the program.
Focus returns to the window that was focused before when the picker exits.`,
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			setupLogging()
			appID, _ := cmd.Flags().GetString("app-id")
			title, _ := cmd.Flags().GetString("title")
			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve utena executable: %w", err)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), wmTimeout)
			defer cancel()
			opener := picker.Opener{
				WM:       aerospace.NewDriver(),
				Launcher: picker.ExecLauncher{},
				AppID:    appID,
				Title:    title,
				Utena:    self,
			}
			if err := opener.Open(ctx, args); err != nil {
				log.Printf("[ERROR] picker: %v", err)
				return err
			}
			return nil
		},
	}
	cmd.Flags().String("app-id", "", "app bundle ID of the terminal, used to find an open picker window")
	cmd.Flags().String("title", defaultPickerTitle, "exact window title of the picker window")
	return cmd
}

func returnPickerFocus(d wm.Driver, getenv func(string) string) {
	ctx, cancel := context.WithTimeout(context.Background(), wmTimeout)
	defer cancel()
	if err := picker.ReturnFocus(ctx, d, getenv); err != nil {
		log.Printf("[WARN] %v", err)
	}
}
