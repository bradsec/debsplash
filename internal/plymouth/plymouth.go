package plymouth

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/deps"
	"github.com/bradsec/debsplash/internal/fileutil"
)

const (
	debsplashTheme   = "debsplash"
	plymouthThemeDir = "/usr/share/plymouth/themes"
)

// Apply installs the image as a custom Plymouth theme with password prompt
// support and activates it. The EncryptScreenConfig controls the appearance
// and position of the password entry dialog.
func Apply(imgPath string, esc config.EncryptScreenConfig) error {
	themeDir := filepath.Join(plymouthThemeDir, debsplashTheme)

	fmt.Printf("  [plymouth] Creating theme directory: %s\n", themeDir)
	if err := mkdirAll(themeDir, 0755); err != nil {
		return fmt.Errorf("could not create theme dir: %w", err)
	}

	// Copy the image into the theme directory.
	ext := strings.ToLower(filepath.Ext(imgPath))
	destImage := filepath.Join(themeDir, "background"+ext)
	fmt.Printf("  [plymouth] Copying image to %s\n", destImage)
	if err := fileutil.CopyFile(imgPath, destImage); err != nil {
		return fmt.Errorf("could not copy image: %w", err)
	}

	// Write the .plymouth theme descriptor with dialog alignment settings.
	themePath := filepath.Join(themeDir, debsplashTheme+".plymouth")
	scriptPath := filepath.Join(themeDir, debsplashTheme+".script")
	fmt.Printf("  [plymouth] Writing theme descriptor: %s\n", themePath)
	if err := writeThemeDescriptor(themePath, themeDir, scriptPath, esc); err != nil {
		return fmt.Errorf("could not write theme descriptor: %w", err)
	}

	// Generate box image for styles that use a background panel.
	if esc.Style == "floating" || esc.Style == "boxed" {
		boxImgPath := filepath.Join(themeDir, "background_box.png")
		if err := generateBoxImage(boxImgPath, esc); err != nil {
			return fmt.Errorf("could not generate box image: %w", err)
		}
	}

	// Generate bullet dot image for password entry.
	bulletImgPath := filepath.Join(themeDir, "bullet.png")
	if err := generateBulletImage(bulletImgPath, esc); err != nil {
		return fmt.Errorf("could not generate bullet image: %w", err)
	}

	// Write the theme script with password prompt handling.
	fmt.Printf("  [plymouth] Writing theme script: %s\n", scriptPath)
	if err := writeThemeScript(scriptPath, "background"+ext, esc); err != nil {
		return fmt.Errorf("could not write theme script: %w", err)
	}

	// Activate the theme.
	if err := deps.EnsureCommand("plymouth-set-default-theme"); err != nil {
		return err
	}
	fmt.Printf("  [plymouth] Setting default theme to '%s'...\n", debsplashTheme)
	out, err := exec.Command("plymouth-set-default-theme", debsplashTheme).CombinedOutput()
	if err != nil {
		return fmt.Errorf("plymouth-set-default-theme failed: %w\n%s", err, string(out))
	}
	if err := os.RemoveAll(filepath.Join(plymouthThemeDir, config.LegacyTheme)); err != nil {
		return err
	}

	// Rebuild the initramfs so the theme is included in the boot image.
	if err := deps.EnsureCommand("update-initramfs"); err != nil {
		return err
	}
	fmt.Println("  [plymouth] Rebuilding initramfs (this may take a moment)...")
	out, err = exec.Command("update-initramfs", "-u").CombinedOutput()
	if err != nil {
		return fmt.Errorf("update-initramfs failed: %w\n%s", err, string(out))
	}

	fmt.Println("  [plymouth] Done.")
	return nil
}

// writeThemeDescriptor creates the .plymouth INI-style descriptor file
// with dialog alignment settings from the encrypt screen config.
func writeThemeDescriptor(path, imageDir, scriptPath string, esc config.EncryptScreenConfig) error {
	content := fmt.Sprintf(`[Plymouth Theme]
Name=Debsplash
Description=Custom splash screen managed by debsplash
ModuleName=script

[script]
ImageDir=%s
ScriptFile=%s
DialogHorizontalAlignment=%g
DialogVerticalAlignment=%g
`, imageDir, scriptPath, esc.HAlign, esc.VAlign)

	return fileutil.WriteFileAtomic(path, []byte(content), 0644)
}

// escapePlymouthString escapes a string for use in Plymouth script string literals.
func escapePlymouthString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}

