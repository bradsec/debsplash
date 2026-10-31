package loginmgr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteGDMProfilePreservesExisting(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "etc", "gdm")
	if err := os.MkdirAll(filepath.Dir(profile), 0755); err != nil {
		t.Fatal(err)
	}
	original := "user-db:user\nfile-db:/var/lib/gdm3/greeter-dconf-defaults\n"
	if err := os.WriteFile(profile, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	want := original + "system-db:gdm\n"
	for range 2 {
		if err := writeGDMProfile(profile, filepath.Join(dir, "unused")); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(profile)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("GDM profile = %q, want %q", got, want)
		}
	}
}

func TestWriteGDMProfileUsesPackagedProfile(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "etc", "gdm")
	packaged := filepath.Join(dir, "usr", "gdm")
	for _, path := range []string{profile, packaged} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	original := "user-db:user\nfile-db:/var/lib/gdm3/greeter-dconf-defaults\n"
	if err := os.WriteFile(packaged, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeGDMProfile(profile, packaged); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	want := original + "system-db:gdm\n"
	if string(got) != want {
		t.Fatalf("GDM profile = %q, want %q", got, want)
	}
}
