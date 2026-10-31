package grub

import (
	"reflect"
	"testing"
)

func TestRemoveGrubKeyLines_PreservesUserSettings(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		key   string
		want  []string
	}{
		{"serial console", []string{`GRUB_TERMINAL="serial console"`}, grubTerminalKey, []string{`GRUB_TERMINAL="serial console"`}},
		{"user background", []string{`GRUB_BACKGROUND="/custom/bg.png"`}, grubBgKey, []string{`GRUB_BACKGROUND="/custom/bg.png"`}},
		{"legacy gfxterm", []string{`GRUB_TERMINAL="gfxterm"`}, grubTerminalKey, []string{}},
		{"legacy background", []string{`GRUB_BACKGROUND="/boot/grub/splashchanger-background.png"`}, grubBgKey, []string{}},
		{"marked terminal", []string{`# debsplash: GRUB_TERMINAL`, `GRUB_TERMINAL="gfxterm"`}, grubTerminalKey, []string{}},
		{"comment", []string{`# GRUB_TERMINAL="serial console"`, `GRUB_TERMINAL="gfxterm"`}, grubTerminalKey, []string{`# GRUB_TERMINAL="serial console"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeGrubKeyLines(tt.lines, tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetOrReplaceKey_SecondRun(t *testing.T) {
	first := setOrReplaceKey([]string{`# GRUB_GFXMODE="old"`}, grubGfxmodeKey, grubGfxmode)
	if first == nil {
		t.Fatal("first run made no change")
	}
	if second := setOrReplaceKey(first, grubGfxmodeKey, grubGfxmode); second != nil {
		t.Fatalf("second run changed lines: %q", second)
	}
}

func TestSetOrReplaceKey_PreservesUserSettingsAndComments(t *testing.T) {
	tests := []struct {
		name        string
		lines, want []string
	}{
		{"user mode", []string{`GRUB_GFXMODE="1024x768"`}, nil},
		{"comment", []string{`# GRUB_GFXMODE="1024x768"`}, []string{`# GRUB_GFXMODE="1024x768"`, `# debsplash: GRUB_GFXMODE`, `GRUB_GFXMODE="auto"`}},
		{"owned mode", []string{`# debsplash: GRUB_GFXMODE`, `GRUB_GFXMODE="old"`}, []string{`# debsplash: GRUB_GFXMODE`, `GRUB_GFXMODE="auto"`}},
		{"idempotent", []string{`# debsplash: GRUB_GFXMODE`, `GRUB_GFXMODE="auto"`}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := setOrReplaceKey(tt.lines, grubGfxmodeKey, grubGfxmode)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureSplashInLines_AlreadyPresent(t *testing.T) {
	lines := []string{
		`GRUB_CMDLINE_LINUX_DEFAULT="quiet splash"`,
	}
	result, err := ensureSplashInLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil (no change needed) when splash already present")
	}
}

func TestEnsureSplashInLines_AddsSplash(t *testing.T) {
	lines := []string{
		"GRUB_TIMEOUT=5",
		`GRUB_CMDLINE_LINUX_DEFAULT="quiet"`,
		"GRUB_DEFAULT=0",
	}
	result, err := ensureSplashInLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected updated lines, got nil")
	}
	if result[1] != `GRUB_CMDLINE_LINUX_DEFAULT="quiet splash"` {
		t.Errorf("expected splash added, got %q", result[1])
	}
	// Other lines should be unchanged.
	if result[0] != "GRUB_TIMEOUT=5" || result[2] != "GRUB_DEFAULT=0" {
		t.Errorf("other lines were modified: %v", result)
	}
}

func TestEnsureSplashInLines_EmptyValue(t *testing.T) {
	lines := []string{
		`GRUB_CMDLINE_LINUX_DEFAULT=""`,
	}
	result, err := ensureSplashInLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected updated lines, got nil")
	}
	if result[0] != `GRUB_CMDLINE_LINUX_DEFAULT="splash"` {
		t.Errorf("expected splash added to empty value, got %q", result[0])
	}
}

func TestEnsureSplashInLines_NoLine(t *testing.T) {
	lines := []string{
		"GRUB_TIMEOUT=5",
	}
	result, err := ensureSplashInLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected updated lines, got nil")
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(result))
	}
	if result[1] != `GRUB_CMDLINE_LINUX_DEFAULT="splash"` {
		t.Errorf("expected appended line, got %q", result[1])
	}
}

