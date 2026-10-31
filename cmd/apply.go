package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/bradsec/debsplash/internal/grub"
	"github.com/bradsec/debsplash/internal/imgutil"
	"github.com/bradsec/debsplash/internal/loginmgr"
	"github.com/bradsec/debsplash/internal/plymouth"
	"github.com/spf13/cobra"
)

var (
	applyTargetsFlag string
	applySkipFlag    string
	grubApply        = grub.Apply
	plymouthApply    = plymouth.Apply
	loginApply       = loginmgr.Apply
)

var applyCmd = &cobra.Command{
	Use:   "apply <image>",
	Short: "Apply an image to all detected splash targets",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return validateEncryptFlags()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		imgPath, cleanup, err := validateAndResolveImage(args[0])
		if err != nil {
			return err
		}
		defer cleanup()

		cfg := buildConfig()

		env, err := detectAndBackup(cfg, backupPolicy{requireBackup: true})
		if err != nil {
			return err
		}
		env.Print()

		targets, err := resolveTargets(env, applyTargetsFlag, applySkipFlag)
		if err != nil {
			return err
		}

		resolveTargetDimensions(cfg, targets)

		var errs []error
		var succeeded []config.Target
		for _, target := range targets {
			if err := applyTarget(target, imgPath, cfg, env); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", target, err))
			} else {
				succeeded = append(succeeded, target)
			}
		}

		if len(errs) > 0 {
			fmt.Println("\n[debsplash] Completed with errors:")
			if len(succeeded) > 0 {
				fmt.Printf("  Succeeded: ")
				for i, t := range succeeded {
					if i > 0 {
						fmt.Print(", ")
					}
					fmt.Print(string(t))
				}
				fmt.Println()
			}
			fmt.Println("  Failed:")
			for _, e := range errs {
				fmt.Printf("    - %v\n", e)
			}
			fmt.Println("\n  To undo all changes, run: debsplash restore")
			return errors.New("one or more targets failed (see above)")
		}

		if cfg.DryRun {
			fmt.Println("\n[debsplash] Dry run complete. No splash screens updated.")
			return nil
		}
		fmt.Println("\n[debsplash] All splash screens updated successfully.")
		return nil
	},
}

// resolveTargets determines which targets to apply to based on flags and detected environment.
func resolveTargets(env *detect.Environment, targetsFlag, skipFlag string) ([]config.Target, error) {
	if targetsFlag != "" && skipFlag != "" {
		return nil, errors.New("--targets and --skip are mutually exclusive")
	}

	// Start from all detected targets.
	available := make(map[config.Target]bool)
	if env.HasGrub {
		available[config.TargetGrub] = true
	}
	if env.HasPlymouth {
		available[config.TargetPlymouth] = true
	}
	if env.LoginManager == detect.LMLY {
		fmt.Println("  [warning] Ly login manager is unsupported; skipping login target")
	} else if env.LoginManager != detect.LMUnknown {
		available[config.TargetLogin] = true
	}

	if targetsFlag != "" {
		requested, err := config.ParseTargets(targetsFlag)
		if err != nil {
			return nil, err
		}
		var result []config.Target
		for _, t := range requested {
			if !available[t] {
				fmt.Printf("  [warning] Target %q requested but not available on this system\n", t)
				continue
			}
			result = append(result, t)
		}
		if len(result) == 0 {
			return nil, errors.New("no requested targets are available on this system")
		}
		return result, nil
	}

	if skipFlag != "" {
		skipped, err := config.ParseTargets(skipFlag)
		if err != nil {
			return nil, err
		}
		skipSet := make(map[config.Target]bool)
		for _, t := range skipped {
			skipSet[t] = true
		}
		var result []config.Target
		for _, t := range config.AllTargets {
			if available[t] && !skipSet[t] {
				result = append(result, t)
			}
		}
		if len(result) == 0 {
			return nil, errors.New("all available targets were skipped")
		}
		return result, nil
	}

	// Default: all available targets in canonical order.
	var result []config.Target
	for _, t := range config.AllTargets {
		if available[t] {
			result = append(result, t)
		}
	}
	return result, nil
}

// applyTarget applies the image to a single target, with optional resize processing.
func applyTarget(target config.Target, imgPath string, cfg *config.Config, env *detect.Environment) error {
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
		fmt.Println("[debsplash] Applying GRUB background...")
		return grubApply(finalPath)
	case config.TargetPlymouth:
		fmt.Println("[debsplash] Applying Plymouth (encrypt/boot) splash...")
		return plymouthApply(finalPath, cfg.EncryptScreen)
	case config.TargetLogin:
		fmt.Println("[debsplash] Applying login screen background...")
		return loginApply(finalPath, env.LoginManager)
	default:
		return fmt.Errorf("unknown target %q", target)
	}
}

// processImageForTarget resizes the image if resize mode is active.
// Returns the path to use and an optional cleanup function.
func processImageForTarget(target config.Target, imgPath string, cfg *config.Config) (string, func(), error) {
	if cfg.Resize == config.ResizeNone {
		return imgPath, nil, nil
	}

	dims, ok := cfg.TargetDimensions[target]
	if !ok {
		return imgPath, nil, nil
	}

	tmpFile, err := os.CreateTemp(os.TempDir(), fmt.Sprintf("debsplash-%s-*.png", target))
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	tmpFile.Close()

	fmt.Printf("  [resize] Processing image for %s (%dx%d, mode=%s)...\n", target, dims.Width, dims.Height, cfg.Resize)
	if err := imgutil.ProcessImage(imgPath, tmpPath, dims, cfg.Resize); err != nil {
		os.Remove(tmpPath)
		return "", nil, fmt.Errorf("image processing failed: %w", err)
	}

	cleanup := func() { os.Remove(tmpPath) }
	return tmpPath, cleanup, nil
}
