package loginmgr

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/deps"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/bradsec/debsplash/internal/fileutil"
)

const destDir = "/usr/share/debsplash"

// Paths written by this package, exported so backup can include them in its scope.
const (
	StagingDir            = destDir
	SlimConfPath          = "/etc/slim.conf"
	GnomeShellGresource   = "/usr/share/gnome-shell/gnome-shell-theme.gresource"
	GresourceBackupSuffix = ".debsplash-backup"
	GresourceHashSuffix   = ".debsplash-sha256"
)

// Apply routes to the correct login manager background handler.
func Apply(imgPath string, lm detect.LoginManager) error {
	// Copy image to a shared location we control.
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("could not create image staging dir: %w", err)
	}
	ext := filepath.Ext(imgPath)
	stagedPath := filepath.Join(destDir, "login-background"+ext)
	if err := fileutil.CopyFile(imgPath, stagedPath); err != nil {
		return fmt.Errorf("could not stage login image: %w", err)
	}

	var err error
	switch lm {
	case detect.LMGDM:
		err = applyGDM(stagedPath)
	case detect.LMLightDM:
		err = applyLightDM(stagedPath)
	case detect.LMSDDM:
		err = applySDDM(stagedPath)
	case detect.LMSlim:
		err = applySlim(stagedPath)
	default:
		return fmt.Errorf("unsupported or undetected login manager: %s — please set the background manually", lm)
	}
	if err != nil {
		return err
	}
	return os.RemoveAll(config.LegacyStagingDir)
}

// escapeDconfURI escapes a file path for use in a dconf picture-uri value.
// Ensures single quotes within the path don't break the INI format.
func escapeDconfURI(path string) string {
	// dconf values are wrapped in single quotes; escape any embedded single quotes
	return strings.ReplaceAll(path, "'", "%27")
}

// gdmSchemaFile is the path to the GNOME desktop background schema (injectable for testing).
var gdmSchemaFile = "/usr/share/glib-2.0/schemas/org.gnome.desktop.background.gschema.xml"

// gdmSupportsDarkURI checks if the installed GNOME schema supports picture-uri-dark.
// Returns false on Debian 11 (GNOME 3.38) where the key doesn't exist.
func gdmSupportsDarkURI() bool {
	data, err := os.ReadFile(gdmSchemaFile)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "picture-uri-dark")
}

