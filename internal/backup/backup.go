package backup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/bradsec/debsplash/internal/config"
	"github.com/bradsec/debsplash/internal/detect"
	"github.com/bradsec/debsplash/internal/fileutil"
	"github.com/bradsec/debsplash/internal/grub"
	"github.com/bradsec/debsplash/internal/loginmgr"
)

// allowedCommands is the set of commands that execCommand may run.
var allowedCommands = map[string]bool{
	"update-grub":                true,
	"update-initramfs":           true,
	"plymouth-set-default-theme": true,
	"dconf":                      true,
}

// execCommand is an injectable package-level var for running post-restore commands.
var execCommand = func(name string, args ...string) error {
	if !allowedCommands[name] {
		return fmt.Errorf("command %q is not in the allowed command list", name)
	}
	fmt.Printf("  [exec] Running: %s %s\n", name, strings.Join(args, " "))
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w\n%s", name, err, string(out))
	}
	return nil
}

// filesToBackup lists all files/dirs that may be modified by splashchanger.
var filesToBackup = []string{
	// GRUB
	"/etc/default/grub",
	// Plymouth
	"/etc/plymouth/plymouthd.conf",
	"/usr/share/plymouth/themes/splashchanger",
	// GDM (dconf-based)
	"/etc/dconf/profile/gdm",
	"/etc/dconf/db/gdm.d/00-splashchanger",
	"/usr/share/gdm/dconf/91-splashchanger",
	// GDM (GNOME Shell theme gresource)
	"/usr/share/gnome-shell/gnome-shell-theme.gresource",
	// LightDM
	"/etc/lightdm/lightdm-gtk-greeter.conf",
	"/etc/lightdm/lightdm.conf",
	// SDDM
	"/etc/sddm.conf",
	"/usr/share/sddm/themes/breeze/backgrounds",
}

var plymouthConfPath = "/etc/plymouth/plymouthd.conf"

// legacyBackupScope is the path list every manifest without a "Backup scope:"
// record was written against. It describes existing snapshots, so it must not
// change; add new paths to backupPaths, not to filesToBackup.
var legacyBackupScope = slices.Clone(filesToBackup)

// Paths splashchanger writes outside filesToBackup. They are variables so
// tests can redirect them; the defaults come from the packages that write them.
var (
	grubBackgroundPNG         = grub.BackgroundPNG
	slimConfPath              = loginmgr.SlimConfPath
	stagingDir                = loginmgr.StagingDir
	gresourceBackupPath       = loginmgr.GnomeShellGresource + loginmgr.GresourceBackupSuffix
	gresourceHashPath         = loginmgr.GnomeShellGresource + loginmgr.GresourceHashSuffix
	legacyGresourceBackupPath = loginmgr.GnomeShellGresource + config.LegacyGresourceBackupSuffix
	legacyGresourceHashPath   = loginmgr.GnomeShellGresource + config.LegacyGresourceHashSuffix
	legacyGrubBackgroundPNG   = config.LegacyGrubBackground + ".png"
	legacyStagingDir          = config.LegacyStagingDir
	sddmThemeConfUserFn       = loginmgr.SDDMThemeConfUser
	gdmProfilePath            = "/etc/dconf/profile/gdm"
	// createdFiles are fixed-path files whose names mark them as splashchanger's own.
	createdFiles = []string{
		config.LegacyGDMDconf,
		config.LegacyGDMOverride,
		"/usr/share/plymouth/themes/" + config.LegacyTheme,
		"/etc/dconf/db/gdm.d/00-debsplash",
		"/usr/share/gdm/dconf/91-debsplash",
		"/usr/share/plymouth/themes/debsplash",
	}
)

const scopeHeader = "Backup scope:"