func TestEnsureSplashInLines_SplashOnly(t *testing.T) {
	lines := []string{
		`GRUB_CMDLINE_LINUX_DEFAULT="splash"`,
	}
	result, err := ensureSplashInLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("expected nil (no change needed) when splash is the only value")
	}
}

func TestEnsureSplashInLines_DoesNotMatchSubstring(t *testing.T) {
	// "nosplash" should not count as "splash"
	lines := []string{
		`GRUB_CMDLINE_LINUX_DEFAULT="quiet nosplash"`,
	}
	result, err := ensureSplashInLines(lines)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil {
		t.Fatal("expected updated lines — 'nosplash' is not 'splash'")
	}
	if result[0] != `GRUB_CMDLINE_LINUX_DEFAULT="quiet nosplash splash"` {
		t.Errorf("expected splash appended, got %q", result[0])
	}
}

func TestEnsureSplashInLines_ReportedForms(t *testing.T) {
	tests := []struct {
		name, input, want string
	}{
		{"prefixed key", `GRUB_CMDLINE_LINUX_DEFAULT_EXTRA="x"`, `GRUB_CMDLINE_LINUX_DEFAULT_EXTRA="x"`},
		{"trailing comment", `GRUB_CMDLINE_LINUX_DEFAULT="quiet" # note`, `GRUB_CMDLINE_LINUX_DEFAULT="quiet splash" # note`},
		{"single quotes", `GRUB_CMDLINE_LINUX_DEFAULT='quiet'`, `GRUB_CMDLINE_LINUX_DEFAULT='quiet splash'`},
		{"unquoted", `GRUB_CMDLINE_LINUX_DEFAULT=quiet`, `GRUB_CMDLINE_LINUX_DEFAULT="quiet splash"`},
		{"hash inside quotes", `GRUB_CMDLINE_LINUX_DEFAULT="quiet#tag"`, `GRUB_CMDLINE_LINUX_DEFAULT="quiet#tag splash"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ensureSplashInLines([]string{tt.input})
			if err != nil {
				t.Fatal(err)
			}
			if tt.name == "prefixed key" {
				if len(got) != 2 || got[0] != tt.want {
					t.Fatalf("got %q, want original key preserved and target appended", got)
				}
				return
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureSplashInLines_RejectsUnsupportedValues(t *testing.T) {
	for _, input := range []string{
		`GRUB_CMDLINE_LINUX_DEFAULT="$other"`,
		`GRUB_CMDLINE_LINUX_DEFAULT='quiet`,
		`GRUB_CMDLINE_LINUX_DEFAULT="quiet" extra`,
		`GRUB_CMDLINE_LINUX_DEFAULT=quiet splash`,
	} {
		t.Run(input, func(t *testing.T) {
			if got, err := ensureSplashInLines([]string{input}); err == nil || got != nil {
				t.Fatalf("got %q, %v; want error without changes", got, err)
			}
		})
	}
}

func TestLegacyGrubMarkerMigrates(t *testing.T) {
	lines := []string{"# splashchanger: GRUB_GFXMODE", `GRUB_GFXMODE="old"`}
	got := setOrReplaceKey(lines, grubGfxmodeKey, grubGfxmode)
	if len(got) != 2 || got[0] != "# debsplash: GRUB_GFXMODE" || got[1] != `GRUB_GFXMODE="auto"` {
		t.Fatalf("legacy marker not migrated: %q", got)
	}
	if got := removeGrubKeyLines([]string{"# splashchanger: GRUB_TERMINAL", `GRUB_TERMINAL="gfxterm"`}, grubTerminalKey); len(got) != 0 {
		t.Fatalf("legacy marker not removed: %q", got)
	}
}
