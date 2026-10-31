# debsplash

debsplash is not part of Debian, not produced by the Debian project, and not endorsed by the Debian project. Debian is a registered trademark owned by Software in the Public Interest, Inc.

A Debian Linux tool to change splash screen images for GRUB, Plymouth (encryption/boot),
and the desktop login screen from a single command. Supports automatic backup and restore.

> **Warning:** This tool modifies critical system files including GRUB bootloader configuration,
> Plymouth initramfs themes, GNOME Shell theme resources, and display manager configs. Incorrect
> changes to these files can result in an unbootable system, a broken login screen, or loss of
> access to an encrypted disk prompt. The tool attempts a backup before each image change,
> always ensure you have a separate recovery method available (e.g. a live USB) before use.
> This tool must be run as root and will install system packages via apt if missing.
> Use at your own risk.

## Supported Login Managers

| Login Manager | Desktop Environments          |
|---------------|-------------------------------|
| GDM3          | GNOME                         |
| LightDM       | XFCE, LXDE, MATE, Cinnamon    |
| SDDM          | KDE Plasma                    |
| SLiM          | Openbox, i3, legacy setups    |

## Requirements

- Debian-based Linux
- Go 1.27.3 or newer (to build from source)
- Git and Make (for the build steps below)

Runtime dependencies (`grub2-common`, `plymouth`, `libglib2.0-dev-bin`, `dconf-cli`, `initramfs-tools`)
are installed via `apt-get` when a required command is missing, unless `--no-auto-install` is set.

## Build & Install

```bash
git clone https://github.com/bradsec/debsplash.git
cd debsplash
make build
sudo make install
```

To uninstall:

```bash
sudo make uninstall
```

## Usage

```bash
# Show detected environment (does not require root)
debsplash status

# Apply one image to ALL splash screens at once
sudo debsplash apply /path/to/image.png

# Change individual targets
sudo debsplash grub    /path/to/image.png   # GRUB bootloader
sudo debsplash encrypt /path/to/image.png   # Plymouth boot/encryption screen
sudo debsplash login   /path/to/image.png   # Desktop login screen

# Backup current settings manually
sudo debsplash backup

# Restore the most recent backup
sudo debsplash restore

# Restore to original system state (fresh install)
sudo debsplash restore-original
```

Ly is detected by `status`, but its login target is unsupported and `apply` skips it.
`--font-size` accepts 1 through 32 for `apply` and `encrypt`.
Use `--dry-run` with a modifying command to inspect planned changes without writing files.

## Backup & Restore

Every `apply`, `grub`, `encrypt`, or `login` command attempts a backup **before**
changing an image. `apply` stops if that backup fails; single-target commands warn and
continue. Backups are stored in:

```
/var/lib/debsplash/backups/<timestamp>/
```

Backups copy existing files in their backup scope. A successful manifest write creates
`manifest.txt`, listing saved files and covered paths; a manifest write failure only warns.

The 10 most recent backups are kept; older ones are pruned automatically.

To restore at any time:

```bash
# Restore the most recent backup
sudo debsplash restore

# Restore to the original system state (before debsplash was first used)
sudo debsplash restore-original
```

Before the first `apply`, `grub`, `encrypt`, or `login` change, debsplash saves existing
system files to `/var/lib/debsplash/original/`. It never overwrites a completed original
snapshot. If that directory exists without a manifest, an apply stops instead of replacing it.
`restore-original` requires the saved original and restores files it contains.

When present, a manifest lists the paths the backup covered. `restore` and `restore-original` remove files
debsplash created (GRUB background, SDDM `theme.conf.user`, `/usr/share/debsplash`) when
the backup shows they did not exist yet. An original saved by an older version has no such list;
for it, `restore-original` removes only files it can identify as debsplash's own and keeps
`/usr/share/debsplash` while a remaining SLiM or SDDM config still points into it.

## Image Requirements

### Format

**PNG is recommended.** Resized output is saved as PNG regardless of input format.
Progressive JPEGs and indexed or interlaced PNGs produce warnings because GRUB cannot use
those formats directly; image processing converts them when applied with a resize mode.

### Resolution