// applyGDM sets the GDM3 background via dconf and GNOME Shell theme CSS.
func applyGDM(imgPath string) error {
	// Apply via GNOME Shell theme gresource (controls the actual login screen background
	// on GNOME 43+). This is the primary mechanism on modern Debian/GNOME.
	fmt.Println("  [loginmgr/gdm3] Patching GNOME Shell theme for login background...")
	if err := applyGDMShellTheme(imgPath); err != nil {
		fmt.Printf("  [loginmgr/gdm3] Warning: could not patch GNOME Shell theme: %v\n", err)
		fmt.Println("  [loginmgr/gdm3] Falling back to dconf-only approach (may not affect login screen on GNOME 43+)")
	}

	// Also apply via dconf (sets screensaver background and works on older GNOME).
	fmt.Println("  [loginmgr/gdm3] Applying background via dconf...")

	// Build the dconf content with background URIs and logo hiding.
	escaped := escapeDconfURI(imgPath)
	var dbContent string
	// Hide the distro logo on the login screen.
	logoSection := "\n[org/gnome/login-screen]\nlogo=''\n"

	if gdmSupportsDarkURI() {
		dbContent = fmt.Sprintf("[org/gnome/desktop/background]\npicture-uri='file://%s'\npicture-uri-dark='file://%s'\n\n"+
			"[org/gnome/desktop/screensaver]\npicture-uri='file://%s'\npicture-uri-dark='file://%s'\n",
			escaped, escaped, escaped, escaped)
	} else {
		dbContent = fmt.Sprintf("[org/gnome/desktop/background]\npicture-uri='file://%s'\n\n"+
			"[org/gnome/desktop/screensaver]\npicture-uri='file://%s'\n",
			escaped, escaped)
	}
	dbContent += logoSection

	// Debian/Ubuntu GDM uses its own dconf directory at /usr/share/gdm/dconf/
	// with priority files like 90-debian-settings. Write our override with
	// priority 91 so it takes precedence over the distro logo setting.
	gdmDconfDir := "/usr/share/gdm/dconf"
	if _, err := os.Stat(gdmDconfDir); err == nil {
		gdmOverride := filepath.Join(gdmDconfDir, "91-debsplash")
		if err := fileutil.WriteFileAtomic(gdmOverride, []byte(dbContent), 0644); err != nil {
			fmt.Printf("  [loginmgr/gdm3] Warning: could not write GDM dconf override: %v\n", err)
		} else if err := os.Remove(config.LegacyGDMOverride); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	// Also write to the traditional /etc/dconf/db/gdm.d/ location as a
	// fallback for non-Debian GDM setups that use this path.
	profileDir := "/etc/dconf/profile"
	dbDir := "/etc/dconf/db/gdm.d"

	if err := os.MkdirAll(profileDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return err
	}

	// Ensure gdm dconf profile exists.
	profilePath := filepath.Join(profileDir, "gdm")
	if err := writeGDMProfile(profilePath, "/usr/share/dconf/profile/gdm"); err != nil {
		return fmt.Errorf("could not write gdm dconf profile: %w", err)
	}

	dbFile := filepath.Join(dbDir, "00-debsplash")
	if err := fileutil.WriteFileAtomic(dbFile, []byte(dbContent), 0644); err != nil {
		return fmt.Errorf("could not write gdm dconf db file: %w", err)
	}
	if err := os.Remove(config.LegacyGDMDconf); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	// Compile the dconf database.
	if err := deps.EnsureCommand("dconf"); err != nil {
		return err
	}
	out, err := exec.Command("dconf", "update").CombinedOutput()
	if err != nil {
		return fmt.Errorf("dconf update failed: %w\n%s", err, string(out))
	}

	fmt.Println("  [loginmgr/gdm3] Done. Restart GDM to see changes: sudo systemctl restart gdm3")
	return nil
}

func writeGDMProfile(profilePath, packagedProfilePath string) error {
	content, err := os.ReadFile(profilePath)
	if errors.Is(err, os.ErrNotExist) {
		content, err = os.ReadFile(packagedProfilePath)
		if errors.Is(err, os.ErrNotExist) {
			content = []byte("user-db:user\n")
			err = nil
		}
	}
	if err != nil {
		return err
	}
	for line := range strings.SplitSeq(string(content), "\n") {
		if strings.TrimSpace(line) == "system-db:gdm" {
			return fileutil.WriteFileAtomic(profilePath, content, 0644)
		}
	}
	if len(content) > 0 && content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	content = append(content, "system-db:gdm\n"...)
	return fileutil.WriteFileAtomic(profilePath, content, 0644)
}

// GNOME Shell theme gresource paths and markers.
const (
	gnomeShellGresourcePrefix = "/org/gnome/shell/theme/"
	cssMarkerStart            = "/* debsplash-start */"
	cssMarkerEnd              = "/* debsplash-end */"
)

// Injectable seams for GDM shell theme testing.
var (
	gnomeShellGresourcePath = GnomeShellGresource
	execCommandFn           = exec.Command
	gdmTempDir              = "/var/tmp"
)

// applyGDMShellTheme modifies the GNOME Shell theme CSS to set a custom login
// background. On GNOME 43+, the GDM login screen background is controlled by
// the shell theme CSS (compiled into a gresource), not by dconf settings.
func applyGDMShellTheme(imgPath string) error {
	// Ensure required tools are available (auto-installs if missing).
	for _, tool := range []string{"gresource", "glib-compile-resources"} {
		if err := deps.EnsureCommand(tool); err != nil {
			return err
		}
	}

	if _, err := os.Stat(gnomeShellGresourcePath); err != nil {
		return fmt.Errorf("GNOME Shell theme not found at %s", gnomeShellGresourcePath)
	}

	backupPath := gnomeShellGresourcePath + GresourceBackupSuffix
	hashPath := gnomeShellGresourcePath + GresourceHashSuffix
	legacyBackup := gnomeShellGresourcePath + config.LegacyGresourceBackupSuffix
	legacyHash := gnomeShellGresourcePath + config.LegacyGresourceHashSuffix
	if _, err := os.Stat(backupPath); errors.Is(err, os.ErrNotExist) {
		if _, err := os.Stat(legacyBackup); err == nil {
			if err := fileutil.CopyFile(legacyBackup, backupPath); err != nil {
				return err
			}
			if hash, err := os.ReadFile(legacyHash); err == nil {
				if err := fileutil.WriteFileAtomic(hashPath, hash, 0644); err != nil {
					return err
				}
			}
		}
	}
	installedHash, err := gresourceHash(gnomeShellGresourcePath)
	if err != nil {
		return fmt.Errorf("could not hash installed gresource: %w", err)
	}
	previousHash, err := os.ReadFile(hashPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not read gresource checksum: %w", err)
	}
	_, backupErr := os.Stat(backupPath)
	if backupErr != nil && !errors.Is(backupErr, os.ErrNotExist) {
		return fmt.Errorf("could not stat gresource backup: %w", backupErr)
	}
	if errors.Is(backupErr, os.ErrNotExist) || len(previousHash) != 0 && string(previousHash) != installedHash {
		fmt.Println("  [loginmgr/gdm3] Backing up original GNOME Shell theme...")
		if err := fileutil.CopyFile(gnomeShellGresourcePath, backupPath); err != nil {
			return fmt.Errorf("could not back up gresource: %w", err)
		}
	}

	// Always extract from the backup to avoid compounding CSS changes.
	sourcePath := backupPath

	tmpDir, err := os.MkdirTemp(gdmTempDir, "debsplash-gdm-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	// List all resources in the gresource file.
	out, err := execCommandFn("gresource", "list", sourcePath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("gresource list failed: %w\n%s", err, out)
	}

	resources := strings.Split(strings.TrimSpace(string(out)), "\n")
	themeDir := filepath.Join(tmpDir, "theme")
	var xmlFiles []string

	// Extract each resource file.
	for _, res := range resources {
		if res == "" {
			continue
		}

		data, err := execCommandFn("gresource", "extract", sourcePath, res).Output()
		if err != nil {
			return fmt.Errorf("failed to extract %s: %w", res, err)
		}

		relPath := strings.TrimPrefix(res, gnomeShellGresourcePrefix)
		localPath := filepath.Join(themeDir, relPath)
		if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(localPath, data, 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", localPath, err)
		}
		xmlFiles = append(xmlFiles, relPath)
	}

	// Inject our background CSS into all shell CSS variant files.
	// GNOME 42 and earlier: gnome-shell.css
	// GNOME 43-46: gnome-shell.css + gnome-shell-dark.css
	// GNOME 47+: gnome-shell-light.css + gnome-shell-dark.css + gnome-shell-high-contrast.css
	for _, cssName := range []string{"gnome-shell.css", "gnome-shell-dark.css", "gnome-shell-light.css", "gnome-shell-high-contrast.css"} {
		cssPath := filepath.Join(themeDir, cssName)
		content, err := os.ReadFile(cssPath)
		if err != nil {
			continue // file may not exist (e.g., no dark variant)
		}
		newContent := injectBackgroundCSS(string(content), imgPath)
		if err := os.WriteFile(cssPath, []byte(newContent), 0644); err != nil {
			return fmt.Errorf("failed to write modified %s: %w", cssName, err)
		}
	}

	// Generate the gresource XML manifest.
	xmlContent := generateGresourceXML(xmlFiles)
	xmlPath := filepath.Join(tmpDir, "gnome-shell-theme.gresource.xml")
	if err := os.WriteFile(xmlPath, []byte(xmlContent), 0644); err != nil {
		return err
	}

	// Compile the modified theme.
	outputPath := filepath.Join(tmpDir, "gnome-shell-theme.gresource")
	cmd := execCommandFn("glib-compile-resources", "--sourcedir", themeDir, "--target", outputPath, xmlPath)
	if compileOut, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("glib-compile-resources failed: %w\n%s", err, compileOut)
	}

	// Replace the system gresource with our modified version.
	if err := fileutil.CopyFile(outputPath, gnomeShellGresourcePath); err != nil {
		return fmt.Errorf("failed to install modified gresource: %w", err)
	}
	installedHash, err = gresourceHash(gnomeShellGresourcePath)
	if err != nil {
		return fmt.Errorf("could not hash patched gresource: %w", err)
	}
	if err := fileutil.WriteFileAtomic(hashPath, []byte(installedHash), 0644); err != nil {
		return fmt.Errorf("could not record gresource checksum: %w", err)
	}
	for _, path := range []string{legacyBackup, legacyHash} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	fmt.Println("  [loginmgr/gdm3] GNOME Shell theme patched successfully.")
	return nil
}

func gresourceHash(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// injectBackgroundCSS adds or replaces the debsplash background rule in GNOME Shell CSS.
func injectBackgroundCSS(content, imgPath string) string {
	bgRule := fmt.Sprintf("%s\n#lockDialogGroup {\n  background: url('file://%s');\n  background-size: cover;\n  background-position: center;\n}\n.login-dialog-logo-bin {\n  display: none;\n}\n%s",
		cssMarkerStart, imgPath, cssMarkerEnd)

	// Remove any existing debsplash block.
	for _, markers := range [][2]string{{cssMarkerStart, cssMarkerEnd}, {config.LegacyCSSStart, config.LegacyCSSEnd}} {
		if start := strings.Index(content, markers[0]); start >= 0 {
			if end := strings.Index(content[start:], markers[1]); end >= 0 {
				content = content[:start] + content[start+end+len(markers[1]):]
			}
		}
	}

	return strings.TrimRight(content, " \t\n") + "\n\n" + bgRule + "\n"
}

// generateGresourceXML creates a gresource XML manifest for the extracted theme files.
func generateGresourceXML(files []string) string {
	var sb strings.Builder
	sb.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	sb.WriteString("<gresources>\n")
	sb.WriteString("  <gresource prefix=\"/org/gnome/shell/theme\">\n")
	for _, f := range files {
		fmt.Fprintf(&sb, "    <file>%s</file>\n", html.EscapeString(f))
	}
	sb.WriteString("  </gresource>\n")
	sb.WriteString("</gresources>\n")
	return sb.String()
}

// applyLightDM writes to the lightdm-gtk-greeter config.
func applyLightDM(imgPath string) error {
	fmt.Println("  [loginmgr/lightdm] Applying background...")

	configPath := "/etc/lightdm/lightdm-gtk-greeter.conf"

	content, err := os.ReadFile(configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not read LightDM greeter config: %w", err)
	}

	newContent := setIniValue(string(content), "greeter", "background", imgPath)

	if err := fileutil.WriteFileAtomic(configPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("could not write LightDM greeter config: %w", err)
	}

	fmt.Println("  [loginmgr/lightdm] Done. Restart LightDM to see changes: sudo systemctl restart lightdm")
	return nil
}

// Injectable seams for SDDM testing.
var (
	readFileFn        = os.ReadFile
	writeFileAtomicFn = fileutil.WriteFileAtomic
	mkdirAllFn        = os.MkdirAll
	globFn            = filepath.Glob
)

// sddmFileExists returns true if path exists (used in SDDM theme lookup).
func sddmFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var sddmFileExistsFn = sddmFileExists

// parseSDDMThemeName extracts the Current= value from the [Theme] section
// of sddm.conf content. Returns (themeName, fromConfig) where fromConfig
// indicates whether the theme was explicitly set in the config file.
// Defaults to "debian-breeze" if not found.
func parseSDDMThemeName(content string) (string, bool) {
	lines := strings.Split(content, "\n")
	inTheme := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "[Theme]" {
			inTheme = true
			continue
		}
		if len(trimmed) > 0 && trimmed[0] == '[' {
			inTheme = false
			continue
		}
		if inTheme && strings.HasPrefix(trimmed, "Current") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 {
				val := strings.TrimSpace(parts[1])
				if val != "" {
					return val, true
				}
			}
		}
	}
	return "debian-breeze", false
}

