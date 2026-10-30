# Contributing to the magpie fnOS packaging

This fork packages [yetone/magpie](https://github.com/yetone/magpie) as a
飞牛 fnOS app. Upstream code is not modified; all fork work happens under
`fnos/`. Read [fnos/AGENTS.md](fnos/AGENTS.md) for the project overlay
(build/test/verify commands, sync protocol, secrets rules) and
[FNOS.md](FNOS.md) for what the packaging does.

## Dev setup

- Go 1.26 for the bridge (`cd fnos/bridge && go test ./...`)
- [fnpack 1.2.3](https://developer.fnnas.com/docs/cli/fnpack/) for the FPK
  (`cd fnos/app && fnpack build`)
- A fnOS test device with admin access for deploy tests (guide: FNOS.md)

## The test command

`cd fnos/bridge && go test ./...` — plus `fnpack build` when packaging
files change.

## Review and PR conventions

Follow the shared rules at
[CallMeHFK/open-source-repo-rules](https://github.com/CallMeHFK/open-source-repo-rules)
(`pr-rules.md`, `commit-message-rules.md`): Conventional Commits, squash
merge, PR title = commit subject. Scope matcher: `fnos|bridge|fpk|ci|sync`.

## Contributor-origin terms

None — no DCO or CLA. By contributing you agree your packaging
contributions are MIT like the rest of the repo.
