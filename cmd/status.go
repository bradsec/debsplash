package cmd

import (
	"fmt"
	"strings"

	"github.com/bradsec/debsplash/internal/backup"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/spf13/cobra"
)

var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up current splash images and settings",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildConfig()
		env, err := detect.DetectEnvironment()
		if err != nil {
			return err
		}
		return backup.TakeBackup(cfg, env)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show detected desktop environment, login manager, and available targets",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := detect.DetectEnvironment()
		if err != nil {
			return err
		}
		env.Print()

		// Show available targets.
		var available []string
		if env.HasGrub {
			available = append(available, "grub")
		}
		if env.HasPlymouth {
			available = append(available, "plymouth")
		}
		if env.LoginManager == detect.LMLY {
			fmt.Println("  Login target        : unsupported (Ly)")
		} else if env.LoginManager != detect.LMUnknown {
			available = append(available, "login")
		}
		if len(available) > 0 {
			fmt.Printf("  Available targets   : %s\n", strings.Join(available, ", "))
		} else {
			fmt.Printf("  Available targets   : none detected\n")
		}

		return nil
	},
}
