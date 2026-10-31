# Changelog

## 0.1.0

First public versioned release of debsplash. The earlier `v1.0.0` tag is being withdrawn.

- Rename the project and CLI from splashchanger to debsplash. Existing state moves to `/var/lib/debsplash` on the first modifying command when that directory is absent. Legacy backups and original snapshots remain available.
- Add dry runs that report planned changes without writing files.
- Reject oversized image sources and use a validated copy throughout each apply. Resolve image symlinks before opening; reject paths that change to symlinks during opening.
- Protect the first original snapshot from replacement. Stop an apply if an incomplete original snapshot exists.
- Restore saved files and remove debsplash-created files absent from the backup scope. Preserve unrelated GRUB settings and GDM profile entries when applying changes.
- Keep both transition lock paths stable across commands.
- Publish a statically linked Linux amd64 binary (`CGO_ENABLED=0`) with `SHA256SUMS` after module verification, vet, and tests.
- Use the native mode of the single connected display for Plymouth and login images when `--resolution` is not given; GRUB stays at 1920x1080. Each target prints its size and source, and `--resolution` overrides all targets.
