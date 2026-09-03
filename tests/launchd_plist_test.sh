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

# The other direction. Every plist is installed by something, checked above;
# what nothing asked was whether every binary meant to run continuously has a
# job at all.
#
# hexroute-sentinel had none. A binary with a SHALL requirement behind it and a
# configuration example committed beside it, and nothing anywhere that could
# start it — found by hand, and only because a task turned out to be waiting on
# it. The package census cannot see this: it asks whether a package is in a
# binary, and the sentinel is a binary.
#
# Residency is a decision and cannot be derived: a command that happens to have
# no job today is exactly what this is looking for, so inferring the list from
# the jobs would make it assert itself.
resident=(
  hexrouted
  hexroute-userd
  hexroute-sentinel
)

# Commands that run on a schedule rather than continuously. Same question, and
# the answer is a job either way.
scheduled=(
  hexroute-connectivity-watch
  hexroute-archive-report
  hexroute-policy-qualification
)

for binary in "${resident[@]}" "${scheduled[@]}"; do
  [[ -d "cmd/$binary" ]] || {
    printf 'cmd/%s is listed as needing a job and does not exist\n' "$binary" >&2
    status=1
    continue
  }
  if ! grep -rqF "/$binary<" deploy/macos/*.plist &&
    ! grep -rqF "$binary" scripts/macos/*.sh; then
    printf '%s is meant to run and nothing installs it\n' "$binary" >&2
    printf '  give it a launchd job and an installer, or say why it runs by hand\n' >&2
    status=1
  fi
done

# An installer that copies a configuration has to survive that configuration
# already being at its destination. That is the ordinary case on reinstall —
# a new binary, or a plist that gained an argument — and `install` refuses to
# copy a file onto itself.
#
# It failed exactly this way on the root daemon, and because it failed midway
# the binary was already replaced while the plist was not: a half-installed
# daemon whose loaded job was still the old one, and no message saying so.
for installer in scripts/macos/*-launchd.sh; do
  grep -q '"\$config"' "$installer" || continue
  if ! grep -q -- '-ef' "$installer"; then
    printf '%s copies a configuration and does not survive it already being\n' \
      "$installer" >&2
    printf '  in place; install refuses to copy a file onto itself and exits\n' >&2
    printf '  midway, leaving the binary replaced and the plist not\n' >&2
    status=1
  fi
done

[[ "$status" -eq 0 ]] || exit 1

printf 'ok: every launchd job answers to the name it is installed under, and every binary meant to run has one (%d jobs, %d run continuously)\n' \
  "${#plists[@]}" "${#resident[@]}"
