# Security Policy

## Scope

Two layers live in this repository:

1. **magpie itself** — the AI gateway from `yetone/magpie`. Vulnerabilities
   in magpie's code belong to upstream: report them per
   [upstream's security policy](https://github.com/yetone/magpie/blob/main/SECURITY.md)
   (or its docs). Do not open a public issue.
2. **The fnOS packaging** — `fnos/bridge` and `fnos/app` (compose, scripts,
   workflows) added by this fork.

## Reporting a vulnerability in the packaging

Use GitHub's private vulnerability reporting on this repository
(Security → Report a vulnerability). Include: what you found, the
component (`bridge` or packaging), and how to reproduce. You'll get an
acknowledgement within 7 days and a fix or a mitigation plan within 90
days, per the shared
[security policy rules](https://github.com/CallMeHFK/open-source-repo-rules/blob/main/security-policy-rules.md)
this repo follows.

## Known design boundaries (not vulnerabilities)

- The bridge container runs as root inside its container so it can create
  the gateway socket in the root-owned install target directory. It is a
  fixed-function binary: no shell, no config parsing beyond a few
  environment variables, and the only egress is the fixed
  `BRIDGE_UPSTREAM` (an internal docker service name); it never fetches
  request-supplied URLs.
- The bridge injects magpie's web key cookie only for requests carrying
  the fnOS gateway's `X-Trim-Userid` header. A socket client without that
  header reaches magpie's own sign-in page only.
- The bridge listens on a `0666` Unix socket in the app's target
  directory; access to that directory is already controlled by fnOS, and
  a connection alone grants nothing (the key cookie is not injected).
- Neither container publishes a port; the only entrance is the fnOS
  unified gateway, which requires a fnOS session. Gateway client routes
  additionally require magpie's own gateway key (magpie's guard, unchanged
  from upstream).