// writeThemeScript creates a Plymouth script that displays the background image
// and handles the password entry dialog for encrypted disk prompts.
func writeThemeScript(path, imageFilename string, esc config.EncryptScreenConfig) error {
	imageFilename = escapePlymouthString(imageFilename)
	// Parse hex colors to 0.0-1.0 RGB floats for Plymouth script.
	boxR, boxG, boxB, err := hexToFloats(esc.BoxColor)
	if err != nil {
		return err
	}
	txtR, txtG, txtB, err := hexToFloats(esc.TextColor)
	if err != nil {
		return err
	}

	// Select box drawing parameters based on style.
	var boxDrawing string
	switch esc.Style {
	case "minimal":
		boxDrawing = `  # Minimal style: just a subtle line under the text
  line_img = Image.Text("________________________________________", ` +
			fmt.Sprintf("%g, %g, %g", txtR, txtG, txtB) + `);
  line_sprite = Sprite(line_img);
  line_sprite.SetPosition(box_x, box_y + box_h - 4, 10001);
`
	case "floating":
		boxDrawing = `  # Floating style: semi-transparent box behind password prompt
  bg_box = Image("background_box.png");
  bg_box_scaled = bg_box.Scale(box_w, box_h);
  box_sprite.SetImage(bg_box_scaled);
  box_sprite.SetPosition(box_x, box_y, 10000);
`
	default: // "boxed"
		boxDrawing = `  # Boxed style: solid background rectangle behind the prompt
  bg_box = Image("background_box.png");
  bg_box_scaled = bg_box.Scale(box_w, box_h);
  box_sprite.SetImage(bg_box_scaled);
  box_sprite.SetPosition(box_x, box_y, 10000);
`
	}

	content := fmt.Sprintf(`# debsplash Plymouth theme script
# Generated by debsplash — do not edit manually

# --- Background ---
wallpaper_image = Image("%s");
screen_width = Window.GetWidth();
screen_height = Window.GetHeight();
resized_wallpaper = wallpaper_image.Scale(screen_width, screen_height);
wallpaper_sprite = Sprite(resized_wallpaper);
wallpaper_sprite.SetZ(-100);

# --- Password dialog configuration ---
dialog_halign = %g;
dialog_valign = %g;
box_opacity = %g;
box_r = %g;
box_g = %g;
box_b = %g;
text_r = %g;
text_g = %g;
text_b = %g;
font_size = %d;

# --- Password prompt state ---
prompt_text = "";
bullet_count = 0;
label_sprite = Sprite();
hint_sprite = Sprite();
box_sprite = Sprite();
bullet_image = Image("bullet.png");
bullet_dot_w = bullet_image.GetWidth();
bullet_spacing = Math.Int(bullet_dot_w * 1.2);
max_bullets = 32;

# Pre-create bullet sprites (hidden off-screen).
for (i = 0; i < max_bullets; i++) {
  bullet_sprites[i] = Sprite(bullet_image);
  bullet_sprites[i].SetPosition(-100, -100, 10002);
  bullet_sprites[i].SetOpacity(0);
}

# --- Box dimensions ---
box_w = Math.Int(screen_width * 0.45);
box_h = Math.Int(font_size * 7);
box_x = Math.Int(screen_width * dialog_halign - box_w / 2);
box_y = Math.Int(screen_height * dialog_valign - box_h / 2);

fun draw_password_box() {
%s
  box_sprite.SetPosition(box_x, box_y, 10000);
}

# --- Display password callback ---
fun display_password_callback(prompt_string, num_bullets) {
  prompt_text = prompt_string;
  bullet_count = num_bullets;

  draw_password_box();

  # Draw prompt label
  label_image = Image.Text(prompt_text, text_r, text_g, text_b);
  label_sprite.SetImage(label_image);
  label_x = box_x + Math.Int((box_w - label_image.GetWidth()) / 2);
  label_y = box_y + Math.Int(font_size * 0.8);
  label_sprite.SetPosition(label_x, label_y, 10002);

  # Position bullet dot images for entered password
  # Clamp to box width: only show as many bullets as fit inside the box
  box_padding = Math.Int(bullet_spacing);
  max_visible = Math.Int((box_w - box_padding * 2) / bullet_spacing);
  if (max_visible < 1) max_visible = 1;
  visible_bullets = num_bullets;
  if (visible_bullets > max_visible) visible_bullets = max_visible;

  total_w = (visible_bullets - 1) * bullet_spacing + bullet_dot_w;
  start_x = box_x + Math.Int((box_w - total_w) / 2);
  bullet_y = box_y + Math.Int(font_size * 4);

  for (i = 0; i < max_bullets; i++) {
    if (i < visible_bullets) {
      bullet_sprites[i].SetPosition(start_x + i * bullet_spacing, bullet_y, 10002);
      bullet_sprites[i].SetOpacity(1);
    } else {
      bullet_sprites[i].SetPosition(-100, -100, 10002);
      bullet_sprites[i].SetOpacity(0);
    }
  }

  # Show hint text when no characters entered
  if (num_bullets == 0) {
    hint_image = Image.Text("[ Enter passphrase ]", text_r * 0.6, text_g * 0.6, text_b * 0.6);
    hint_sprite.SetImage(hint_image);
    hint_x = box_x + Math.Int((box_w - hint_image.GetWidth()) / 2);
    hint_sprite.SetPosition(hint_x, bullet_y, 10002);
  } else {
    hint_sprite.SetPosition(-100, -100, 10002);
  }
}

Plymouth.SetDisplayPasswordFunction(display_password_callback);

# --- Normal display message callback ---
message_sprite = Sprite();

fun display_message_callback(text) {
  if (text == "") {
    message_sprite.SetPosition(-100, -100, 0);
    return;
  }
  msg_image = Image.Text(text, text_r, text_g, text_b);
  message_sprite.SetImage(msg_image);
  msg_x = Math.Int(screen_width / 2 - msg_image.GetWidth() / 2);
  msg_y = Math.Int(screen_height * 0.9);
  message_sprite.SetPosition(msg_x, msg_y, 10000);
}

Plymouth.SetMessageFunction(display_message_callback);

# --- Display normal callback (hides password when switching) ---
fun display_normal_callback() {
  label_sprite.SetPosition(-100, -100, 0);
  hint_sprite.SetPosition(-100, -100, 0);
  box_sprite.SetPosition(-100, -100, 0);
  for (i = 0; i < max_bullets; i++) {
    bullet_sprites[i].SetPosition(-100, -100, 0);
    bullet_sprites[i].SetOpacity(0);
  }
}

Plymouth.SetDisplayNormalFunction(display_normal_callback);
`,
		imageFilename,
		esc.HAlign, esc.VAlign, esc.BoxOpacity,
		boxR, boxG, boxB,
		txtR, txtG, txtB,
		esc.FontSize,
		boxDrawing,
	)

	return fileutil.WriteFileAtomic(path, []byte(content), 0644)
}

