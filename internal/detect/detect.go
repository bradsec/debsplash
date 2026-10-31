package detect

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// LoginManager represents the detected display/login manager.
type LoginManager string

const (
	LMUnknown LoginManager = "unknown"
	LMGDM     LoginManager = "gdm"     // GNOME Display Manager
	LMLightDM LoginManager = "lightdm" // LightDM (XFCE, LXDE, MATE, etc.)
	LMSDDM    LoginManager = "sddm"    // Simple Desktop Display Manager (KDE Plasma)
	LMSlim    LoginManager = "slim"    // SLiM (legacy)
	LMLY      LoginManager = "ly"      // Ly (TUI display manager)
)

// DesktopEnvironment represents the detected DE.
type DesktopEnvironment string

const (
	DEUnknown  DesktopEnvironment = "unknown"
	DEGNOME    DesktopEnvironment = "gnome"
	DEKDE      DesktopEnvironment = "kde"
	DEXFCE     DesktopEnvironment = "xfce"
	DEMATE     DesktopEnvironment = "mate"
	DELXDE     DesktopEnvironment = "lxde"
	DECinnamon DesktopEnvironment = "cinnamon"
	DEI3       DesktopEnvironment = "i3"
	DEOpenbox  DesktopEnvironment = "openbox"
)

// Environment holds all detected system info.
type Environment struct {
	Desktop      DesktopEnvironment
	LoginManager LoginManager
	HasPlymouth  bool
	HasGrub      bool
	ScreenWidth  int // 0 = unknown
	ScreenHeight int // 0 = unknown
}

// desktopCandidate maps a process name to a desktop environment.
type desktopCandidate struct {
	process string
	de      DesktopEnvironment
}

// loginManagerCandidate maps a process name to a login manager.
type loginManagerCandidate struct {
	process string
	lm      LoginManager
}

// configPathCandidate maps a config file path to a login manager.
type configPathCandidate struct {
	path string
	lm   LoginManager
}

// Injectable function vars for testing. Production code calls through these.
var (
	processRunningFn = processRunning
	commandExistsFn  = commandExists
	fileExistsFn     = fileExists
	readLinkFn       = os.Readlink
	getenvFn         = os.Getenv
	readFileFn       = os.ReadFile
	execOutputFn     = execOutput
)

// desktopProcessOrder defines desktop detection priority via process checks.
// Order: GNOME > KDE > XFCE > MATE > LXDE > Cinnamon > i3 > Openbox
var desktopProcessOrder = []desktopCandidate{
	{"gnome-shell", DEGNOME},
	{"plasmashell", DEKDE},
	{"xfce4-session", DEXFCE},
	{"mate-session", DEMATE},
	{"lxsession", DELXDE},
	{"cinnamon", DECinnamon},
	{"i3", DEI3},
	{"openbox", DEOpenbox},
}

// loginManagerProcessOrder defines login manager detection priority via process checks.
// Order: GDM > GDM3 > LightDM > SDDM > SLiM
var loginManagerProcessOrder = []loginManagerCandidate{
	{"gdm", LMGDM},
	{"gdm3", LMGDM},
	{"lightdm", LMLightDM},
	{"sddm", LMSDDM},
	{"slim", LMSlim},
}

// loginManagerConfigOrder defines login manager detection priority via config paths.
// Order: GDM > LightDM > SDDM > SLiM
var loginManagerConfigOrder = []configPathCandidate{
	{"/etc/gdm3/daemon.conf", LMGDM},
	{"/etc/gdm/custom.conf", LMGDM},
	{"/etc/lightdm/lightdm.conf", LMLightDM},
	{"/etc/sddm.conf", LMSDDM},
	{"/etc/sddm.conf.d", LMSDDM},
	{"/etc/slim.conf", LMSlim},
}

// DetectEnvironment probes the system and returns an Environment.
func DetectEnvironment() (*Environment, error) {
	env := &Environment{}
	env.Desktop = detectDesktop()
	env.LoginManager = detectLoginManager()
	env.HasPlymouth = commandExistsFn("plymouth")
	env.HasGrub = fileExistsFn("/etc/default/grub")
	env.ScreenWidth, env.ScreenHeight = DetectScreenResolution()
	return env, nil
}

// Print displays detected environment info to stdout.
func (e *Environment) Print() {
	fmt.Printf("  Desktop Environment : %s\n", e.Desktop)
	fmt.Printf("  Login Manager       : %s\n", e.LoginManager)
	fmt.Printf("  Plymouth present    : %v\n", e.HasPlymouth)
	fmt.Printf("  GRUB present        : %v\n", e.HasGrub)
	if e.ScreenWidth > 0 && e.ScreenHeight > 0 {
		fmt.Printf("  Screen resolution   : %dx%d\n", e.ScreenWidth, e.ScreenHeight)
	} else {
		fmt.Printf("  Screen resolution   : unknown\n")
	}
}

