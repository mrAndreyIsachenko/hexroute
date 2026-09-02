#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# Five launchd jobs, each with its own test. What none of them checks is the
# thing all five share: a job's identity is written twice, in the plist's file
# name and in its Label key, and every installer derives one from the other.
#
# `launchctl bootout system/<label>` uses the name; the loaded job answers to
# the key. If they disagree, install works, uninstall silently boots out
# nothing, and the old job keeps running under a name nobody is looking at.
# They agree today and nothing was keeping them agreeing.

buddy=/usr/libexec/PlistBuddy
[[ -x "$buddy" ]] || {
  printf 'PlistBuddy is required to read a plist\n' >&2
  exit 1
}

shopt -s nullglob
plists=(deploy/macos/*.plist)
[[ "${#plists[@]}" -gt 0 ]] || {
  printf 'no launchd plists found; this gate reads deploy/macos\n' >&2
  exit 1
}

status=0

for plist in "${plists[@]}"; do
  stem="$(basename "$plist" .plist)"
  label="$("$buddy" -c "Print :Label" "$plist" 2>/dev/null || true)"

  if [[ -z "$label" ]]; then
    printf '%s has no Label\n' "$plist" >&2
    status=1
    continue
  fi
  if [[ "$label" != "$stem" ]]; then
    printf '%s is named for %s and answers to %s\n' "$plist" "$stem" "$label" >&2
    printf '  uninstall boots out the name and the job answers to the key,\n' >&2
    printf '  so a mismatch leaves the old job running unnoticed\n' >&2
    status=1
  fi

  # A plist nothing installs is a job that cannot start. It is the same
  # question the package census asks about a package in no binary.
  if ! grep -rqF "$stem" scripts/macos/*.sh; then
    printf '%s is installed by no script under scripts/macos\n' "$plist" >&2
    status=1
  fi

  # A scheduled job and a resident daemon want opposite things, and the plist
  # says which it is. A daemon that should stay up asks for KeepAlive; a job
  # that runs on an interval must not, because a run that ends is a run that
  # finished and restarting it turns its exit status into a crash loop.
  #
  # The first version of this refused KeepAlive outright and failed three
  # daemons that are right to have it.
  scheduled=0
  grep -q '<key>StartInterval</key>' "$plist" && scheduled=1
  grep -q '<key>StartCalendarInterval</key>' "$plist" && scheduled=1
  if [[ "$scheduled" == "1" ]] && grep -q '<key>KeepAlive</key>' "$plist"; then
    printf '%s runs on a schedule and asks to be restarted on exit\n' \
      "$plist" >&2
    printf '  a run that ends is a run that finished; restarting it makes its\n' >&2
    printf '  exit status read as a crash loop\n' >&2
    status=1
  fi
  if [[ "$scheduled" == "0" ]] && ! grep -q '<key>KeepAlive</key>' "$plist"; then
    printf '%s is resident and does not ask to be restarted\n' "$plist" >&2
    printf '  a daemon with no schedule and no KeepAlive stays down after its\n' >&2
    printf '  first exit\n' >&2
    status=1
  fi

  # Every job writes somewhere. A job with no log has nothing to look at on
  # the day it matters.
  if ! grep -q '<key>StandardErrorPath</key>' "$plist"; then
    printf '%s writes no error log\n' "$plist" >&2
    status=1
  fi
done

[[ "$status" -eq 0 ]] || exit 1

printf 'ok: every launchd job answers to the name it is installed under (%d jobs)\n' \
  "${#plists[@]}"
