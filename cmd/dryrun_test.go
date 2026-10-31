package cmd

import (
	"path/filepath"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
)

func TestApplyTargetDryRunSkipsApply(t *testing.T) {
	oldGrub, oldPlymouth, oldLogin := grubApply, plymouthApply, loginApply
	t.Cleanup(func() {
		grubApply, plymouthApply, loginApply = oldGrub, oldPlymouth, oldLogin
	})
	grubApply = func(string) error { t.Error("grub Apply called during dry-run"); return nil }
	plymouthApply = func(string, config.EncryptScreenConfig) error {
		t.Error("plymouth Apply called during dry-run")
		return nil
	}
	loginApply = func(string, detect.LoginManager) error { t.Error("login Apply called during dry-run"); return nil }

	cfg := config.DefaultConfig()
	cfg.DryRun = true
	cfg.Resize = config.ResizeNone
	env := &detect.Environment{LoginManager: detect.LMGDM}
	image := filepath.Join(t.TempDir(), "image.png")
	for _, target := range config.AllTargets {
		t.Run(string(target), func(t *testing.T) {
			if err := applyTarget(target, image, cfg, env); err != nil {
				t.Fatal(err)
			}
		})
	}
}
