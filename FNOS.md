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
bridge  (fnos/bridge, a ~250-line Go adapter, shipped as a static
   │      binary inside the package and run on a stock shell image)
   │  strips /app/magpie, injects the web key cookie for
   │  fnOS-authenticated sessions, streams everything
   ▼
magpie web one-port server  :3430
   ├─ page + /api/*            (web console)
   └─ /v1 /v1beta /mcp …       (gateway client routes, magpie's own guards)
```

- The console has no port of its own. Its only entrance is the fnOS
  gateway, which requires a logged-in fnOS session and lists the entry for
  administrators alone (`allUsers=false`). The one published port is the
  model-gateway port 3425, which LAN agents call directly, and magpie
  demands a gateway key on every non-loopback request there.
- The magpie model gateway itself still requires a **gateway key** for
  client routes (magpie's own security, unchanged). Issue one in the web
  console; point agents at `https://<device-domain>/app/magpie/v1`.
- No upstream code is modified: the bridge injects a small fetch-URL shim
  into the page's HTML at runtime so the console works under the
  `/app/magpie` sub-path.

## Install (local test package)

1. Download `magpie.fpk` from this fork's Releases (`fnos-vX.Y.Z`).
2. fnOS 应用中心 → 手动安装 → choose the `.fpk`.
3. First install pulls the version-pinned `magpie-fnos` image from
   ghcr.io (the bridge ships inside the package and needs no image). On
   networks where ghcr.io is slow, configure a registry mirror in the fnOS
   Docker settings first.
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
| `fnos/app/app/docker/docker-compose.yaml` | magpie + bridge; only the model-gateway port 3425 is published |
| `fnos/app/cmd/main` | status reporting (`magpie-app` container) |
| `fnos/app/cmd/install_init` | lifecycle marker; the web key is negotiated at container start |
| `fnos/app/cmd/upgrade_callback` | pulls the images the new version pins |
| `fnos/bridge/` | the gateway adapter (Go, stdlib only) |

## How the package follows the fnOS app rules

| Rule | How this package does it |
|---|---|
| 统一网关, not a published console port | `gatewayPrefix=/app/magpie`, `gatewaySocket=app.sock`; the bridge listens on `<target>/app.sock` and the two containers publish no console port |
| 管理接口需要管理员身份 | the entry is `allUsers=false` + `control.accessPerm=readonly`, and the bridge withholds the web key cookie unless `X-Trim-Isadmin` is `true` |
| `service_port` names the service port | `service_port=3425` (the model gateway LAN agents call) |
| 端口检查 after install | `checkport=false`: the first install pulls a multi-arch image, so a port probe would report a started app that has nothing to serve yet |
| 授权目录 | `disable_authorization_path=true`: magpie needs no user-chosen file or directory |
| 数据可保存、读取、更新 | `/config` is the `magpie-config` named volume, not an image anonymous volume |
| 不暴露密钥 | the key lives in `<target>/magpie.env`, `0600`, owned by the container uid |
| 图标 | `ICON.PNG` 64x64 and `ICON_256.PNG` 256x256, rounded body, clear corners, under 1024 KB |

## Building the package yourself

```sh
# fnpack 1.2.3 from https://developer.fnnas.com/docs/cli/fnpack/
cd fnos/app && fnpack build    # -> magpie.fpk
```

CI publishes the magpie container image (multi-arch) when its tag is
missing; the bridge ships inside the FPK as a static binary - no image
builds, no registry dependency beyond the magpie image itself.

## Privacy & security notes

- magpie's settings, accounts and gateway keys live in the `magpie-config`
  Docker volume mounted at `/config`. A named volume keeps its name across
  a stop/start and an upgrade, so the data comes back; it goes with the app
  on uninstall.
- The bridge injects the web key cookie only when the gateway's
  `X-Trim-Userid` names a session **and** `X-Trim-Isadmin` is `true`. The
  console changes every account, gateway key and route the device has, so
  it belongs to an administrator; anyone else gets magpie's own sign-in
  page, which they cannot pass without the key. The socket is only
  reachable through the app's own directory on the NAS.
- `<target>/magpie.env` holds that key, mode `0600`, owned by the magpie
  container's uid (65532): no other app on the device can read it.
- Upstream security policy (magpie itself) stays with upstream; packaging
  and bridge issues: see `SECURITY.md` in this fork.
