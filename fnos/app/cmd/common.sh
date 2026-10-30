#!/bin/bash
# Lifecycle scripts self-locate the app target directory: fnOS builds do
# not export the TRIM_* variables to these scripts (verified on a live
# device), while the compose file gets ${TRIM_APPDEST} interpolated fine.
# fnpack built on Windows drops the executable bit, so rebuild FPKs on
# Linux (CI does) or repack; see fnos/AGENTS.md.
find_target() {
	local p
	if [ -n "${TRIM_APPDEST:-}" ] && [ -d "${TRIM_APPDEST}/app" ]; then
		printf '%s' "${TRIM_APPDEST}"
		return 0
	fi
	p="$(cd "$(dirname "$0")/.." 2>/dev/null && pwd)" || p=""
	case "$p" in
	*/cmd) p="${p%/*}" ;;
	esac
	if [ -n "$p" ] && [ -d "$p/app" ]; then
		printf '%s' "$p"
		return 0
	fi
	for p in /var/apps/magpie/target /vol*/@appcenter/magpie; do
		[ -d "$p/app" ] && {
			printf '%s' "$p"
			return 0
		}
	done
	return 1
}
