# Magpie for fnOS (飞牛)

This fork packages [yetone/magpie](https://github.com/yetone/magpie) — the
local AI gateway that lets Claude Code, Codex, Gemini CLI and friends use
any model — as a 飞牛 fnOS app. After install it shows up in the fnOS
desktop; via 飞牛 Connect the same app works from the phone App, anywhere.

## How it fits together

```
fnOS desktop / 飞牛 App / browser
   │  (fnOS session auth; remotely via 飞牛 Connect relay)
   ▼
fnOS 统一网关  https://<device>/app/magpie/*
   │  (Unix socket /var/apps/magpie/target/app.sock)
   ▼
bridge  (fnos/bridge, a ~200-line Go adapter)
   │  strips /app/magpie, injects the web key cookie for
   │  fnOS-authenticated sessions, streams everything
   ▼
magpie web one-port server  :3430
   ├─ page + /api/*            (web console)
   └─ /v1 /v1beta /mcp …       (gateway client routes, magpie's own guards)
```

- Neither container publishes a port. The only entrance is the fnOS
  gateway, which requires a logged-in fnOS session.
- The magpie model gateway itself still requires a **gateway key** for
  client routes (magpie's own security, unchanged). Issue one in the web
  console; point agents at `https://<device-domain>/app/magpie/v1`.
- No upstream code is modified: the bridge injects a small fetch-URL shim
  into the page's HTML at runtime so the console works under the
  `/app/magpie` sub-path.

## Install (local test package)

1. Download `magpie.fpk` from this fork's Releases (`fnos-vX.Y.Z`).
2. fnOS 应用中心 → 手动安装 → choose the `.fpk`.
3. First install pulls the images from ghcr.io (`magpie-fnos` and
   `magpie-fnos-bridge`, version-pinned). On networks where ghcr.io is slow,
   configure a registry mirror in the fnOS Docker settings first.
4. Open Magpie from the fnOS desktop.

## Agent configuration example (via Connect)

```
base_url = https://<your-device>.connect.fnnas.com/app/magpie/v1
api_key  = <a gateway key created in magpie's console>
```

Works the same on the LAN: `https://<device>/app/magpie/v1`.

## Files

| Path | Purpose |
|---|---|
| `fnos/app/manifest` | fnOS app metadata; version tracks upstream |
| `fnos/app/app/docker/docker-compose.yaml` | magpie + bridge, no published ports |
| `fnos/app/cmd/main` | status reporting (`magpie-app` container) |
| `fnos/app/cmd/install_callback` | generates `MAGPIE_WEB_KEY` into app conf |
| `fnos/app/cmd/upgrade_callback` | restores the key file, pulls pinned images |
| `fnos/bridge/` | the gateway adapter (Go, stdlib only) |

## Building the package yourself

```sh
# fnpack 1.2.3 from https://developer.fnnas.com/docs/cli/fnpack/
cd fnos/app && fnpack build    # -> magpie.fpk
```

CI publishes both container images (`magpie-fnos`, `magpie-fnos-bridge`)
multi-arch (amd64 + arm64) on every tracked upstream release.

## Privacy & security notes

- magpie's settings, accounts and keys stay in `${TRIM_PKGVAR}/config` —
  app data that survives upgrades and is removed on uninstall with the app.
- The bridge trusts the fnOS gateway's `X-Trim-Userid` header to decide
  whether to inject the web key cookie; the socket it listens on is only
  reachable through the app's own directory on the NAS.
- Upstream security policy (magpie itself) stays with upstream; packaging
  and bridge issues: see `SECURITY.md` in this fork.