// backupPaths returns every path a backup covers: filesToBackup, the other
// files debsplash writes, and the active SDDM theme.conf.user.
func backupPaths() []string {
	paths := slices.Concat(filesToBackup, []string{grubBackgroundPNG, legacyGrubBackgroundPNG, slimConfPath, stagingDir, legacyStagingDir, gresourceHashPath, legacyGresourceHashPath, legacyGresourceBackupPath, gresourceBackupPath, "/etc/dconf/db/gdm.d/00-debsplash", "/usr/share/gdm/dconf/91-debsplash", "/usr/share/plymouth/themes/debsplash"})
	if conf := sddmThemeConfUserFn(); conf != "" && !slices.Contains(paths, conf) {
		paths = append(paths, conf)
	}
	return paths
}

var originalBackupDir = config.OriginalBackupDir
var stateDir = config.StateDir
var legacyStateDir = config.LegacyStateDir

// MigrateState moves the complete legacy state tree before any backup is read or written.
// The caller holds both the old and new CLI locks.
func MigrateState(dryRun bool) error {
	newInfo, newErr := os.Lstat(stateDir)
	oldInfo, oldErr := os.Lstat(legacyStateDir)
	if newErr != nil && !errors.Is(newErr, os.ErrNotExist) {
		return newErr
	}
	if oldErr != nil && !errors.Is(oldErr, os.ErrNotExist) {
		return oldErr
	}
	if oldErr != nil {
		return nil
	}
	if !oldInfo.IsDir() {
		return fmt.Errorf("legacy state is not a directory: %s", legacyStateDir)
	}
	if newErr == nil {
		if !newInfo.IsDir() {
			return fmt.Errorf("new state is not a directory: %s", stateDir)
		}
		fmt.Printf("  [debsplash] Warning: keeping legacy state at %s; new state already exists\n", legacyStateDir)
		return nil
	}
	if dryRun {
		fmt.Printf("  [debsplash] Would migrate state from %s to %s\n", legacyStateDir, stateDir)
		return nil
	}
	if err := os.Rename(legacyStateDir, stateDir); err != nil {
		return fmt.Errorf("migrate legacy state: %w", err)
	}
	return nil
}

// backupSet represents a backup directory with common save/restore operations.
type backupSet struct {
	dir    string
	label  string // display label: "backup" or "original"
	dryRun bool
	strict bool
}

// saveFiles copies every path in scope into the backup directory. It also
// returns the covered paths: those copied or confirmed absent. A path whose
// stat or copy failed is not covered, so a restore cannot read it as absent.
func (bs *backupSet) saveFiles(scope []string) (int, []string, []string, error) {
	backed := 0
	var backedFiles, covered []string
	var copyErrors []error
	for _, src := range scope {
		info, err := os.Stat(src)
		if errors.Is(err, os.ErrNotExist) {
			covered = append(covered, src)
			continue
		}
		if err != nil {
			if bs.strict {
				return backed, backedFiles, covered, fmt.Errorf("stat %s: %w", src, err)
			}
			fmt.Printf("  [%s] Warning: could not stat %s: %v\n", bs.label, src, err)
			continue
		}

		if bs.dryRun {
			fmt.Printf("  [%s] Would back up %s\n", bs.label, src)
			backed++
			covered = append(covered, src)
			continue
		}

		dest := filepath.Join(bs.dir, src)
		if info.IsDir() {
			if err := fileutil.CopyDir(src, dest); err != nil {
				if bs.strict {
					return backed, backedFiles, covered, fmt.Errorf("copy %s: %w", src, err)
				}
				fmt.Printf("  [%s] Warning: could not back up %s: %v\n", bs.label, src, err)
				copyErrors = append(copyErrors, fmt.Errorf("copy %s: %w", src, err))
			} else {
				backed++
				backedFiles = append(backedFiles, src)
				covered = append(covered, src)
			}
		} else {
			if err := fileutil.CopyFile(src, dest); err != nil {
				if bs.strict {
					return backed, backedFiles, covered, fmt.Errorf("copy %s: %w", src, err)
				}
				fmt.Printf("  [%s] Warning: could not back up %s: %v\n", bs.label, src, err)
				copyErrors = append(copyErrors, fmt.Errorf("copy %s: %w", src, err))
			} else {
				backed++
				backedFiles = append(backedFiles, src)
				covered = append(covered, src)
			}
		}
	}
	return backed, backedFiles, covered, errors.Join(copyErrors...)
}