// scanSDDMDropInDirs reads /etc/sddm.conf.d/*.conf for [Theme] overrides.
// Files are read in alphabetical order; later files override earlier ones.
func scanSDDMDropInDirs() (string, bool) {
	matches, err := globFn("/etc/sddm.conf.d/*.conf")
	if err != nil || len(matches) == 0 {
		return "", false
	}
	slices.Sort(matches)

	var lastTheme string
	found := false
	for _, path := range matches {
		data, err := readFileFn(path)
		if err != nil {
			continue
		}
		if name, ok := parseSDDMThemeName(string(data)); ok {
			lastTheme = name
			found = true
		}
	}
	return lastTheme, found
}

// escapeSDDMValue ensures a path doesn't contain characters that break
// INI-format SDDM config files.
func escapeSDDMValue(path string) string {
	// SDDM INI format: newlines or section headers would break parsing
	path = strings.ReplaceAll(path, "\n", "")
	path = strings.ReplaceAll(path, "\r", "")
	return path
}

// resolveSDDMThemeDir returns the theme directory applySDDM writes to, read
// from /etc/sddm.conf and drop-in dirs. found is false when no candidate
// directory exists; themeDir is then the primary location applySDDM creates.
func resolveSDDMThemeDir() (themeDir, themeName string, fromConfig, found bool, err error) {
	content, err := readFileFn("/etc/sddm.conf")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", false, false, fmt.Errorf("could not read sddm.conf: %w", err)
	}

	themeName, fromConfig = parseSDDMThemeName(string(content))

	// Check drop-in directories for overrides (Debian 13+ / SDDM 0.20+).
	if dropInTheme, ok := scanSDDMDropInDirs(); ok {
		themeName = dropInTheme
		fromConfig = true
	}

	// Search for the theme directory, checking both debian-breeze and breeze.
	searchNames := []string{themeName}
	if themeName == "breeze" {
		searchNames = append(searchNames, "debian-breeze")
	} else if themeName == "debian-breeze" {
		searchNames = append(searchNames, "breeze")
	}

	for _, name := range searchNames {
		primary := filepath.Join("/usr/share/sddm/themes", name)
		fallback := filepath.Join("/usr/local/share/sddm/themes", name)

		if sddmFileExistsFn(primary) {
			return primary, themeName, fromConfig, true, nil
		}
		if sddmFileExistsFn(fallback) {
			return fallback, themeName, fromConfig, true, nil
		}
	}
	return filepath.Join("/usr/share/sddm/themes", themeName), themeName, fromConfig, false, nil
}

