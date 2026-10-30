# fnos/ — project overlay for the fnOS packaging

Shared agent rules: [CallMeHFK/open-source-repo-rules](https://github.com/CallMeHFK/open-source-repo-rules).
That ruleset defines *what* must hold; this overlay defines *how to verify
it here*. It layers on top of upstream's [AGENTS.md](../AGENTS.md) (magpie's
own code standards); for `fnos/` work, this file wins on packaging matters.

## What this fork adds

Upstream `yetone/magpie` (AI gateway, Go, MIT) is packaged as a fnOS app:

- `bridge/` — Go Unix-socket adapter: strips the `/app/magpie` gateway
  prefix, injects magpie's web key cookie for fnOS-authenticated sessions,
  proxies to magpie web's one-port server (`:3430` in-container; page,
  `/api/*` and the `/v1`,`/v1beta`,`/mcp` client routes together)
- `app/` — the fnpack project (manifest, lifecycle scripts, compose, icons)
- `.github/workflows/` — CI, upstream sync, FPK release (see section below)

Upstream code is not modified. The page shim (fetch-URL rewrite) is
injected at runtime by the bridge, so upstream merges stay conflict-free.

## Branches and commits

- Default branch `main` = upstream + `fnos/`. Feature branches: `fnos/…`
  (for example `fnos/fix-bridge-timeouts`), merged by PR.
- Scope matcher for subjects and PR titles: `fnos|bridge|fpk|ci|sync`.
- Upstream's own commit style (prose subjects) applies to upstream code;
  packaging changes follow Conventional Commits from the shared rules.

## Build, test, verify

| What | Command | Notes |
|---|---|---|
| Bridge: test | `cd fnos/bridge && go test ./...` | stdlib-only, Go 1.26 |
| Bridge: vet | `cd fnos/bridge && go vet ./...` | |
| Bridge: build | `cd fnos/bridge && CGO_ENABLED=0 go build .` | |
| Package: build | `cd fnos/app && fnpack build` | fnpack 1.2.3 |
| Package: inspect | `unzip -l magpie.fpk` | manifest + compose + cmd/* present |
| Deploy test | `appcenter-cli install-fpk magpie.fpk` | real fnOS device; user guide: [FNOS.md](../FNOS.md) |

CI check names (branch protection contexts): `lint`, `test`, `package`.
A PR merges only after all three report on its tip.

## Upstream sync protocol

`upstream-sync.yml` (schedule + manual) does, on a `fnos/sync-*` branch:

1. Read the tracked version from `fnos/UPSTREAM_VERSION`.
2. `gh api repos/yetone/magpie/tags` — pick the newest `vX.Y.Z` newer than
   tracked (semver; highest wins). Tags, not upstream `main`.
3. Merge that tag into `main`; in the same commit update
   `fnos/UPSTREAM_VERSION` and both `image:` tags in
   `app/app/docker/docker-compose.yaml` (`chore(fnos): track upstream
   vX.Y.Z`).
4. PR with `gh pr merge --auto --squash`; CI green merges it, and
   `fpk-release.yml` publishes `fnos-vX.Y.Z` with the FPK.

FPK version = upstream version. Our releases are automated artifacts that
track upstream; the deliberate deviation from the shared release rules is
documented here: the release body names the upstream version and links its
notes, `CHANGELOG.md` records fork-side changes only.

## Secrets and privacy

- `MAGPIE_WEB_KEY` is device-generated at install (`cmd/install_callback`,
  backstopped by the other lifecycle callbacks) into
  `${TRIM_APPDEST}/app/docker/magpie.env`, which the compose file reads via
  `env_file`; upgrades regenerate it.
  Never commit the file; `.gitignore` covers the local forms, and the
  pre-commit screen in the shared commit rules runs before every commit.
- No real device hostnames, usernames, LAN IPs or tokens in the repo —
  placeholder forms only.

## Multi-agent notes

All agents share the single forge account. Per `multi-agent-rules.md`: one
leased integrator at a time; sync branches (`fnos/sync-*`) are the
integrator's claim; workers' own `git fetch` and read-only `gh` calls are
exempt. Automated sync PRs self-merge under branch protection with
`required_approving_review_count: 0` (the solo-repo setting).
