package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bradsec/debsplash/internal/config"
)

// drmSysfsRoot is the DRM sysfs directory; tests point it at a fixture.
var drmSysfsRoot = "/sys/class/drm"

// nativeMode is the preferred mode of the only connected display.
type nativeMode struct {
	connector     string
	width, height int
}

// detectNativeMode reads drmSysfsRoot/card*-*/{status,modes}. It succeeds only
// when exactly one connector reports status "connected" and the first line of
// its modes file (the preferred mode) is a WIDTHxHEIGHT inside the
// --resolution limits. The error text is the reason for falling back.
// xrandr is not used: it depends on the user session and DISPLAY/XAUTHORITY.
func detectNativeMode() (nativeMode, error) {
	entries, err := os.ReadDir(drmSysfsRoot)
	if err != nil {
		return nativeMode{}, fmt.Errorf("cannot read %s: %w", drmSysfsRoot, err)
	}
	var connected []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "card") || !strings.Contains(name, "-") {
			continue
		}
		status, err := os.ReadFile(filepath.Join(drmSysfsRoot, name, "status"))
		if err != nil {
			continue
		}
		if strings.TrimSuffix(string(status), "\n") == "connected" {
			connected = append(connected, name)
		}
	}
	switch len(connected) {
	case 0:
		return nativeMode{}, fmt.Errorf("no connected display found")
	case 1:
	default:
		return nativeMode{}, fmt.Errorf("%d connected displays found", len(connected))
	}

	name := connected[0]
	modes, err := os.ReadFile(filepath.Join(drmSysfsRoot, name, "modes"))
	if err != nil {
		return nativeMode{}, fmt.Errorf("cannot read modes of %s: %w", name, err)
	}
	first, _, _ := strings.Cut(string(modes), "\n")
	w, h, err := parseResolution(first)
	if err != nil {
		return nativeMode{}, fmt.Errorf("unparsable mode %q on %s", first, name)
	}
	if w < minResWidth || h < minResHeight || w > maxResWidth || h > maxResHeight {
		return nativeMode{}, fmt.Errorf("mode %dx%d on %s is outside %dx%d..%dx%d",
			w, h, name, minResWidth, minResHeight, maxResWidth, maxResHeight)
	}
	return nativeMode{connector: name, width: w, height: h}, nil
}

// resolveTargetDimensions picks the output size for each target in targets,
// stores it in cfg, and prints "<target>: WxH (<source>)". An explicit
// --resolution (already applied by buildConfig) wins; GRUB keeps its default
// because the firmware mode is unknown; Plymouth and login use the detected
// native mode, or the default with a reason when detection is not safe.
func resolveTargetDimensions(cfg *config.Config, targets []config.Target) {
	var native nativeMode
	var nativeErr error
	detected := false
	for _, t := range targets {
		d := cfg.TargetDimensions[t]
		source := "default"
		switch {
		case flags.resWidth > 0 && flags.resHeight > 0:
			source = "--resolution"
		case t == config.TargetGrub:
		default:
			if !detected {
				native, nativeErr = detectNativeMode()
				detected = true
			}
			if nativeErr != nil {
				source = "default; " + nativeErr.Error()
			} else {
				d = config.Dimensions{Width: native.width, Height: native.height}
				cfg.TargetDimensions[t] = d
				source = "detected: " + native.connector
			}
		}
		fmt.Printf("  %s: %dx%d (%s)\n", t, d.Width, d.Height, source)
	}
}
