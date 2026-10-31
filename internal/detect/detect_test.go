package detect

import (
	"fmt"
	"testing"
)

// saveAndRestore saves all injectable function vars and restores them via t.Cleanup.
func saveAndRestore(t *testing.T) {
	t.Helper()
	origProcessRunning := processRunningFn
	origCommandExists := commandExistsFn
	origFileExists := fileExistsFn
	origReadLink := readLinkFn
	origGetenv := getenvFn
	origIsDebian := isDebianFn
	origReadFile := readFileFn
	origExecOutput := execOutputFn
	t.Cleanup(func() {
		processRunningFn = origProcessRunning
		commandExistsFn = origCommandExists
		fileExistsFn = origFileExists
		readLinkFn = origReadLink
		getenvFn = origGetenv
		isDebianFn = origIsDebian
		readFileFn = origReadFile
		execOutputFn = origExecOutput
	})
}

func TestDetectDesktop_EnvVar(t *testing.T) {
	tests := []struct {
		envVal   string
		expected DesktopEnvironment
	}{
		{"GNOME", DEGNOME},
		{"ubuntu:GNOME", DEGNOME},
		{"KDE", DEKDE},
		{"plasma", DEKDE},
		{"XFCE", DEXFCE},
		{"MATE", DEMATE},
		{"LXDE", DELXDE},
		{"X-Cinnamon", DECinnamon},
		{"i3", DEI3},
		{"openbox", DEOpenbox},
	}

	for _, tt := range tests {
		t.Run(tt.envVal, func(t *testing.T) {
			saveAndRestore(t)
			getenvFn = func(key string) string {
				if key == "XDG_CURRENT_DESKTOP" {
					return tt.envVal
				}
				return ""
			}
			processRunningFn = func(string) bool { return false }

			got := detectDesktop()
			if got != tt.expected {
				t.Errorf("detectDesktop() with XDG_CURRENT_DESKTOP=%q = %q, want %q", tt.envVal, got, tt.expected)
			}
		})
	}
}

func TestDetectDesktop_ProcessFallback(t *testing.T) {
	tests := []struct {
		process  string
		expected DesktopEnvironment
	}{
		{"gnome-shell", DEGNOME},
		{"plasmashell", DEKDE},
		{"xfce4-session", DEXFCE},
		{"mate-session", DEMATE},
		{"lxsession", DELXDE},
		{"cinnamon", DECinnamon},
		{"i3", DEI3},
		{"openbox", DEOpenbox},
	}

	for _, tt := range tests {
		t.Run(tt.process, func(t *testing.T) {
			saveAndRestore(t)
			// No env vars set.
			getenvFn = func(string) string { return "" }
			processRunningFn = func(name string) bool {
				return name == tt.process
			}

			got := detectDesktop()
			if got != tt.expected {
				t.Errorf("detectDesktop() with process %q = %q, want %q", tt.process, got, tt.expected)
			}
		})
	}
}

func TestDetectDesktop_Unknown(t *testing.T) {
	saveAndRestore(t)
	getenvFn = func(string) string { return "" }
	processRunningFn = func(string) bool { return false }

	got := detectDesktop()
	if got != DEUnknown {
		t.Errorf("detectDesktop() = %q, want %q", got, DEUnknown)
	}
}

func TestDetectLoginManager_Symlink(t *testing.T) {
	tests := []struct {
		target   string
		expected LoginManager
	}{
		{"/lib/systemd/system/gdm.service", LMGDM},
		{"/lib/systemd/system/gdm3.service", LMGDM},
		{"/lib/systemd/system/lightdm.service", LMLightDM},
		{"/lib/systemd/system/sddm.service", LMSDDM},
		{"/lib/systemd/system/slim.service", LMSlim},
		{"/lib/systemd/system/ly.service", LMLY},
		{"/lib/systemd/system/plymouth.service", LMUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			saveAndRestore(t)
			readLinkFn = func(name string) (string, error) {
				if name == "/etc/systemd/system/display-manager.service" {
					return tt.target, nil
				}
				return "", fmt.Errorf("not found")
			}
			fileExistsFn = func(string) bool { return false }
			processRunningFn = func(string) bool { return false }

			got := detectLoginManager()
			if got != tt.expected {
				t.Errorf("detectLoginManager() with symlink %q = %q, want %q", tt.target, got, tt.expected)
			}
		})
	}
}