// hexToFloats converts a hex color string like "#FF8800" to 0.0-1.0 RGB floats.
func hexToFloats(hex string) (r, g, b float64, err error) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		hex = string(hex[0]) + string(hex[0]) + string(hex[1]) + string(hex[1]) + string(hex[2]) + string(hex[2])
	}
	if len(hex) != 6 {
		return 0, 0, 0, fmt.Errorf("invalid hex color %q: must be 3 or 6 hex digits", hex)
	}
	r = float64(hexByte(hex[0:2])) / 255.0
	g = float64(hexByte(hex[2:4])) / 255.0
	b = float64(hexByte(hex[4:6])) / 255.0
	return r, g, b, nil
}

func hexByte(s string) int {
	val := 0
	for _, c := range s {
		val *= 16
		switch {
		case c >= '0' && c <= '9':
			val += int(c - '0')
		case c >= 'a' && c <= 'f':
			val += int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			val += int(c-'A') + 10
		}
	}
	return val
}

// generateBoxImage creates a solid-color PNG with semi-transparent background
// for the floating password prompt style.
func generateBoxImage(path string, esc config.EncryptScreenConfig) error {
	r, g, b, err := hexToFloats(esc.BoxColor)
	if err != nil {
		return err
	}
	a := uint8(esc.BoxOpacity * 255)
	fillColor := color.NRGBA{
		R: uint8(r * 255),
		G: uint8(g * 255),
		B: uint8(b * 255),
		A: a,
	}

	img := image.NewNRGBA(image.Rect(0, 0, 400, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{fillColor}, image.Point{}, draw.Src)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("could not encode PNG: %w", err)
	}

	return fileutil.WriteFileAtomic(path, buf.Bytes(), 0644)
}

// generateBulletImage creates a small filled-circle PNG for password bullet dots.
// Uses the text color from the encrypt screen config so bullets match the theme.
func generateBulletImage(path string, esc config.EncryptScreenConfig) error {
	r, g, b, err := hexToFloats(esc.TextColor)
	if err != nil {
		return err
	}
	fillColor := color.NRGBA{
		R: uint8(r * 255),
		G: uint8(g * 255),
		B: uint8(b * 255),
		A: 255,
	}

	// Create a small image and draw a filled circle.
	const size = 10
	const radius = 4
	const cx, cy = size / 2, size / 2

	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			dx := float64(x - cx)
			dy := float64(y - cy)
			if dx*dx+dy*dy <= float64(radius*radius) {
				img.Set(x, y, fillColor)
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("could not encode bullet PNG: %w", err)
	}

	return fileutil.WriteFileAtomic(path, buf.Bytes(), 0644)
}

// mkdirAll wraps os.MkdirAll (kept as a seam for testing).
var mkdirAll = mkdirAllReal

func mkdirAllReal(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
