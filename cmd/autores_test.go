package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradsec/debsplash/internal/config"
)

type fixtureConn struct {
	name, status, modes string
}

// withDRM builds a fake sysfs tree and points drmSysfsRoot at it.
func withDRM(t *testing.T, conns ...fixtureConn) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "card1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range conns {
		dir := filepath.Join(root, c.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(c.status), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "modes"), []byte(c.modes), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := drmSysfsRoot
	drmSysfsRoot = root
	t.Cleanup(func() { drmSysfsRoot = old })
}

func withResFlag(t *testing.T, w, h int) {
	t.Helper()
	ow, oh := flags.resWidth, flags.resHeight
	flags.resWidth, flags.resHeight = w, h
	t.Cleanup(func() { flags.resWidth, flags.resHeight = ow, oh })
}

func captureOut(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

func TestResolveTargetDimensionsDetection(t *testing.T) {
	edp := fixtureConn{"card1-eDP-1", "connected\n", "2560x1440\n1920x1080\n"}
	tests := []struct {
		name  string
		conns []fixtureConn
		want  map[config.Target]string // target -> expected line (after trim)
	}{
		{"one connected", []fixtureConn{edp}, map[config.Target]string{
			config.TargetGrub:     "grub: 1920x1080 (default)",
			config.TargetPlymouth: "plymouth: 2560x1440 (detected: card1-eDP-1)",
			config.TargetLogin:    "login: 2560x1440 (detected: card1-eDP-1)",
		}},
		{"two connected", []fixtureConn{edp, {"card1-HDMI-A-1", "connected\n", "3840x2160\n"}}, map[config.Target]string{
			config.TargetGrub:     "grub: 1920x1080 (default)",
			config.TargetPlymouth: "plymouth: 1920x1080 (default; 2 connected displays found)",
			config.TargetLogin:    "login: 1920x1080 (default; 2 connected displays found)",
		}},
		{"none connected", []fixtureConn{{"card1-eDP-1", "disconnected\n", ""}}, map[config.Target]string{
			config.TargetPlymouth: "plymouth: 1920x1080 (default; no connected display found)",
		}},
		{"disconnected with modes ignored", []fixtureConn{edp, {"card1-HDMI-A-1", "disconnected\n", "3840x2160\n"}}, map[config.Target]string{
			config.TargetPlymouth: "plymouth: 2560x1440 (detected: card1-eDP-1)",
		}},
		{"status not exactly connected", []fixtureConn{{"card1-eDP-1", "connected \n", "2560x1440\n"}}, map[config.Target]string{
			config.TargetPlymouth: "plymouth: 1920x1080 (default; no connected display found)",
		}},
		{"only first mode line used", []fixtureConn{{"card0-DP-1", "connected\n", "1280x720\n3840x2160\n"}}, map[config.Target]string{
			config.TargetLogin: "login: 1280x720 (detected: card0-DP-1)",
		}},
		{"malformed mode", []fixtureConn{{"card1-eDP-1", "connected\n", "garbage\n"}}, map[config.Target]string{
			config.TargetPlymouth: `plymouth: 1920x1080 (default; unparsable mode "garbage" on card1-eDP-1)`,
		}},
		{"interlaced suffix is malformed", []fixtureConn{{"card1-eDP-1", "connected\n", "1920x1080i\n"}}, map[config.Target]string{
			config.TargetLogin: `login: 1920x1080 (default; unparsable mode "1920x1080i" on card1-eDP-1)`,
		}},
		{"empty modes", []fixtureConn{{"card1-eDP-1", "connected\n", ""}}, map[config.Target]string{
			config.TargetPlymouth: `plymouth: 1920x1080 (default; unparsable mode "" on card1-eDP-1)`,
		}},
		{"below minimum", []fixtureConn{{"card1-eDP-1", "connected\n", "100x100\n"}}, map[config.Target]string{
			config.TargetPlymouth: "plymouth: 1920x1080 (default; mode 100x100 on card1-eDP-1 is outside 320x240..7680x4320)",
		}},
		{"above maximum", []fixtureConn{{"card1-eDP-1", "connected\n", "15360x8640\n"}}, map[config.Target]string{
			config.TargetLogin: "login: 1920x1080 (default; mode 15360x8640 on card1-eDP-1 is outside 320x240..7680x4320)",
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withDRM(t, tc.conns...)
			withResFlag(t, 0, 0)
			cfg := config.DefaultConfig()
			var targets []config.Target
			for tg := range tc.want {
				targets = append(targets, tg)
			}
			out := captureOut(t, func() { resolveTargetDimensions(cfg, targets) })
			for tg, line := range tc.want {
				if !strings.Contains(out, "  "+line+"\n") {
					t.Errorf("target %s: want line %q in output:\n%s", tg, line, out)
				}
			}
			if g := cfg.TargetDimensions[config.TargetGrub]; g != (config.Dimensions{Width: 1920, Height: 1080}) {
				t.Errorf("grub dims changed to %v", g)
			}
		})
	}
}

func TestResolveTargetDimensionsMissingRoot(t *testing.T) {
	old := drmSysfsRoot
	drmSysfsRoot = filepath.Join(t.TempDir(), "absent")
	t.Cleanup(func() { drmSysfsRoot = old })
	withResFlag(t, 0, 0)
	cfg := config.DefaultConfig()
	out := captureOut(t, func() { resolveTargetDimensions(cfg, []config.Target{config.TargetPlymouth}) })
	if !strings.Contains(out, "plymouth: 1920x1080 (default; cannot read ") {
		t.Errorf("unexpected output: %s", out)
	}
}

func TestResolveTargetDimensionsExplicitWinsForEveryTarget(t *testing.T) {
	withDRM(t, fixtureConn{"card1-eDP-1", "connected\n", "2560x1440\n"})
	flagsRes := flags.resolution
	t.Cleanup(func() { flags.resolution = flagsRes })
	flags.resolution = "3840x2160"
	if err := validateGlobalFlags(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { flags.resWidth, flags.resHeight = 0, 0 })
	cfg := buildConfig()
	out := captureOut(t, func() { resolveTargetDimensions(cfg, config.AllTargets) })
	for _, tg := range config.AllTargets {
		if want := "  " + string(tg) + ": 3840x2160 (--resolution)\n"; !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
		if d := cfg.TargetDimensions[tg]; d != (config.Dimensions{Width: 3840, Height: 2160}) {
			t.Errorf("%s dims = %v", tg, d)
		}
	}
}

func TestResolveTargetDimensionsAllTargetsOutput(t *testing.T) {
	withDRM(t, fixtureConn{"card1-eDP-1", "connected\n", "2560x1440\n"})
	withResFlag(t, 0, 0)
	cfg := config.DefaultConfig()
	cfg.DryRun = true
	out := captureOut(t, func() { resolveTargetDimensions(cfg, config.AllTargets) })
	want := "  grub: 1920x1080 (default)\n  plymouth: 2560x1440 (detected: card1-eDP-1)\n  login: 2560x1440 (detected: card1-eDP-1)\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}
