package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bradsec/debsplash/internal/backup"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/spf13/cobra"
)

var forceRestore bool

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore previously backed-up images and settings",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildConfig()
		env, err := detect.DetectEnvironment()
		if err != nil {
			return err
		}

		if !forceRestore && !flags.dryRun {
			fmt.Printf("This will overwrite system files from the most recent backup.\n")
			fmt.Print("Continue? [y/N] ")
			answer := stdinReader()
			if strings.ToLower(answer) != "y" {
				return errors.New("restore cancelled")
			}
		}

		return backup.Restore(cfg, env)
	},
}

var forceRestoreOriginal bool

var restoreOriginalCmd = &cobra.Command{
	Use:   "restore-original",
	Short: "Restore the original system splash screens from before debsplash was first used",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := buildConfig()
		env, err := detect.DetectEnvironment()
		if err != nil {
			return err
		}

		if !forceRestoreOriginal && !flags.dryRun {
			fmt.Printf("This will restore all splash screens to their original state (fresh install).\n")
			fmt.Print("Continue? [y/N] ")
			answer := stdinReader()
			if strings.ToLower(answer) != "y" {
				return errors.New("restore cancelled")
			}
		}

		return backup.RestoreOriginal(cfg, env)
	},
}
