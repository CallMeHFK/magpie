# Changelog

All notable changes to the fnOS packaging in this fork. Upstream magpie
changes are not duplicated here; each `fnos-vX.Y.Z` release names the
upstream version it packages (see AGENTS/fnos release protocol).

## [0.1.3075-1] - 2026-11-06

### Changed

- Aligned the package with the fnOS developer guide
  (https://developer.fnnas.com/docs): the manifest now declares
  `service_port=3425` and hides the authorization-directory setting
  (`disable_authorization_path=true`) - magpie needs no user-chosen file or
  directory. `checkport` stays `false` on purpose: the first install pulls
  a multi-arch image, so a port probe would refuse to start an app that has
  nothing to serve yet.
- The desktop entry is administrator-only: `allUsers=false` with
  `control.accessPerm=readonly`, the pair the app-entry guide recommends for
  an admin console.
- `/config` (magpie's settings, accounts, gateway keys and the model
  catalog) is the `magpie-config` named volume, not the image's anonymous
  one. A named volume keeps its name across a stop/start and an upgrade, so
  the data comes back; an anonymous one is left behind and a new empty one
  is mounted next time.
- `FNOS.md`: documents the one published port (3425, the model gateway) and
  the rules table; the data-location and admin-gate notes were wrong.

### Fixed

- The web key cookie went to every signed-in fnOS user, and the entry was
  visible to all of them. The console changes every account, gateway
  key and route on the device, so the bridge now injects the cookie only
  when the gateway also reports `X-Trim-Isadmin: true` (the header the
  unified gateway forwards for every session). A non-administrator still
  reaches magpie's own sign-in page and never the key.
- `<target>/magpie.env` was written 0644 - readable by every user of
  every app on the device. The bridge now writes it 0600 and hands it to
  the magpie container's uid (65532, `BRIDGE_KEY_UID`/`BRIDGE_KEY_GID`),
  and tightens a file an older package left world readable.
- `cmd/common.sh` looked for `<target>/app`, which the fnOS layout never
  creates (the package's `app/` is installed as the target root). Every
  `upgrade_callback` exited 1 before reaching its image pull; it now finds
  `<target>/docker`.
- `upgrade_callback` was a second writer of the key, at a path that does
  not exist (`<target>/app/docker/magpie.env`), as the package user with
  0600 - a file the containers could not read anyway. Removed; the bridge
  is the only writer.
- `ICON.PNG` was a 512x512 square; the package icon is now the required
  64x64 with a rounded body and transparent corners, and `ICON_256.PNG` and
  the entry icons are regenerated from the same artwork.

### Not run

- `fnpack build`, a real device install, and the unified-gateway path on
  hardware: no fnOS device or Docker in this checkout. See
  `fnos/PUBLISHING.md` for the checklist that covers them.

## [0.1.3075] - 2026-11-06

### Changed

- Track upstream [yetone/magpie v0.1.3075](https://github.com/yetone/magpie/releases/tag/v0.1.3075),
  51 commits past v0.1.3049. The merge was clean: the fork never modifies
  upstream code, so nothing under `internal/` needed a resolution, and
  `fnos/UPSTREAM_VERSION`, both compose image tags and the FPK version now
  pin 0.1.3075.
- One-off: the tracked channel stays `stable-7`. v0.1.3075 is inside the
  soak window, so the scheduled sync would not have taken it; this catch-up
  is deliberate and later syncs keep their own gate.

## [0.1.2989] - 2026-11-05

### Changed

- Track upstream [yetone/magpie v0.1.2989](https://github.com/yetone/magpie/releases/tag/v0.1.2989),
  9 commits past v0.1.2980. The merge records v0.1.2989 as its second
  parent and replays only the upstream delta, so the fork's fnOS overlay
  remains unchanged.

## [0.1.2980] - 2026-11-05

### Changed

- Track upstream [yetone/magpie v0.1.2980](https://github.com/yetone/magpie/releases/tag/v0.1.2980),
  15 commits past v0.1.2965, on top of the bridge-in-package rework
  (0.1.2242-9/-10, merged as #8). The upstream merge needed 66 files
  resolved to the v0.1.2980 side: the #10 squash merge left v0.1.2965
  outside main's ancestry, so git re-diffed everything against the
  v0.1.2242 base; the fork never modifies upstream code, so the tag's
  content is the resolution everywhere upstream owns a path.

## [0.1.2965] - 2026-11-04

### Changed

- Track upstream [yetone/magpie v0.1.2965](https://github.com/yetone/magpie/releases/tag/v0.1.2965):
  1,015 commits since v0.1.2242. The merge was clean — no conflict in
  `fnos/` or in the fork's own workflows. `fnos/UPSTREAM_VERSION`, both
  compose image tags and the FPK version now pin 0.1.2965.

### Fixed

- Recorded here from 0.1.2242-5/-6: the web key is negotiated between the
  two containers at start (fnOS starts compose before the install callbacks
  and does not export `TRIM_*` to them), `BRIDGE_AUTH_HEADER` defaults to
  `X-Trim-Userid`, and a stale file at the socket path no longer blocks the
  bridge from listening.

## [0.1.2242-10] - 2026-11-01

### Added

- Update channels (`fnos/UPDATE_CHANNEL`): `stable-<days>` (default
  `stable-7`) tracks only full releases that have been out for at least
  the soak window - upstream flags every build as a full release, so age
  is the only stability signal - and `latest` tracks the newest one.
  Verified against live data (1163 releases scanned, picks the newest
  qualifying one).

## [0.1.2242-9] - 2026-10-31

### Fixed

- Live-device installs uncovered the real layout: the package contents
  extract to the install target root (no `app/` prefix), and the compose
  project is created before any lifecycle callback runs, so the web key
  cannot be prepared host-side. The key is now negotiated between the
  containers at start (bridge writes `${TRIM_APPDEST}/docker/magpie.env`,
  the magpie entrypoint waits for it and sources it).
- The bridge ships as a static binary inside the FPK (amd64 + arm64) and
  runs on an image already present on the device - no second image to
  build, publish or pull.

## [0.1.2242-3] - 2026-10-31

### Fixed

- The web key file now lives inside the install target
  (`${TRIM_APPDEST}/app/docker/magpie.env`), the one location every stage
  of the real install flow demonstrably provides: `TRIM_PKGVAR` proved
  unavailable to the install scripts on a live device, which failed the
  install ("执行应用初始化脚本异常"). `install_init` degrades to a no-op
  when the target is not populated yet; `install_callback` generates the
  key, and `upgrade_callback` regenerates it after upgrades replace the
  package files.

## [0.1.2242-2] - 2026-10-31

### Fixed

- The web key (`MAGPIE_WEB_KEY`) now lives in `${TRIM_PKGVAR}/magpie.env`,
  generated by `cmd/install_init` before the compose project starts; the
  compose file reads it via `env_file` instead of variable interpolation,
  which fnOS's compose runner does not source from the package. The bridge
  reads `MAGPIE_WEB_KEY` directly.
- fpk-release skips the image builds when the pinned tags are already on
  ghcr.io.

## [0.1.2242] - 2026-10-31

### Added

- fnOS app packaging under `fnos/`: manifest, lifecycle scripts, compose
  (magpie + bridge, no published ports), icons, wizard placeholder.
- `fnos/bridge`: Unix-socket adapter for the fnOS unified gateway —
  prefix strip, fnOS-session cookie injection, streaming reverse proxy
  with a runtime fetch-URL shim injected into the page HTML.
- CI: `ci.yml` (lint/test/package), `upstream-sync.yml` (scheduled merge of
  upstream release tags), `fpk-release.yml` (multi-arch images + FPK
  release `fnos-vX.Y.Z`).
- Repository hygiene files (CONTRIBUTING, SECURITY, CODE_OF_CONDUCT,
  CHANGELOG) and the fnOS user guide `FNOS.md`.
