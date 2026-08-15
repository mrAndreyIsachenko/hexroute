#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/scripts/ops/acceptance-smoke.sh"
doc="$repo_root/docs/testing/operational-acceptance.md"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-acceptance.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

test -x "$script"
test -s "$doc"

for term in \
  'operational acceptance drill' \
  'baseline' \
  'post-activation' \
  'post-sleep' \
  'post-reboot' \
  'post-network-loss' \
  'ordinary_internet' \
  'codex_chatgpt_http' \
  'gitlab_web' \
  'git_transport' \
  'pritunl_process' \
  'adguard_process' \
  'twilight_status' \
  'telegram_monitoring' \
  'fallback_path' \
  'private/acceptance-targets.env' \
  'not_configured' \
  'Manual Checkpoints' \
  'Waivers' \
  'does not enable a production adapter'; do
  rg -Fq "$term" "$doc"
done

for forbidden in \
  'sudo ' \
  'launchctl bootout' \
  'launchctl bootstrap' \
  'launchctl kickstart' \
  'route add' \
  'route delete' \
  'route change' \
  'networksetup -set' \
  'scutil --set' \
  'kill ' \
  'pkill' \
  'make up' \
  'make down' \
  'docker compose up' \
  'terraform apply' \
  'security find-generic-password'; do
  if rg -n --fixed-strings "$forbidden" "$script"; then
    printf 'acceptance smoke script contains forbidden mutation surface: %s\n' "$forbidden" >&2
    exit 1
  fi
done

canary='otp-pin-secret-live-host.git.example'
HEXROUTE_ACCEPTANCE_OUT_DIR="$tmp/out" \
HEXROUTE_ACCEPTANCE_SECRET_CANARY="$canary" \
HEXROUTE_ACCEPTANCE_URL_INTERNET="$canary" \
HEXROUTE_ACCEPTANCE_URL_CODEX="$canary" \
HEXROUTE_ACCEPTANCE_URL_GITLAB="$canary" \
HEXROUTE_ACCEPTANCE_GIT_REMOTE="$canary" \
"$script" --dry-run --phase post-sleep >/tmp/hexroute-acceptance-dry-run.out

evidence="$(find "$tmp/out" -type f -name 'operational-acceptance-post-sleep-*.json' | head -n 1)"
test -s "$evidence"

for term in \
  '"schema": "hexroute.operational-acceptance.v1"' \
  '"phase": "post-sleep"' \
  '"dry_run": true' \
  '"overall": "dry_run"' \
  '"label":"codex_chatgpt_http"' \
  '"label":"git_transport"' \
  '"label":"sleep_wake_completed"'; do
  rg -Fq "$term" "$evidence"
done

if rg -Fq "$canary" "$evidence"; then
  printf 'acceptance evidence leaked protected canary\n' >&2
  exit 1
fi

rg -Fq 'docs/testing/operational-acceptance.md' "$repo_root/README.md"
rg -Fq 'add-operational-acceptance-drill' "$repo_root/docs/roadmap.md"
rg -Fq 'operational acceptance drill' "$repo_root/docs/roadmap.md"
rg -Fq 'tests/operational_acceptance_drill_test.sh' "$repo_root/Makefile"

printf 'ok: operational acceptance drill is documented, redacted and non-mutating\n'
