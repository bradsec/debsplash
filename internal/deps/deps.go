package deps

import (
	"fmt"
	"os"
	"os/exec"
)

// commandToPackage maps command names to their Debian package names.
var commandToPackage = map[string]string{
	"gresource":                  "libglib2.0-dev-bin",
	"glib-compile-resources":     "libglib2.0-dev-bin",
	"dconf":                      "dconf-cli",
	"update-grub":                "grub2-common",
	"plymouth-set-default-theme": "plymouth",
	"update-initramfs":           "initramfs-tools",
}

// lookPathFn is injectable for testing.
var lookPathFn = exec.LookPath

// execCommandFn is injectable for testing.
var execCommandFn = exec.Command

// AutoInstallEnabled controls whether EnsureCommand will auto-install missing packages.
var AutoInstallEnabled = true

// EnsureCommand checks if a command is available and installs its package via apt if not.
func EnsureCommand(cmd string) error {
	if _, err := lookPathFn(cmd); err == nil {
		return nil
	}

	pkg, ok := commandToPackage[cmd]
	if !ok {
		return fmt.Errorf("%s not found and no known package provides it", cmd)
	}

	if !AutoInstallEnabled {
		return fmt.Errorf("%s not found; install it manually: sudo apt-get install %s", cmd, pkg)
	}

	fmt.Printf("  [deps] %s not found, installing %s...\n", cmd, pkg)
	install := execCommandFn("apt-get", "install", "-y", "--no-install-recommends", pkg)
	install.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	if _, err := install.CombinedOutput(); err != nil {
		update := execCommandFn("apt-get", "update")
		update.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		if updateOut, updateErr := update.CombinedOutput(); updateErr != nil {
			return fmt.Errorf("failed to update apt package lists after install failed: %w\n%s", updateErr, updateOut)
		}
		install = execCommandFn("apt-get", "install", "-y", "--no-install-recommends", pkg)
		install.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		if out, err := install.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to install %s after updating apt package lists: %w\n%s", pkg, err, out)
		}
	}

	// Verify the command is now available.
	if _, err := lookPathFn(cmd); err != nil {
		return fmt.Errorf("%s still not found after installing %s", cmd, pkg)
	}

	fmt.Printf("  [deps] %s installed successfully.\n", pkg)
	return nil
}
