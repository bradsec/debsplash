package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/bradsec/debsplash/internal/backup"
	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/deps"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/bradsec/debsplash/internal/lockfile"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const banner = "\ndebsplash\n"

// encryptFlags groups all encryption screen appearance flags.
type encryptFlags struct {
	halign     float64
	valign     float64
	boxColor   string
	boxOpacity float64
	textColor  string
	fontSize   int
	style      string
}

// globalFlags groups all CLI flag values.
type globalFlags struct {
	dryRun     bool
	encrypt    encryptFlags
	resize     string
	resolution string
	resWidth   int // parsed from resolution
	resHeight  int // parsed from resolution
}

var flags globalFlags
var noBanner bool
var noAutoInstall bool
var activeLock *lockfile.Lock
var activeLegacyLock *lockfile.Lock
var lockPath = "/var/run/debsplash.lock"
var legacyLockPath = config.LegacyLockPath

// backupPolicy controls how errors from detection and backup are handled.
type backupPolicy struct {
	requireBackup bool // true = hard error on backup failure
}

// stdinReader is an injectable seam for reading user input (for testing).
var stdinReader = func() string {
	var answer string
	fmt.Scanln(&answer)
	return answer
}

var rootCmd = &cobra.Command{
	Use:   "debsplash",
	Short: "Change splash screen images on Debian Linux",
	Long: `debsplash - Change splash screen images on Debian Linux

Applies images to GRUB, Plymouth (encryption/boot), and desktop login screens.
Modifying commands require root (sudo debsplash ...).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Run: func(cmd *cobra.Command, args []string) {
		if !noBanner && term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Print(banner)
		}
		cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		name := cmd.Name()
		if name == "debsplash" || name == "help" || name == "status" || name == "completion" || name == "__complete" || name == "__completeNoDesc" {
			return nil
		}

		if os.Geteuid() != 0 {
			return errors.New("this tool must be run as root (use sudo)")
		}

		// Acquire filesystem lock for commands that modify system files.
		if !flags.dryRun {
			legacy, err := lockfile.AcquirePath(legacyLockPath)
			if err != nil {
				return err
			}
			activeLegacyLock = legacy
			lock, err := lockfile.AcquirePath(lockPath)
			if err != nil {
				return err
			}
			activeLock = lock
		}
		if !detect.IsDebian() {
			return errors.New("debsplash is designed for Debian-based systems only")
		}
		if noAutoInstall {
			deps.AutoInstallEnabled = false
		}
		if err := validateGlobalFlags(); err != nil {
			return err
		}
		return backup.MigrateState(flags.dryRun)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flags.dryRun, "dry-run", false, "Show what would be changed without making modifications")
	rootCmd.PersistentFlags().StringVar(&flags.resize, "resize", "fill", "Image resize mode: none, fit, fill, crop")
	rootCmd.PersistentFlags().StringVar(&flags.resolution, "resolution", "", "Output size WIDTHxHEIGHT for all targets, overriding detection (e.g. 2560x1440). Default: GRUB 1920x1080; Plymouth and login use the native mode of the single connected display, else 1920x1080")
	rootCmd.PersistentFlags().BoolVar(&noAutoInstall, "no-auto-install", false, "Don't auto-install missing dependencies")
	rootCmd.PersistentFlags().BoolVar(&noBanner, "no-banner", false, "Suppress banner")

	applyCmd.Flags().StringVar(&applyTargetsFlag, "targets", "", "Comma-separated targets to apply (grub,plymouth,login)")
	applyCmd.Flags().StringVar(&applySkipFlag, "skip", "", "Comma-separated targets to skip (grub,plymouth,login)")

	addEncryptFlags(applyCmd)
	addEncryptFlags(encryptCmd)

	restoreCmd.Flags().BoolVarP(&forceRestore, "force", "f", false, "Skip confirmation prompt")
	restoreOriginalCmd.Flags().BoolVarP(&forceRestoreOriginal, "force", "f", false, "Skip confirmation prompt")

	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(grubCmd)
	rootCmd.AddCommand(encryptCmd)
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(backupCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(restoreOriginalCmd)
	rootCmd.AddCommand(statusCmd)
}

func addEncryptFlags(cmd *cobra.Command) {
	cmd.Flags().Float64Var(&flags.encrypt.halign, "halign", 0.5, "Horizontal position of password box (0.0-1.0)")
	cmd.Flags().Float64Var(&flags.encrypt.valign, "valign", 0.7, "Vertical position of password box (0.0-1.0)")
	cmd.Flags().StringVar(&flags.encrypt.boxColor, "box-color", "#000000", "Background color of password box (hex)")
	cmd.Flags().Float64Var(&flags.encrypt.boxOpacity, "box-opacity", 0.7, "Opacity of password box (0.0-1.0)")
	cmd.Flags().StringVar(&flags.encrypt.textColor, "text-color", "#FFFFFF", "Text color for password prompt (hex)")
	cmd.Flags().IntVar(&flags.encrypt.fontSize, "font-size", 14, "Font size for password prompt")
	cmd.Flags().StringVar(&flags.encrypt.style, "style", "boxed", "Password box style: minimal, boxed, floating")
}

// Execute runs the root cobra command.
func Execute() error {
	err := rootCmd.Execute()
	if activeLock != nil {
		activeLock.Release()
	}
	if activeLegacyLock != nil {
		activeLegacyLock.Release()
	}
	return err
}
