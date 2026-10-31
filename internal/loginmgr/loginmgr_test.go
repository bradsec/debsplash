package loginmgr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetIniValue_ExistingKey(t *testing.T) {
	input := "[greeter]\nbackground = /old/path.png\n"
	result := setIniValue(input, "greeter", "background", "/new/path.png")
	if !strings.Contains(result, "background = /new/path.png") {
		t.Errorf("expected updated value, got:\n%s", result)
	}
}

func TestSetIniValue_NewKey(t *testing.T) {
	input := "[greeter]\nother = value\n"
	result := setIniValue(input, "greeter", "background", "/new/path.png")
	if !strings.Contains(result, "[greeter]") {
		t.Error("missing existing section")
	}
	if !strings.Contains(result, "background = /new/path.png") {
		t.Errorf("expected new key, got:\n%s", result)
	}
}

func TestSetIniValue_NewSection(t *testing.T) {
	input := ""
	result := setIniValue(input, "Theme", "Current", "breeze")
	if !strings.Contains(result, "[Theme]") {
		t.Error("missing new section header")
	}
	if !strings.Contains(result, "Current = breeze") {
		t.Errorf("expected new key/value, got:\n%s", result)
	}
}

func TestSetIniValue_DoesNotClobberSimilarKeys(t *testing.T) {
	input := "[greeter]\nbackground_color = #000000\nbackground = /old.png\n"
	result := setIniValue(input, "greeter", "background", "/new.png")
	if !strings.Contains(result, "background_color = #000000") {
		t.Error("background_color was clobbered")
	}
	if !strings.Contains(result, "background = /new.png") {
		t.Error("background was not updated")
	}
}

func TestMatchesKey_Exact(t *testing.T) {
	tests := []struct {
		line, key, sep string
		want           bool
	}{
		{"background = /path", "background", "=", true},
		{"background=/path", "background", "=", true},
		{"background_color = #000", "background", "=", false},
		{"background\t= /path", "background", "=", true},
		{"background", "background", "=", true},
		{"other = val", "background", "=", false},
	}
	for _, tt := range tests {
		got := matchesKey(tt.line, tt.key, tt.sep)
		if got != tt.want {
			t.Errorf("matchesKey(%q, %q, %q) = %v, want %v", tt.line, tt.key, tt.sep, got, tt.want)
		}
	}
}

func TestGDMDconfContent_IncludesPictureURIDark(t *testing.T) {
	imgPath := "/usr/share/splashchanger/login-background.png"
	dbContent := fmt.Sprintf("[org/gnome/desktop/background]\npicture-uri='file://%s'\npicture-uri-dark='file://%s'\n\n"+
		"[org/gnome/desktop/screensaver]\npicture-uri='file://%s'\npicture-uri-dark='file://%s'\n", imgPath, imgPath, imgPath, imgPath)

	if !strings.Contains(dbContent, "picture-uri-dark='file://"+imgPath+"'") {
		t.Error("missing picture-uri-dark in background section")
	}

	count := strings.Count(dbContent, "picture-uri-dark=")
	if count != 2 {
		t.Errorf("expected 2 picture-uri-dark entries, got %d", count)
	}
}

func TestParseSDDMThemeName_Default(t *testing.T) {
	result, fromConfig := parseSDDMThemeName("")
	if result != "debian-breeze" {
		t.Errorf("expected debian-breeze, got %q", result)
	}
	if fromConfig {
		t.Error("expected fromConfig=false for empty content")
	}
}

func TestParseSDDMThemeName_WithTheme(t *testing.T) {
	content := "[General]\nsomething=value\n\n[Theme]\nCurrent=nordic\n\n[X11]\nstuff=other\n"
	result, fromConfig := parseSDDMThemeName(content)
	if result != "nordic" {
		t.Errorf("expected nordic, got %q", result)
	}
	if !fromConfig {
		t.Error("expected fromConfig=true for configured theme")
	}
}