// restoreFiles walks the backup dir and copies files back to their original locations.
// Returns which types of files were restored for post-restore commands.
func (bs *backupSet) restoreFiles() (grub, plymouth, dconf bool, err error) {
	err = filepath.Walk(bs.dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == bs.dir || path == filepath.Join(bs.dir, "manifest.txt") {
			return nil
		}

		rel, relErr := filepath.Rel(bs.dir, path)
		if relErr != nil {
			return relErr
		}
		dest := filepath.Join("/", rel)

		switch {
		case strings.HasPrefix(dest, "/etc/default/grub") || strings.HasPrefix(dest, "/boot/grub/"):
			grub = true
		case strings.HasPrefix(dest, "/usr/share/plymouth/") || strings.HasPrefix(dest, "/etc/plymouth/"):
			plymouth = true
		case strings.HasPrefix(dest, "/etc/dconf/") || strings.HasPrefix(dest, "/usr/share/gnome-shell/") || strings.HasPrefix(dest, "/usr/share/gdm/dconf/"):
			dconf = true
		}

		if bs.dryRun {
			fmt.Printf("  [%s] Would restore %s to %s\n", bs.label, path, dest)
			return nil
		}

		if info.IsDir() {
			return os.MkdirAll(dest, info.Mode())
		}
		return fileutil.CopyFile(path, dest)
	})
	return
}

var backupNow = time.Now

// TakeBackup creates a timestamped backup of all relevant config files and images.
func TakeBackup(cfg *config.Config, env *detect.Environment) error {
	timestamp := backupNow().Format("2006-01-02_15-04-05.000000000")
	backupDir := filepath.Join(cfg.BackupDir, timestamp)

	if !cfg.DryRun {
		if err := os.MkdirAll(cfg.BackupDir, 0700); err != nil {
			return fmt.Errorf("could not create backup parent directory %s: %w", cfg.BackupDir, err)
		}
		for suffix := 0; ; suffix++ {
			if suffix > 0 {
				backupDir = filepath.Join(cfg.BackupDir, fmt.Sprintf("%s-%d", timestamp, suffix))
			}
			err := os.Mkdir(backupDir, 0700)
			if err == nil {
				break
			}
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("could not create backup directory %s: %w", backupDir, err)
			}
		}
	}

	bs := &backupSet{dir: backupDir, label: "backup", dryRun: cfg.DryRun}
	scope := backupPaths()
	backed, backedFiles, covered, copyErr := bs.saveFiles(scope)

	if !cfg.DryRun {
		manifest := filepath.Join(backupDir, "manifest.txt")
		if err := writeManifest(manifest, env, timestamp, backedFiles, covered); err != nil {
			fmt.Printf("  [backup] Warning: could not write manifest: %v\n", err)
		}
	}

	if cfg.DryRun {
		fmt.Printf("  [backup] Would back up %d item(s) to %s\n", backed, backupDir)
	} else {
		fmt.Printf("  [backup] Backed up %d item(s) to %s\n", backed, backupDir)
	}

	if !cfg.DryRun {
		pruneOldBackups(cfg.BackupDir, 10)
	}

	return copyErr
}

// Restore restores the most recent backup.
func Restore(cfg *config.Config, env *detect.Environment) error {
	latest, err := latestBackup(cfg.BackupDir)
	if err != nil {
		return fmt.Errorf("no backups found in %s: %w", cfg.BackupDir, err)
	}

	if cfg.DryRun {
		fmt.Printf("[debsplash] Would restore from backup: %s\n", latest)
	} else {
		fmt.Printf("[debsplash] Restoring from backup: %s\n", latest)
	}
	checkManifestMismatch(latest, env)

	bs := &backupSet{dir: latest, label: "restore", dryRun: cfg.DryRun}
	restoredGrub, restoredPlymouth, restoredDconf, err := bs.restoreFiles()
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	cleanupSplashchangerFiles(latest, bs.label, cfg.DryRun, false)
	runPostRestoreCommands(cfg.DryRun, restoredGrub, restoredPlymouth, restoredDconf)
	if cfg.DryRun {
		fmt.Println("[debsplash] Dry run complete. No files restored.")
	} else {
		fmt.Println("[debsplash] Restore complete.")
	}
	return nil
}

