# Publishing Magpie to the fnOS 应用中心

Follows [上架应用](https://developer.fnnas.com/docs/quick-started/publish-application/).
This page is the checklist for this package; a box is only ticked with the
evidence next to it. Anything not run is listed as not run.

## 1. Where to submit

Before the developer backend opens, applications go in through the
应用中心开发者先锋交流群: join any 飞牛 fan group from the 飞牛 website (微信群
QR code), ask the community host for the developer group, and submit the
app information, the package and the test material there. Once the backend
is live, its flow replaces this.

## 2. Release material

| Material | Here | State |
|---|---|---|
| Final `.fpk` from `fnpack build` | built by `.github/workflows/fpk-release.yml` on every `fnos/**` push to `main`, attached to the `fnos-vX.Y.Z` release | not built locally: `fnpack` is not installed in this checkout (CI runs it) |
| App icons | `fnos/app/ICON.PNG` (64x64), `fnos/app/ICON_256.PNG` (256x256), entry icons `app/ui/images/icon_{64,128,256}.png` | done: rounded body, transparent corners, 64 px still legible, every file under 1024 KB |
| Screenshots | not in the repo | **not taken** - they must show the real console (Providers, Requests, a gateway key), never a mock-up or a page under construction |
| manifest fields | `fnos/app/manifest` | appname, version, display_name, desc, source, platform, maintainer(_url), distributor(_url), os_min_version, ctl_stop, checkport, service_port, disable_authorization_path, desktop_uidir, desktop_applaunchhanme, hangelog all set |

Screenshots to take, on a device with the package installed:

1. fnOS desktop with the Magpie icon.
2. Magpie console, Providers page, with at least one subscription signed in.
3. The gateway-key page (a key issued, its `/app/magpie/v1` base URL shown).
4. Requests page with a few requests, or the model picker.

Hide real account names, hostnames, LAN addresses and keys before
submitting; the console's own masking does not cover every field.

## 3. Pre-release tests

The guide asks for more than one successful install, on each fnOS version
and architecture that is claimed. `platform=all` claims x86 and ARM.

| Test | State |
|---|---|
| Install from 应用中心 → 手动安装 and see the app | not run (no fnOS device here); CI's `package` job proves the FPK builds and holds `manifest`, `cmd/main`, `config/privilege`, both icons, the compose file and `ui/config` |
| Open the entry from the desktop | not run |
| 统一网关 path (`/app/magpie/`) through 飞牛 Connect | not run |
| Main flow: add a subscription, issue a gateway key, send one request | not run |
| Data: accounts and keys survive a container restart **and** a package upgrade | not run; static: `/config` is the `magpie-config` named volume |
| A non-administrator cannot reach the console or its data | bridge unit tests only: `go test ./fnos/bridge` - `TestCookieOnlyForAdmin` (no cookie without `X-Trim-Isadmin: true`); the device path is not run |
| Only the ports and background tasks the app needs are exposed | static: only 3425 is published (model gateway, gateway keys required); the console has no port |
| Dependency unavailable / misconfigured: ghcr.io unreachable, no outbound network | not run; `cmd/upgrade_callback` pulls best effort (`|| true`) |
| Upgrade keeps working: `upgrade_callback` finds the target and pulls the new image | not run; the script's target lookup was fixed in 0.1.3049-1 (it looked for `<target>/app`, which the fnOS layout never creates) |

## 4. What CI does check

`ci.yml` on every PR: `go vet` and `shellcheck` (`lint`), `go test`
(`test`), and `fnpack build` plus an FPK layout check (`package`). Those are
the three required contexts on `main`. They do not install anything, so
none of section 3 is covered by CI.
