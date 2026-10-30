# Changelog

All notable changes to the fnOS packaging in this fork. Upstream magpie
changes are not duplicated here; each `fnos-vX.Y.Z` release names the
upstream version it packages (see AGENTS/fnos release protocol).

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