Each target prints its output size and the source before it is applied, for example
`plymouth: 2560x1440 (detected: card1-eDP-1)`, `grub: 1920x1080 (default)` or
`login: 3840x2160 (--resolution)`. Dry runs print the same lines.

| Target            | Size used                                                               |
|-------------------|-------------------------------------------------------------------------|
| GRUB              | 1920x1080, always, because the firmware mode is unknown and large PNGs slow GRUB down |
| Plymouth (boot)   | Native mode of the single connected display, else 1920x1080             |
| Login screen      | Native mode of the single connected display, else 1920x1080             |

Detection reads `/sys/class/drm/card*-*/status` and `modes`. A connector counts only when
`status` is exactly `connected`; its native mode is the first line of `modes`. debsplash
uses it only when exactly one connector is connected and the mode is a plain `WIDTHxHEIGHT`
between 320x240 and 7680x4320. With zero or several connected connectors, or an unparsable
or out-of-range mode, it uses 1920x1080 and prints the reason. `xrandr` is not used for
this, because it depends on the user session and on `DISPLAY`/`XAUTHORITY` under `sudo`.
`status` still prints the current screen resolution; that value does not set output size.

Pass `--resolution WIDTHxHEIGHT` to override detection and the GRUB default for every target, for example
`--resolution 2560x1440` or `--resolution 3840x2160`. The minimum is 320x240 and
the maximum is 7680x4320. Use a source image with the same aspect ratio as the
target, at or above its size, to avoid cropping and upscaling.

Source images are rejected before pixel decoding if they exceed 16384 pixels on either side or
67,108,864 pixels in total (8192x8192), or if the file exceeds 100 MiB. 8K
(7680x4320) sources are accepted. The image is read once and validated, and only that
validated copy is installed; installed images are written with mode 0644. Symlinks in
the image path are resolved first; if any part of the resolved path is a symlink when the
file is opened, the image is rejected. On kernels without `openat2`,
only the final path component gets this check.

### Resize Modes

Use `--resize` to control how the image is adapted to the target resolution:

- **none**: copies the image as-is with no processing
- **fit**: scales to fit within the target dimensions, adding black letterbox bars
- **fill** (default): scales to cover the entire target, center-cropping any overflow
- **crop**: alias of **fill**, kept for existing commands

## Automatic Configuration

- **Boot splash**: If `splash` is not present in `GRUB_CMDLINE_LINUX_DEFAULT`, it is added automatically when applying a GRUB background, so Plymouth boot splash is displayed.
- **Missing packages**: See [Requirements](#requirements) for automatic installation behavior.

## Project Structure

```
debsplash/
├── main.go                      # Entry point
├── cmd/                         # CLI command routing
├── internal/
│   ├── backup/                  # Backup and restore logic
│   ├── config/                  # Paths and runtime config
│   ├── deps/                    # Auto-install missing system packages
│   ├── detect/                  # DE and login manager detection
│   ├── fileutil/                # Atomic file operations
│   ├── grub/                    # GRUB background handler
│   ├── imgutil/                 # Image resize/crop/validate
│   ├── lockfile/                # Concurrency lock for system-wide operations
│   ├── loginmgr/                # Login manager handler (GDM/LightDM/SDDM/SLiM)
│   ├── plymouth/                # Plymouth theme handler
│   └── safepath/                # Path validation and sanitization
├── Makefile
├── CHANGELOG.md
└── README.md
```

## Upgrading from splashchanger

The first modifying command moves `/var/lib/splashchanger` to `/var/lib/debsplash` under both command locks when the new directory does not exist. It keeps the first original snapshot and all prior backups byte for byte. If both directories exist, debsplash uses the new one and warns; it leaves the old one untouched. `--dry-run` reports the move without changing either directory.

Apply replaces old named GRUB, Plymouth, and login artifacts with debsplash names. Restore and restore-original recognize old markers and names. Keep the legacy binary stopped during the upgrade; both versions use `/var/run/splashchanger.lock` during the transition. To roll back after migration, stop debsplash and rename `/var/lib/debsplash` back to `/var/lib/splashchanger` only if the legacy path is absent. Review any new backups before running the old binary.