// detectDesktop checks environment variables and running processes.
func detectDesktop() DesktopEnvironment {
	// Check $XDG_CURRENT_DESKTOP and $DESKTOP_SESSION first (fast path).
	for _, envVar := range []string{"XDG_CURRENT_DESKTOP", "DESKTOP_SESSION", "GDMSESSION"} {
		val := strings.ToLower(getenvFn(envVar))
		switch {
		case strings.Contains(val, "gnome"):
			return DEGNOME
		case strings.Contains(val, "kde") || strings.Contains(val, "plasma"):
			return DEKDE
		case strings.Contains(val, "xfce"):
			return DEXFCE
		case strings.Contains(val, "mate"):
			return DEMATE
		case strings.Contains(val, "lxde"):
			return DELXDE
		case strings.Contains(val, "cinnamon"):
			return DECinnamon
		case strings.Contains(val, "i3"):
			return DEI3
		case strings.Contains(val, "openbox"):
			return DEOpenbox
		}
	}

	// Fall back to checking running processes via pgrep (deterministic order).
	for _, c := range desktopProcessOrder {
		if processRunningFn(c.process) {
			return c.de
		}
	}

	return DEUnknown
}

// detectLoginManager checks systemd service state and known config paths.
func detectLoginManager() LoginManager {
	// Check which display manager systemd points to.
	target, err := readLinkFn("/etc/systemd/system/display-manager.service")
	if err == nil {
		lower := strings.ToLower(target)
		switch {
		case strings.Contains(lower, "gdm"):
			return LMGDM
		case strings.Contains(lower, "lightdm"):
			return LMLightDM
		case strings.Contains(lower, "sddm"):
			return LMSDDM
		case strings.Contains(lower, "slim"):
			return LMSlim
		case filepath.Base(lower) == "ly.service":
			return LMLY
		}
	}

	// Fall back to checking known config file presence (deterministic order).
	for _, c := range loginManagerConfigOrder {
		if fileExistsFn(c.path) {
			return c.lm
		}
	}

	// Last resort: check running processes (deterministic order).
	for _, c := range loginManagerProcessOrder {
		if processRunningFn(c.process) {
			return c.lm
		}
	}

	return LMUnknown
}

// IsDebian returns true if the system is Debian-based.
// Checks /etc/os-release for ID=debian or ID_LIKE containing "debian".
func IsDebian() bool {
	return isDebianFn()
}

var isDebianFn = isDebian

func isDebian() bool {
	data, err := readFileFn("/etc/os-release")
	if err != nil {
		return false
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if val, ok := strings.CutPrefix(line, "ID="); ok {
			val = strings.Trim(val, "\"")
			if val == "debian" {
				return true
			}
		}
		if val, ok := strings.CutPrefix(line, "ID_LIKE="); ok {
			val = strings.Trim(val, "\"")
			if slices.Contains(strings.Fields(val), "debian") {
				return true
			}
		}
	}
	return false
}

// DetectScreenResolution attempts to determine the current screen resolution.
// Returns 0,0 if detection fails.
func DetectScreenResolution() (int, int) {
	// Skip xrandr on Wayland sessions — xrandr under XWayland may return
	// stale or incorrect resolution data without erroring.
	sessionType := getenvFn("XDG_SESSION_TYPE")

	// Try xrandr first, but only on X11 sessions.
	out, err := ([]byte)(nil), error(nil)
	if sessionType != "wayland" {
		out, err = execOutputFn("xrandr", "--query")
	} else {
		err = fmt.Errorf("skipping xrandr on Wayland session")
	}
	if err == nil {
		for line := range strings.Lines(string(out)) {
			if !strings.Contains(line, "*") {
				continue
			}
			// Line format: "   1920x1080     60.00*+  ..."
			fields := strings.Fields(line)
			if len(fields) > 0 {
				parts := strings.SplitN(fields[0], "x", 2)
				if len(parts) == 2 {
					var w, h int
					if _, err := fmt.Sscanf(parts[0], "%d", &w); err == nil {
						if _, err := fmt.Sscanf(parts[1], "%d", &h); err == nil {
							return w, h
						}
					}
				}
			}
		}
	}

	// Fall back to /sys/class/drm/card0-*/modes.
	entries, err := os.ReadDir("/sys/class/drm")
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, "card") || !strings.Contains(name, "-") {
				continue
			}
			modesPath := fmt.Sprintf("/sys/class/drm/%s/modes", name)
			data, err := readFileFn(modesPath)
			if err != nil || len(data) == 0 {
				continue
			}
			// First line is the preferred mode.
			firstLine := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
			parts := strings.SplitN(firstLine, "x", 2)
			if len(parts) == 2 {
				var w, h int
				if _, err := fmt.Sscanf(parts[0], "%d", &w); err == nil {
					if _, err := fmt.Sscanf(parts[1], "%d", &h); err == nil {
						return w, h
					}
				}
			}
		}
	}

	return 0, 0
}

// processRunning returns true if a process with the given name is running.
func processRunning(name string) bool {
	err := exec.Command("pgrep", "-x", name).Run()
	return err == nil
}

// commandExists returns true if a command is available in PATH.
func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// fileExists returns true if path exists (file or directory).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// execOutput runs a command and returns its combined output.
func execOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}