func TestParseSDDMThemeName_EmptyCurrent(t *testing.T) {
	content := "[Theme]\nCurrent=\n"
	result, fromConfig := parseSDDMThemeName(content)
	if result != "debian-breeze" {
		t.Errorf("expected debian-breeze for empty Current, got %q", result)
	}
	if fromConfig {
		t.Error("expected fromConfig=false for empty Current value")
	}
}

func TestParseSDDMThemeName_WithSpaces(t *testing.T) {
	content := "[Theme]\nCurrent = sugar-candy \n"
	result, fromConfig := parseSDDMThemeName(content)
	if result != "sugar-candy" {
		t.Errorf("expected sugar-candy, got %q", result)
	}
	if !fromConfig {
		t.Error("expected fromConfig=true")
	}
}

func TestParseSDDMThemeName_NoThemeSection(t *testing.T) {
	content := "[General]\nstuff=val\n[X11]\nother=thing\n"
	result, fromConfig := parseSDDMThemeName(content)
	if result != "debian-breeze" {
		t.Errorf("expected debian-breeze, got %q", result)
	}
	if fromConfig {
		t.Error("expected fromConfig=false for missing Theme section")
	}
}

func TestApplySDDM_FallbackToBreeze(t *testing.T) {
	origReadFile := readFileFn
	origWriteFile := writeFileAtomicFn
	origMkdirAll := mkdirAllFn
	origFileExists := sddmFileExistsFn
	origGlob := globFn
	defer func() {
		readFileFn = origReadFile
		writeFileAtomicFn = origWriteFile
		mkdirAllFn = origMkdirAll
		sddmFileExistsFn = origFileExists
		globFn = origGlob
	}()

	var writtenPath string
	var writtenContent string

	readFileFn = func(name string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	writeFileAtomicFn = func(path string, data []byte, perm os.FileMode) error {
		writtenPath = path
		writtenContent = string(data)
		return nil
	}
	mkdirAllFn = func(path string, perm os.FileMode) error {
		return nil
	}
	// Only "breeze" exists, not "debian-breeze" — should fall back to breeze.
	sddmFileExistsFn = func(path string) bool {
		return path == "/usr/share/sddm/themes/breeze"
	}
	globFn = func(pattern string) ([]string, error) {
		return nil, nil
	}

	err := applySDDM("/test/bg.png")
	if err != nil {
		t.Fatalf("applySDDM failed: %v", err)
	}

	if writtenPath != "/usr/share/sddm/themes/breeze/theme.conf.user" {
		t.Errorf("expected breeze theme.conf.user, got %q", writtenPath)
	}
	if !strings.Contains(writtenContent, "background=/test/bg.png") {
		t.Errorf("expected background in content, got %q", writtenContent)
	}
}

func TestScanSDDMDropInDirs(t *testing.T) {
	origGlob := globFn
	origReadFile := readFileFn
	defer func() {
		globFn = origGlob
		readFileFn = origReadFile
	}()

	globFn = func(pattern string) ([]string, error) {
		return []string{"/etc/sddm.conf.d/10-theme.conf", "/etc/sddm.conf.d/20-override.conf"}, nil
	}
	readFileFn = func(name string) ([]byte, error) {
		switch name {
		case "/etc/sddm.conf.d/10-theme.conf":
			return []byte("[Theme]\nCurrent=breeze\n"), nil
		case "/etc/sddm.conf.d/20-override.conf":
			return []byte("[Theme]\nCurrent=nordic\n"), nil
		}
		return nil, os.ErrNotExist
	}

	theme, ok := scanSDDMDropInDirs()
	if !ok {
		t.Error("expected drop-in theme to be found")
	}
	if theme != "nordic" {
		t.Errorf("expected nordic (last file wins), got %q", theme)
	}
}

func TestGenerateGresourceXML_EscapesSpecialChars(t *testing.T) {
	files := []string{"normal.css", "file<with>&special.css"}
	result := generateGresourceXML(files)
	if strings.Contains(result, "<with>") {
		t.Error("XML special characters were not escaped")
	}
	if !strings.Contains(result, "file&lt;with&gt;&amp;special.css") {
		t.Errorf("expected escaped characters, got:\n%s", result)
	}
	if !strings.Contains(result, "<file>normal.css</file>") {
		t.Error("normal filename should be unchanged")
	}
}

func TestEscapeDconfURI(t *testing.T) {
	if got := escapeDconfURI("/path/with'quote"); got != "/path/with%27quote" {
		t.Errorf("expected escaped quote, got %q", got)
	}
	if got := escapeDconfURI("/normal/path.png"); got != "/normal/path.png" {
		t.Errorf("expected unchanged path, got %q", got)
	}
}

func TestApplySDDM_WarnsWhenNoThemeDir(t *testing.T) {
	origReadFile := readFileFn
	origWriteFile := writeFileAtomicFn
	origMkdirAll := mkdirAllFn
	origFileExists := sddmFileExistsFn
	origGlob := globFn
	defer func() {
		readFileFn = origReadFile
		writeFileAtomicFn = origWriteFile
		mkdirAllFn = origMkdirAll
		sddmFileExistsFn = origFileExists
		globFn = origGlob
	}()

	readFileFn = func(name string) ([]byte, error) {
		if name == "/etc/sddm.conf" {
			return []byte("[Theme]\nCurrent=mytheme\n"), nil
		}
		return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
	}
	globFn = func(pattern string) ([]string, error) {
		return nil, nil
	}
	writeFileAtomicFn = func(path string, data []byte, perm os.FileMode) error {
		return nil
	}
	var createdDir string
	mkdirAllFn = func(path string, perm os.FileMode) error {
		createdDir = path
		return nil
	}
	sddmFileExistsFn = func(path string) bool {
		return false // neither primary nor fallback exists
	}

	err := applySDDM("/test/bg.png")
	if err != nil {
		t.Fatalf("applySDDM failed: %v", err)
	}

	if createdDir != "/usr/share/sddm/themes/mytheme" {
		t.Errorf("expected created dir for mytheme, got %q", createdDir)
	}
}

func TestSDDMThemeConfUserResolvesWithoutCreating(t *testing.T) {
	origRead, origMkdir, origExists, origGlob := readFileFn, mkdirAllFn, sddmFileExistsFn, globFn
	t.Cleanup(func() { readFileFn, mkdirAllFn, sddmFileExistsFn, globFn = origRead, origMkdir, origExists, origGlob })
	readFileFn = func(string) ([]byte, error) { return []byte("[Theme]\nCurrent=mytheme\n"), nil }
	mkdirAllFn = func(path string, _ os.FileMode) error {
		t.Errorf("resolver created %s", path)
		return nil
	}
	sddmFileExistsFn = func(path string) bool { return path == "/usr/local/share/sddm/themes/mytheme" }
	globFn = func(string) ([]string, error) { return nil, nil }

	if got := SDDMThemeConfUser(); got != "/usr/local/share/sddm/themes/mytheme/theme.conf.user" {
		t.Errorf("SDDMThemeConfUser() = %q", got)
	}
}

func TestIsSplashchangerSDDMThemeConf(t *testing.T) {
	staged := filepath.Join(StagingDir, "login-background.png")
	for content, want := range map[string]bool{
		sddmThemeConfContent(staged):                         true,
		"[General]\nbackground=/home/admin/wall.png\n":       false,
		sddmThemeConfContent(staged) + "font=Noto\n":         false,
		"# admin\n" + sddmThemeConfContent(staged):           false,
		"[General]\nbackground=" + StagingDir + "/a/b.png\n": false,
	} {
		if got := IsSplashchangerSDDMThemeConf([]byte(content)); got != want {
			t.Errorf("IsSplashchangerSDDMThemeConf(%q) = %v, want %v", content, got, want)
		}
	}
}

func TestInjectBackgroundCSSReplacesLegacyBlock(t *testing.T) {
	old := "prefix\n/* splashchanger-start */\nold rule\n/* splashchanger-end */\n"
	got := injectBackgroundCSS(old, "/tmp/new.png")
	if strings.Contains(got, "old rule") || strings.Contains(got, "splashchanger-start") || strings.Count(got, "debsplash-start") != 1 {
		t.Fatalf("legacy CSS block remains: %q", got)
	}
}