func TestDetectLoginManager_ConfigFallback(t *testing.T) {
	tests := []struct {
		configPath string
		expected   LoginManager
	}{
		{"/etc/gdm3/daemon.conf", LMGDM},
		{"/etc/gdm/custom.conf", LMGDM},
		{"/etc/lightdm/lightdm.conf", LMLightDM},
		{"/etc/sddm.conf", LMSDDM},
		{"/etc/slim.conf", LMSlim},
	}

	for _, tt := range tests {
		t.Run(tt.configPath, func(t *testing.T) {
			saveAndRestore(t)
			// Symlink fails.
			readLinkFn = func(string) (string, error) {
				return "", fmt.Errorf("not found")
			}
			fileExistsFn = func(path string) bool {
				return path == tt.configPath
			}
			processRunningFn = func(string) bool { return false }

			got := detectLoginManager()
			if got != tt.expected {
				t.Errorf("detectLoginManager() with config %q = %q, want %q", tt.configPath, got, tt.expected)
			}
		})
	}
}

func TestDetectLoginManager_ProcessFallback(t *testing.T) {
	saveAndRestore(t)
	readLinkFn = func(string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	fileExistsFn = func(string) bool { return false }
	processRunningFn = func(name string) bool {
		return name == "sddm"
	}

	got := detectLoginManager()
	if got != LMSDDM {
		t.Errorf("detectLoginManager() with sddm process = %q, want %q", got, LMSDDM)
	}
}

func TestDetectLoginManager_Unknown(t *testing.T) {
	saveAndRestore(t)
	readLinkFn = func(string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	fileExistsFn = func(string) bool { return false }
	processRunningFn = func(string) bool { return false }

	got := detectLoginManager()
	if got != LMUnknown {
		t.Errorf("detectLoginManager() = %q, want %q", got, LMUnknown)
	}
}

// TestDeterministicPriority_GDMBeatsLightDM verifies that when both GDM and
// LightDM config files exist, GDM is returned (it appears first in the ordered slice).
func TestDeterministicPriority_GDMBeatsLightDM(t *testing.T) {
	saveAndRestore(t)
	readLinkFn = func(string) (string, error) {
		return "", fmt.Errorf("not found")
	}
	fileExistsFn = func(path string) bool {
		switch path {
		case "/etc/gdm3/daemon.conf", "/etc/lightdm/lightdm.conf":
			return true
		}
		return false
	}
	processRunningFn = func(string) bool { return false }

	got := detectLoginManager()
	if got != LMGDM {
		t.Errorf("detectLoginManager() when both GDM and LightDM configs exist = %q, want %q (GDM should win by priority)", got, LMGDM)
	}
}

// TestDeterministicPriority_DesktopProcessOrder verifies GNOME wins over KDE
// when both processes are running.
func TestDeterministicPriority_DesktopProcessOrder(t *testing.T) {
	saveAndRestore(t)
	getenvFn = func(string) string { return "" }
	processRunningFn = func(name string) bool {
		return name == "gnome-shell" || name == "plasmashell"
	}

	got := detectDesktop()
	if got != DEGNOME {
		t.Errorf("detectDesktop() when both GNOME and KDE running = %q, want %q (GNOME should win by priority)", got, DEGNOME)
	}
}

func TestDetectEnvironment_UsesInjectables(t *testing.T) {
	saveAndRestore(t)
	getenvFn = func(key string) string {
		if key == "XDG_CURRENT_DESKTOP" {
			return "XFCE"
		}
		return ""
	}
	readLinkFn = func(string) (string, error) {
		return "/lib/systemd/system/lightdm.service", nil
	}
	commandExistsFn = func(cmd string) bool {
		return cmd == "plymouth"
	}
	fileExistsFn = func(path string) bool {
		return path == "/etc/default/grub"
	}
	processRunningFn = func(string) bool { return false }

	env, err := DetectEnvironment()
	if err != nil {
		t.Fatalf("DetectEnvironment() error = %v", err)
	}
	if env.Desktop != DEXFCE {
		t.Errorf("Desktop = %q, want %q", env.Desktop, DEXFCE)
	}
	if env.LoginManager != LMLightDM {
		t.Errorf("LoginManager = %q, want %q", env.LoginManager, LMLightDM)
	}
	if !env.HasPlymouth {
		t.Error("HasPlymouth = false, want true")
	}
	if !env.HasGrub {
		t.Error("HasGrub = false, want true")
	}
}

func TestIsDebian(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "Debian",
			content:  "ID=debian\nVERSION_ID=\"12\"\n",
			expected: true,
		},
		{
			name:     "Ubuntu",
			content:  "ID=ubuntu\nID_LIKE=debian\nVERSION_ID=\"22.04\"\n",
			expected: true,
		},
		{
			name:     "LinuxMint",
			content:  "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n",
			expected: true,
		},
		{
			name:     "Fedora",
			content:  "ID=fedora\nVERSION_ID=39\n",
			expected: false,
		},
		{
			name:     "Arch",
			content:  "ID=arch\nID_LIKE=\n",
			expected: false,
		},
		{
			name:     "NoFile",
			content:  "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			saveAndRestore(t)
			if tt.content == "" {
				readFileFn = func(string) ([]byte, error) {
					return nil, fmt.Errorf("not found")
				}
			} else {
				readFileFn = func(string) ([]byte, error) {
					return []byte(tt.content), nil
				}
			}
			isDebianFn = isDebian // reset to real impl using mocked readFileFn

			got := IsDebian()
			if got != tt.expected {
				t.Errorf("IsDebian() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDetectScreenResolution_Xrandr(t *testing.T) {
	saveAndRestore(t)
	// Ensure we're on an X11 session so xrandr is attempted.
	getenvFn = func(key string) string {
		if key == "XDG_SESSION_TYPE" {
			return "x11"
		}
		return ""
	}
	execOutputFn = func(name string, args ...string) ([]byte, error) {
		if name == "xrandr" {
			return []byte("Screen 0: minimum 8 x 8\nHDMI-1 connected 2560x1440+0+0\n   2560x1440     59.95*+\n   1920x1080     60.00\n"), nil
		}
		return nil, fmt.Errorf("not found")
	}

	w, h := DetectScreenResolution()
	if w != 2560 || h != 1440 {
		t.Errorf("DetectScreenResolution() = %dx%d, want 2560x1440", w, h)
	}
}

func TestDetectScreenResolution_SkipsXrandrOnWayland(t *testing.T) {
	saveAndRestore(t)
	getenvFn = func(key string) string {
		if key == "XDG_SESSION_TYPE" {
			return "wayland"
		}
		return ""
	}
	xrandrCalled := false
	execOutputFn = func(name string, args ...string) ([]byte, error) {
		if name == "xrandr" {
			xrandrCalled = true
		}
		return nil, fmt.Errorf("not found")
	}

	DetectScreenResolution()
	if xrandrCalled {
		t.Error("xrandr should not be called on Wayland sessions")
	}
}

func TestDetectScreenResolution_Fallback(t *testing.T) {
	saveAndRestore(t)
	execOutputFn = func(string, ...string) ([]byte, error) {
		return nil, fmt.Errorf("not found")
	}

	w, h := DetectScreenResolution()
	// Can't predict DRM files, just verify it returns without panic.
	_ = w
	_ = h
}
