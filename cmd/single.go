package cmd

import (
	"fmt"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/grub"
	"github.com/bradsec/debsplash/internal/loginmgr"
	"github.com/bradsec/debsplash/internal/plymouth"
	"github.com/spf13/cobra"
)

// runSingleTarget validates the image, sets up config, runs backup, and applies to a single target.
func runSingleTarget(target config.Target, rawImagePath string, policy backupPolicy) error {
	imgPath, cleanupSnapshot, err := validateAndResolveImage(rawImagePath)
	if err != nil {
		return err
	}
	defer cleanupSnapshot()

	cfg := buildConfig()
	env, err := detectAndBackup(cfg, policy)
	if err != nil {
		return err
	}

	resolveTargetDimensions(cfg, []config.Target{target})

	if cfg.DryRun {
		fmt.Printf("[debsplash] Dry run: would apply image to %s splash screen.\n", target)
		return nil
	}

	finalPath, cleanup, err := processImageForTarget(target, imgPath, cfg)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}

	switch target {
	case config.TargetGrub:
		return grub.Apply(finalPath)
	case config.TargetPlymouth:
		return plymouth.Apply(finalPath, cfg.EncryptScreen)
	case config.TargetLogin:
		return loginmgr.Apply(finalPath, env.LoginManager)
	default:
		return fmt.Errorf("unknown target %q", target)
	}
}

var grubCmd = &cobra.Command{
	Use:   "grub <image>",
	Short: "Change only the GRUB background",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSingleTarget(config.TargetGrub, args[0], backupPolicy{requireBackup: false})
	},
}

var encryptCmd = &cobra.Command{
	Use:   "encrypt <image>",
	Short: "Change only the encryption/Plymouth splash",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return validateEncryptFlags()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSingleTarget(config.TargetPlymouth, args[0], backupPolicy{requireBackup: false})
	},
}

var loginCmd = &cobra.Command{
	Use:   "login <image>",
	Short: "Change only the desktop login screen background",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSingleTarget(config.TargetLogin, args[0], backupPolicy{requireBackup: false})
	},
}
