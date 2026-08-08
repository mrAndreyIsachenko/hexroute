#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PLIST="$ROOT/deploy/macos/com.hexroute.policy-qualification.plist"
INSTALLER="$ROOT/scripts/macos/policy-qualification-launchd.sh"
FAULTS="$ROOT/scripts/macos/policy-qualification-faults.sh"

[[ -f "$PLIST" ]]
[[ -x "$INSTALLER" ]]
[[ -x "$FAULTS" ]]
if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$PLIST" >/dev/null
fi
grep -q '<string>com.hexroute.policy-qualification</string>' "$PLIST"
grep -q '<string>serve</string>' "$PLIST"
grep -q '<string>60s</string>' "$PLIST"
grep -q '<string>180s</string>' "$PLIST"
grep -q 'private qualification evidence was preserved' "$INSTALLER"
grep -q 'arm-sleep' "$INSTALLER"
grep -q 'TestPolicyCoordinatorConvergesAcrossDeterministicCrashBoundaries' "$FAULTS"

dependencies="$(go list -deps ./cmd/hexroute-policy-qualification)"
for forbidden in \
  github.com/mrAndreyIsachenko/hexroute/internal/actionplan \
  github.com/mrAndreyIsachenko/hexroute/internal/credentials \
  github.com/mrAndreyIsachenko/hexroute/internal/pritunlrescue \
  github.com/mrAndreyIsachenko/hexroute/internal/resumeexecutor \
  github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon \
  github.com/mrAndreyIsachenko/hexroute/internal/routeplan \
  github.com/mrAndreyIsachenko/hexroute/internal/userdaemon; do
  if grep -Fxq "$forbidden" <<<"$dependencies"; then
    printf 'error: qualification agent imports mutation authority: %s\n' "$forbidden" >&2
    exit 1
  fi
done

printf 'ok: policy qualification LaunchAgent is disjoint and mutation-free\n'
