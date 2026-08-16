#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="$repo_root/scripts/ops/acceptance-smoke.sh"
doc="$repo_root/docs/testing/operational-acceptance.md"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-acceptance.XXXXXX")"
http_server_pid=""
cleanup() {
  if [[ -n "$http_server_pid" ]]; then
    kill "$http_server_pid" 2>/dev/null || true
    wait "$http_server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

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
  'HEXROUTE_ACCEPTANCE_MANUAL_NO_EXTERNAL_RESCUE' \
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
  '"label":"sleep_wake_completed"' \
  '"label":"no_external_rescue_used"'; do
  rg -Fq "$term" "$evidence"
done

set +e
HEXROUTE_ACCEPTANCE_OUT_DIR="$tmp/reboot-out" \
HEXROUTE_ACCEPTANCE_URL_INTERNET="http://127.0.0.1:1/" \
HEXROUTE_ACCEPTANCE_MANUAL_CHATGPT_BROWSER=pass \
HEXROUTE_ACCEPTANCE_MANUAL_CODEX_MESSAGE=pass \
HEXROUTE_ACCEPTANCE_MANUAL_GIT_WRITE=pass \
HEXROUTE_ACCEPTANCE_MANUAL_PRITUNL_OTP=pass \
HEXROUTE_ACCEPTANCE_MANUAL_REBOOT=pass \
"$script" --phase post-reboot >/tmp/hexroute-acceptance-reboot.out
reboot_probe_status=$?
set -e
test "$reboot_probe_status" -eq 1
reboot_evidence="$(find "$tmp/reboot-out" -type f -name 'operational-acceptance-post-reboot-*.json' | head -n 1)"
test -s "$reboot_evidence"
rg -Fq '"label":"no_external_rescue_used","status":"incomplete"' "$reboot_evidence"

if rg -Fq "$canary" "$evidence"; then
  printf 'acceptance evidence leaked protected canary\n' >&2
  exit 1
fi

port_file="$tmp/http-port"
ruby -rsocket -e '
server = TCPServer.new("127.0.0.1", 0)
File.write(ARGV.fetch(0), server.addr.fetch(1).to_s)
loop do
  client = server.accept
  client.gets
  while (line = client.gets)
    break if line == "\r\n"
  end
  client.write("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
  client.close
end
' "$port_file" &
http_server_pid=$!
for _ in 1 2 3 4 5; do
  [[ -s "$port_file" ]] && break
  sleep 0.1
done
test -s "$port_file"
port="$(cat "$port_file")"

set +e
HEXROUTE_ACCEPTANCE_OUT_DIR="$tmp/http-out" \
HEXROUTE_ACCEPTANCE_TARGETS="$tmp/missing-targets.env" \
HEXROUTE_ACCEPTANCE_URL_INTERNET="http://127.0.0.1:$port/" \
"$script" --phase baseline >/tmp/hexroute-acceptance-http.out
http_probe_status=$?
set -e
test "$http_probe_status" -eq 1
http_evidence="$(find "$tmp/http-out" -type f -name 'operational-acceptance-baseline-*.json' | head -n 1)"
test -s "$http_evidence"
rg -Fq '"label":"ordinary_internet","kind":"http","status":"pass"' "$http_evidence"
rg -Fq '"exit_class":"http_4xx"' "$http_evidence"

rg -Fq 'docs/testing/operational-acceptance.md' "$repo_root/README.md"
rg -Fq 'add-operational-acceptance-drill' "$repo_root/docs/roadmap.md"
rg -Fq 'operational acceptance drill' "$repo_root/docs/roadmap.md"
rg -Fq 'tests/operational_acceptance_drill_test.sh' "$repo_root/Makefile"

printf 'ok: operational acceptance drill is documented, redacted and non-mutating\n'