// runPostRestoreCommands runs system commands to regenerate configs after restore.
func runPostRestoreCommands(dryRun, grub, plymouth, dconf bool) {
	type cmd struct {
		name string
		args []string
	}
	var cmds []cmd
	if grub {
		cmds = append(cmds, cmd{"update-grub", nil})
	}
	if plymouth {
		if theme := readPlymouthTheme(); theme != "" {
			cmds = append(cmds, cmd{"plymouth-set-default-theme", []string{theme}})
		}
		cmds = append(cmds, cmd{"update-initramfs", []string{"-u"}})
	}
	if dconf {
		cmds = append(cmds, cmd{"dconf", []string{"update"}})
	}

	for _, c := range cmds {
		full := c.name
		if len(c.args) > 0 {
			full += " " + strings.Join(c.args, " ")
		}
		if dryRun {
			fmt.Printf("  Would run: %s\n", full)
			continue
		}
		fmt.Printf("  Running: %s\n", full)
		if err := execCommand(c.name, c.args...); err != nil {
			fmt.Printf("  Warning: %v\n", err)
		}
	}
}

// checkManifestMismatch reads the manifest and warns if the environment differs.
func checkManifestMismatch(backupDir string, env *detect.Environment) {
	manifestPath := filepath.Join(backupDir, "manifest.txt")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return // no manifest, nothing to check
	}

	var manifestLoginMgr, manifestDesktop string
	for line := range strings.Lines(string(data)) {
		line = strings.TrimRight(line, "\r\n")
		if after, ok := strings.CutPrefix(line, "LoginMgr:"); ok {
			manifestLoginMgr = strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "Desktop:"); ok {
			manifestDesktop = strings.TrimSpace(after)
		}
	}

	if manifestLoginMgr != "" && manifestLoginMgr != string(env.LoginManager) {
		fmt.Printf("  Warning: backup was taken with %s but current login manager is %s\n",
			manifestLoginMgr, env.LoginManager)
	}
	if manifestDesktop != "" && manifestDesktop != string(env.Desktop) {
		fmt.Printf("  Warning: backup was taken with %s but current desktop is %s\n",
			manifestDesktop, env.Desktop)
	}
}

// latestBackup finds the most recently created backup directory.
func latestBackup(backupDir string) (string, error) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("no backups found")
	}
	// Entries are sorted alphabetically; since names are timestamps, last = newest.
	for _, entry := range slices.Backward(entries) {
		if entry.IsDir() {
			return filepath.Join(backupDir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("no backup directories found")
}

// pruneOldBackups keeps only the `keep` most recent backups.
func pruneOldBackups(backupDir string, keep int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}
	// Collect directory entries (each backup is a dir).
	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		}
	}
	if len(dirs) <= keep {
		return
	}
	// Remove oldest (lowest timestamp = earliest in sort order).
	for _, d := range dirs[:len(dirs)-keep] {
		path := filepath.Join(backupDir, d.Name())
		if err := os.RemoveAll(path); err != nil {
			fmt.Printf("  [backup] Warning: could not prune old backup %s: %v\n", path, err)
		} else {
			fmt.Printf("  [backup] Pruned old backup: %s\n", path)
		}
	}
}