// SDDMThemeConfUser returns the theme.conf.user path applySDDM would write for
// the active SDDM theme, or "" when the SDDM config cannot be read. It creates
// nothing.
func SDDMThemeConfUser() string {
	themeDir, _, _, _, err := resolveSDDMThemeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(themeDir, "theme.conf.user")
}

// sddmThemeConfContent is the complete theme.conf.user that applySDDM writes.
func sddmThemeConfContent(imgPath string) string {
	return fmt.Sprintf("[General]\nbackground=%s\n", escapeSDDMValue(imgPath))
}

// IsSplashchangerSDDMThemeConf reports whether data is exactly a
// theme.conf.user written by applySDDM for an image staged by Apply.
func IsSplashchangerSDDMThemeConf(data []byte) bool {
	img, ok := strings.CutPrefix(string(data), "[General]\nbackground=")
	if !ok {
		return false
	}
	img, ok = strings.CutSuffix(img, "\n")
	return ok && !strings.ContainsAny(img, "\r\n") && (filepath.Dir(img) == destDir || filepath.Dir(img) == config.LegacyStagingDir) &&
		string(data) == sddmThemeConfContent(img)
}

// applySDDM writes a SDDM theme config pointing to the background image.
// It reads the active theme from /etc/sddm.conf and drop-in dirs, and writes
// theme.conf.user in the corresponding theme directory.
func applySDDM(imgPath string) error {
	fmt.Println("  [loginmgr/sddm] Applying background...")

	themeDir, themeName, fromConfig, found, err := resolveSDDMThemeDir()
	if err != nil {
		return err
	}

	if !fromConfig {
		fmt.Printf("  [loginmgr/sddm] No theme configured in sddm.conf; assuming %q\n", themeName)
	}

	if !found {
		// Create primary location if nothing found.
		fmt.Printf("  [loginmgr/sddm] Warning: no SDDM theme directory found for %q — creating %s\n", themeName, themeDir)
		if err := mkdirAllFn(themeDir, 0755); err != nil {
			return fmt.Errorf("could not create theme dir %s: %w", themeDir, err)
		}
	}

	themeConfUser := filepath.Join(themeDir, "theme.conf.user")
	if err := writeFileAtomicFn(themeConfUser, []byte(sddmThemeConfContent(imgPath)), 0644); err != nil {
		return fmt.Errorf("could not write theme.conf.user: %w", err)
	}

	fmt.Println("  [loginmgr/sddm] Done. Restart SDDM to see changes: sudo systemctl restart sddm")
	return nil
}

