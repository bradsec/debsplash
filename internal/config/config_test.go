package config

import "testing"

func TestValidateHexColor(t *testing.T) {
	valid := []string{"#FFF", "#ff8800", "#000000", "#abc", "#ABCDEF"}
	for _, s := range valid {
		if err := ValidateHexColor(s); err != nil {
			t.Errorf("ValidateHexColor(%q) = %v, want nil", s, err)
		}
	}

	invalid := []string{"red", "#GGG", "000000", "#12345", "", "#", "#1", "#12", "#1234", "#12345G"}
	for _, s := range invalid {
		if err := ValidateHexColor(s); err == nil {
			t.Errorf("ValidateHexColor(%q) = nil, want error", s)
		}
	}
}

func TestParseTargets(t *testing.T) {
	tests := []struct {
		input   string
		want    []Target
		wantErr bool
	}{
		{"", nil, false},
		{"grub", []Target{TargetGrub}, false},
		{"grub,plymouth", []Target{TargetGrub, TargetPlymouth}, false},
		{"grub,plymouth,login", []Target{TargetGrub, TargetPlymouth, TargetLogin}, false},
		{"GRUB, Plymouth", []Target{TargetGrub, TargetPlymouth}, false},
		{"invalid", nil, true},
		{"grub,invalid", nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseTargets(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTargets(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("ParseTargets(%q) = %v, want %v", tt.input, got, tt.want)
					return
				}
				for i := range got {
					if got[i] != tt.want[i] {
						t.Errorf("ParseTargets(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
					}
				}
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.BackupDir != BackupBaseDir {
		t.Errorf("BackupDir = %q, want %q", cfg.BackupDir, BackupBaseDir)
	}
	if cfg.DryRun != false {
		t.Error("DryRun should default to false")
	}
	if cfg.EncryptScreen.HAlign != 0.5 {
		t.Errorf("HAlign = %v, want 0.5", cfg.EncryptScreen.HAlign)
	}
	if cfg.EncryptScreen.VAlign != 0.7 {
		t.Errorf("VAlign = %v, want 0.7", cfg.EncryptScreen.VAlign)
	}
	if cfg.EncryptScreen.BoxColor != "#000000" {
		t.Errorf("BoxColor = %q, want #000000", cfg.EncryptScreen.BoxColor)
	}
	if cfg.EncryptScreen.BoxOpacity != 0.7 {
		t.Errorf("BoxOpacity = %v, want 0.7", cfg.EncryptScreen.BoxOpacity)
	}
	if cfg.EncryptScreen.TextColor != "#FFFFFF" {
		t.Errorf("TextColor = %q, want #FFFFFF", cfg.EncryptScreen.TextColor)
	}
	if cfg.EncryptScreen.FontSize != 14 {
		t.Errorf("FontSize = %d, want 14", cfg.EncryptScreen.FontSize)
	}
	if cfg.EncryptScreen.Style != "boxed" {
		t.Errorf("Style = %q, want boxed", cfg.EncryptScreen.Style)
	}
}