// SaveOriginal saves the factory/original system files on first run only.
// A completed original backup is never changed.
func SaveOriginal(cfg *config.Config, env *detect.Environment) error {
	originalDir := originalBackupDir

	if cfg.DryRun {
		// In dry-run mode, check if it already exists.
		if _, err := os.Stat(originalDir); err == nil {
			return nil
		}
		fmt.Printf("  [original] Would save original system files to %s\n", originalDir)
		return nil
	}

	if info, err := os.Stat(filepath.Join(originalDir, "manifest.txt")); err == nil && info.Mode().IsRegular() {
		return nil
	}
	if info, err := os.Stat(filepath.Join(legacyStateDir, "original", "manifest.txt")); err == nil && info.Mode().IsRegular() {
		return fmt.Errorf("legacy original exists at %s; refusing to create a new original", filepath.Join(legacyStateDir, "original"))
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect legacy original: %w", err)
	}
	if _, err := os.Lstat(originalDir); err == nil {
		return fmt.Errorf("incomplete original backup at %s: manifest missing", originalDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect original backup: %w", err)
	}
	if env == nil {
		return errors.New("cannot save original backup without detected environment")
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(originalDir), 0700); err != nil {
		return fmt.Errorf("could not create parent directory: %w", err)
	}
	entries, err := os.ReadDir(filepath.Dir(originalDir))
	if err != nil {
		return fmt.Errorf("inspect original backup parent: %w", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".original-staging-") && entry.IsDir() {
			if err := os.RemoveAll(filepath.Join(filepath.Dir(originalDir), entry.Name())); err != nil {
				return fmt.Errorf("remove stale original staging directory: %w", err)
			}
		}
	}
	staging, err := os.MkdirTemp(filepath.Dir(originalDir), ".original-staging-")
	if err != nil {
		return fmt.Errorf("create original staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	fmt.Println("  [original] First run detected — saving original system files...")

	bs := &backupSet{dir: staging, label: "original", strict: true}
	scope := backupPaths()
	backed, backedFiles, covered, err := bs.saveFiles(scope)
	if err != nil {
		return err
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	manifest := filepath.Join(staging, "manifest.txt")
	if err := writeManifest(manifest, env, timestamp, backedFiles, covered); err != nil {
		return fmt.Errorf("write original manifest: %w", err)
	}
	if _, err := os.Lstat(originalDir); err == nil {
		return fmt.Errorf("original backup appeared during save: %s", originalDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(staging, originalDir); err != nil {
		return fmt.Errorf("publish original backup: %w", err)
	}

	fmt.Printf("  [original] Saved %d original item(s) to %s\n", backed, originalDir)
	return nil
}

// RestoreOriginal restores the factory/original system files.
func RestoreOriginal(cfg *config.Config, env *detect.Environment) error {
	originalDir := originalBackupDir

	if info, err := os.Stat(filepath.Join(originalDir, "manifest.txt")); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("no original backup found at %s — was debsplash run before?", originalDir)
	}

	if cfg.DryRun {
		fmt.Printf("[debsplash] Would restore original system files from %s\n", originalDir)
	} else {
		fmt.Printf("[debsplash] Restoring original system files from %s\n", originalDir)
	}
	checkManifestMismatch(originalDir, env)

	bs := &backupSet{dir: originalDir, label: "restore-original", dryRun: cfg.DryRun}
	restoredGrub, restoredPlymouth, restoredDconf, err := bs.restoreFiles()
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	cleanupSplashchangerFiles(originalDir, bs.label, cfg.DryRun, true)
	runPostRestoreCommands(cfg.DryRun, restoredGrub, restoredPlymouth, restoredDconf)
	if cfg.DryRun {
		fmt.Println("[debsplash] Dry run complete. No original files restored.")
	} else {
		fmt.Println("[debsplash] Original system files restored.")
	}
	return nil
}

// cleanupSplashchangerFiles removes files debsplash creates when the
// snapshot shows they did not exist when it was taken. A path absent from the
// snapshot counts as absent only if the snapshot's scope covered it. Snapshots
// without a scope record use legacyBackupScope. An original snapshot predates
// every apply, so outside its scope it also removes files identifiable as
// debsplash's own by name or by content.
func cleanupSplashchangerFiles(snapshotDir, label string, dryRun, original bool) {
	scope := readScope(snapshotDir)
	sddmConf := sddmThemeConfUserFn()
	named := slices.Concat(createdFiles, []string{gresourceBackupPath, gresourceHashPath, legacyGresourceBackupPath, legacyGresourceHashPath})
	if matches, err := filepath.Glob(strings.TrimSuffix(grubBackgroundPNG, ".png") + "*"); err == nil {
		named = append(named, matches...)
	}
	if matches, err := filepath.Glob(strings.TrimSuffix(legacyGrubBackgroundPNG, ".png") + "*"); err == nil {
		named = append(named, matches...)
	}
	candidates := slices.Concat([]string{gdmProfilePath}, named)
	if sddmConf != "" {
		candidates = append(candidates, sddmConf)
	}
	// The staging dir goes last: its reference check must see the configs
	// left after the other removals.
	named = append(named, stagingDir, legacyStagingDir)
	candidates = append(candidates, stagingDir, legacyStagingDir)

	for _, path := range candidates {
		if _, err := os.Lstat(filepath.Join(snapshotDir, path)); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		owned := slices.Contains(named, path) || path == sddmConf && isSplashchangerSDDMConf(path)
		if !scope[path] && !owned {
			continue
		}
		if (path == stagingDir || path == legacyStagingDir) && stagingDirReferenced(sddmConf, path) {
			fmt.Printf("  [%s] Keeping %s: still referenced by %s or %s\n", label, path, slimConfPath, sddmConf)
			continue
		}
		if dryRun {
			fmt.Printf("  [%s] Would remove debsplash file: %s\n", label, path)
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			fmt.Printf("  [%s] Warning: could not remove %s: %v\n", label, path, err)
		} else {
			fmt.Printf("  [%s] Removed: %s\n", label, path)
		}
	}
}

// readScope returns the set of paths the snapshot's backup covered.
func readScope(snapshotDir string) map[string]bool {
	scope := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(snapshotDir, "manifest.txt"))
	if err == nil {
		_, section, found := strings.Cut(string(data), "\n"+scopeHeader+"\n")
		if found {
			for line := range strings.Lines(section) {
				path, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "  ")
				if !ok {
					break
				}
				scope[path] = true
			}
			return scope
		}
	}
	for _, path := range legacyBackupScope {
		scope[path] = true
	}
	// Legacy snapshots predate the checksum sidecar. A gresource restored from
	// one must not keep the checksum of another installed version.
	scope[gresourceHashPath] = true
	return scope
}

// isSplashchangerSDDMConf reports whether path holds exactly the
// theme.conf.user that debsplash writes.
func isSplashchangerSDDMConf(path string) bool {
	data, err := os.ReadFile(path)
	return err == nil && loginmgr.IsSplashchangerSDDMThemeConf(data)
}

// stagingDirReferenced reports whether a kept login-manager config still
// points into stagingDir, so removing it would break the login screen.
func stagingDirReferenced(sddmConf, dir string) bool {
	for _, path := range []string{slimConfPath, sddmConf} {
		if path == "" {
			continue
		}
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), dir+"/") {
			return true
		}
	}
	return false
}

// readPlymouthTheme reads the current Plymouth theme name from plymouthd.conf.
func readPlymouthTheme() string {
	data, err := os.ReadFile(plymouthConfPath)
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Theme="); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// writeManifest writes a human-readable summary of the backup.
func writeManifest(path string, env *detect.Environment, timestamp string, backedFiles, scope []string) error {
	var content strings.Builder
	fmt.Fprintf(&content, "debsplash backup\n")
	fmt.Fprintf(&content, "Timestamp:   %s\n", timestamp)
	fmt.Fprintf(&content, "Desktop:     %s\n", env.Desktop)
	fmt.Fprintf(&content, "LoginMgr:    %s\n", env.LoginManager)
	fmt.Fprintf(&content, "Plymouth:    %v\n", env.HasPlymouth)
	fmt.Fprintf(&content, "GRUB:        %v\n", env.HasGrub)
	if len(backedFiles) > 0 {
		fmt.Fprintf(&content, "\nFiles backed up:\n")
		for _, file := range backedFiles {
			fmt.Fprintf(&content, "  %s\n", file)
		}
	}
	fmt.Fprintf(&content, "\n%s\n", scopeHeader)
	for _, file := range scope {
		fmt.Fprintf(&content, "  %s\n", file)
	}
	return os.WriteFile(path, []byte(content.String()), 0600)
}