// applySlim writes the background to the SLiM theme config.
func applySlim(imgPath string) error {
	fmt.Println("  [loginmgr/slim] Applying background...")

	content, err := os.ReadFile(SlimConfPath)
	if err != nil {
		return fmt.Errorf("could not read /etc/slim.conf: %w", err)
	}

	newContent := setKeyValue(string(content), "background", imgPath)
	if err := fileutil.WriteFileAtomic(SlimConfPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("could not write slim.conf: %w", err)
	}

	fmt.Println("  [loginmgr/slim] Done. Restart SLiM to see changes: sudo systemctl restart slim")
	return nil
}

// setIniValue sets key=value under [section] in an INI-style string.
// Matches exact key names (not prefixes) to avoid clobbering similar keys.
func setIniValue(content, section, key, value string) string {
	lines := strings.Split(content, "\n")
	sectionHeader := "[" + section + "]"
	inSection := false
	found := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == sectionHeader {
			inSection = true
			continue
		}
		if len(trimmed) > 0 && trimmed[0] == '[' {
			inSection = false
		}
		if inSection && matchesKey(trimmed, key, "=") {
			lines[i] = key + " = " + value
			found = true
			inSection = false
		}
	}

	if !found {
		lines = append(lines, "", sectionHeader, key+" = "+value)
	}

	return strings.Join(lines, "\n")
}

// setKeyValue sets a space-separated key value in a flat config (like slim.conf).
func setKeyValue(content, key, value string) string {
	lines := strings.Split(content, "\n")
	found := false
	for i, line := range lines {
		if matchesKey(strings.TrimSpace(line), key, " ") {
			lines[i] = key + " " + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, key+" "+value)
	}
	return strings.Join(lines, "\n")
}

// matchesKey checks if a line starts with the exact key followed by the separator
// (or whitespace then the separator). This prevents "background" from matching
// "background_color".
func matchesKey(line, key, sep string) bool {
	if !strings.HasPrefix(line, key) {
		return false
	}
	rest := line[len(key):]
	if len(rest) == 0 {
		return true // line is exactly the key (no value yet)
	}
	// The character after the key must be whitespace or the separator.
	first := rest[0]
	return first == ' ' || first == '\t' || string(first) == sep
}
