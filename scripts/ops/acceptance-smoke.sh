#!/usr/bin/env bash
set -euo pipefail

phase="baseline"
dry_run=0
targets_file="${HEXROUTE_ACCEPTANCE_TARGETS:-private/acceptance-targets.env}"
out_dir="${HEXROUTE_ACCEPTANCE_OUT_DIR:-.local/acceptance}"

usage() {
  cat <<'USAGE'
usage: scripts/ops/acceptance-smoke.sh [--phase PHASE] [--dry-run]

Phases:
  baseline
  post-activation
  post-sleep
  post-reboot
  post-network-loss

Private targets may be supplied as KEY=VALUE lines in:
  private/acceptance-targets.env

The script writes redacted evidence to .local/acceptance by default.
USAGE
}

while (($#)); do
  case "$1" in
    --phase)
      shift
      phase="${1:-}"
      ;;
    --dry-run)
      dry_run=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'error: unknown argument: %s\n' "$1" >&2
      usage >&2
      exit 64
      ;;
  esac
  shift
done

case "$phase" in
  baseline|post-activation|post-sleep|post-reboot|post-network-loss) ;;
  *)
    printf 'error: invalid phase: %s\n' "$phase" >&2
    exit 64
    ;;
esac

load_targets_file() {
  local file="$1"
  local line key value
  [[ -f "$file" ]] || return 0
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ -z "$line" || "$line" == \#* ]] && continue
    key="${line%%=*}"
    value="${line#*=}"
    if [[ "$key" == "$line" || ! "$key" =~ ^HEXROUTE_ACCEPTANCE_[A-Z0-9_]+$ ]]; then
      printf 'error: invalid target line in %s\n' "$file" >&2
      exit 65
    fi
    printf -v "$key" '%s' "$value"
  done <"$file"
}

json_bool() {
  if [[ "$1" == "1" ]]; then
    printf 'true'
  else
    printf 'false'
  fi
}

timing_bucket() {
  local seconds="$1"
  if ((seconds <= 1)); then
    printf 'lt_1s'
  elif ((seconds <= 3)); then
    printf 'lt_3s'
  elif ((seconds <= 10)); then
    printf 'lt_10s'
  else
    printf 'ge_10s'
  fi
}

checks=()
manuals=()
blocked=0
incomplete=0

record_check() {
  local label="$1" kind="$2" status="$3" timing="$4" exit_class="$5"
  checks+=("{\"label\":\"$label\",\"kind\":\"$kind\",\"status\":\"$status\",\"timing_bucket\":\"$timing\",\"exit_class\":\"$exit_class\"}")
  case "$status" in
    pass|dry_run) ;;
    not_configured|fail) blocked=1 ;;
    *) blocked=1 ;;
  esac
}

record_manual() {
  local label="$1" status="$2"
  manuals+=("{\"label\":\"$label\",\"status\":\"$status\"}")
  case "$status" in
    pass|dry_run) ;;
    incomplete) incomplete=1 ;;
    fail) blocked=1 ;;
    *) incomplete=1 ;;
  esac
}

http_check() {
  local label="$1" var_name="$2"
  local url="${!var_name:-}"
  local start finish elapsed status exit_class http_code curl_status
  if ((dry_run)); then
    record_check "$label" "http" "dry_run" "not_measured" "dry_run"
    return
  fi
  if [[ -z "$url" ]]; then
    record_check "$label" "http" "not_configured" "not_measured" "not_configured"
    return
  fi
  if ! command -v curl >/dev/null 2>&1; then
    record_check "$label" "http" "fail" "not_measured" "tool_missing"
    return
  fi
  start="$(date +%s)"
  http_code="$(
    curl -sS -L \
      --max-time "${HEXROUTE_ACCEPTANCE_HTTP_TIMEOUT_SECONDS:-8}" \
      -o /dev/null \
      -w '%{http_code}' \
      "$url" 2>/dev/null
  )"
  curl_status=$?
  if ((curl_status == 0)) && [[ "$http_code" =~ ^[0-9][0-9][0-9]$ ]] &&
    ((10#$http_code >= 200 && 10#$http_code < 500)); then
    status="pass"
    exit_class="http_${http_code:0:1}xx"
  else
    case "$curl_status" in
      6) exit_class="dns_error" ;;
      7) exit_class="connect_error" ;;
      28) exit_class="timeout" ;;
      *)
        if [[ "$http_code" =~ ^5[0-9][0-9]$ ]]; then
          exit_class="http_5xx"
        elif [[ "$http_code" == "000" ]]; then
          exit_class="http_no_response"
        else
          exit_class="probe_error"
        fi
        ;;
    esac
    status="fail"
  fi
  finish="$(date +%s)"
  elapsed=$((finish - start))
  record_check "$label" "http" "$status" "$(timing_bucket "$elapsed")" "$exit_class"
}

git_check() {
  local label="git_transport"
  local remote="${HEXROUTE_ACCEPTANCE_GIT_REMOTE:-}"
  local start finish elapsed status exit_class
  if ((dry_run)); then
    record_check "$label" "git" "dry_run" "not_measured" "dry_run"
    return
  fi
  if [[ -z "$remote" ]]; then
    record_check "$label" "git" "not_configured" "not_measured" "not_configured"
    return
  fi
  if ! command -v git >/dev/null 2>&1; then
    record_check "$label" "git" "fail" "not_measured" "tool_missing"
    return
  fi
  start="$(date +%s)"
  if GIT_TERMINAL_PROMPT=0 \
    GIT_SSH_COMMAND='ssh -o BatchMode=yes -o ConnectTimeout=8 -o StrictHostKeyChecking=yes' \
    git ls-remote --exit-code "$remote" HEAD >/dev/null 2>&1; then
    status="pass"
    exit_class="ok"
  else
    status="fail"
    exit_class="transport_error"
  fi
  finish="$(date +%s)"
  elapsed=$((finish - start))
  record_check "$label" "git" "$status" "$(timing_bucket "$elapsed")" "$exit_class"
}

process_check() {
  local label="$1" pattern="$2"
  if ((dry_run)); then
    record_check "$label" "process" "dry_run" "not_measured" "dry_run"
    return
  fi
  if ! command -v pgrep >/dev/null 2>&1; then
    record_check "$label" "process" "fail" "not_measured" "tool_missing"
    return
  fi
  if pgrep -if "$pattern" >/dev/null 2>&1; then
    record_check "$label" "process" "pass" "lt_1s" "ok"
  else
    record_check "$label" "process" "fail" "lt_1s" "not_running"
  fi
}

twilight_status_check() {
  local dir="${HEXROUTE_ACCEPTANCE_TWILIGHT_DIR:-}"
  local start finish elapsed status exit_class
  if ((dry_run)); then
    record_check "twilight_status" "local_status" "dry_run" "not_measured" "dry_run"
    return
  fi
  if [[ -z "$dir" ]]; then
    record_check "twilight_status" "local_status" "not_configured" "not_measured" "not_configured"
    return
  fi
  if [[ ! -d "$dir" ]]; then
    record_check "twilight_status" "local_status" "fail" "not_measured" "missing_directory"
    return
  fi
  start="$(date +%s)"
  if make -C "$dir" routes-status >/dev/null 2>&1; then
    status="pass"
    exit_class="ok"
  else
    status="fail"
    exit_class="status_error"
  fi
  finish="$(date +%s)"
  elapsed=$((finish - start))
  record_check "twilight_status" "local_status" "$status" "$(timing_bucket "$elapsed")" "$exit_class"
}

manual_checkpoint() {
  local label="$1" var_name="$2"
  local value="${!var_name:-}"
  if ((dry_run)); then
    record_manual "$label" "dry_run"
    return
  fi
  case "$value" in
    pass|fail) record_manual "$label" "$value" ;;
    *) record_manual "$label" "incomplete" ;;
  esac
}

load_targets_file "$targets_file"

http_check "ordinary_internet" "HEXROUTE_ACCEPTANCE_URL_INTERNET"
http_check "codex_chatgpt_http" "HEXROUTE_ACCEPTANCE_URL_CODEX"
http_check "gitlab_web" "HEXROUTE_ACCEPTANCE_URL_GITLAB"
git_check
process_check "pritunl_process" "Pritunl|pritunl-client|pritunl-service"
process_check "adguard_process" "AdGuard|com.adguard"
twilight_status_check
http_check "telegram_monitoring" "HEXROUTE_ACCEPTANCE_URL_MONITORING"
http_check "fallback_path" "HEXROUTE_ACCEPTANCE_URL_FALLBACK"

manual_checkpoint "browser_chatgpt_login" "HEXROUTE_ACCEPTANCE_MANUAL_CHATGPT_BROWSER"
manual_checkpoint "codex_message_sent" "HEXROUTE_ACCEPTANCE_MANUAL_CODEX_MESSAGE"
manual_checkpoint "git_push_or_write_verified" "HEXROUTE_ACCEPTANCE_MANUAL_GIT_WRITE"
manual_checkpoint "pritunl_otp_fallback" "HEXROUTE_ACCEPTANCE_MANUAL_PRITUNL_OTP"

case "$phase" in
  post-sleep)
    manual_checkpoint "sleep_wake_completed" "HEXROUTE_ACCEPTANCE_MANUAL_SLEEP_WAKE"
    manual_checkpoint "no_external_rescue_used" "HEXROUTE_ACCEPTANCE_MANUAL_NO_EXTERNAL_RESCUE"
    ;;
  post-reboot)
    manual_checkpoint "reboot_completed" "HEXROUTE_ACCEPTANCE_MANUAL_REBOOT"
    manual_checkpoint "no_external_rescue_used" "HEXROUTE_ACCEPTANCE_MANUAL_NO_EXTERNAL_RESCUE"
    ;;
  post-network-loss)
    manual_checkpoint "network_loss_recovered" "HEXROUTE_ACCEPTANCE_MANUAL_NETWORK_LOSS"
    manual_checkpoint "no_external_rescue_used" "HEXROUTE_ACCEPTANCE_MANUAL_NO_EXTERNAL_RESCUE"
    ;;
esac

overall="pass"
if ((dry_run)); then
  overall="dry_run"
elif ((blocked)); then
  overall="blocked"
elif ((incomplete)); then
  overall="incomplete"
fi

mkdir -p "$out_dir"
created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
evidence="$out_dir/operational-acceptance-$phase-$stamp.json"
tmp="$(mktemp "$out_dir/.operational-acceptance.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
targets_file_present=0
if [[ -f "$targets_file" ]]; then
  targets_file_present=1
fi

{
  printf '{\n'
  printf '  "schema": "hexroute.operational-acceptance.v1",\n'
  printf '  "phase": "%s",\n' "$phase"
  printf '  "created_at": "%s",\n' "$created_at"
  printf '  "dry_run": %s,\n' "$(json_bool "$dry_run")"
  printf '  "overall": "%s",\n' "$overall"
  printf '  "targets_file_present": %s,\n' "$(json_bool "$targets_file_present")"
  printf '  "checks": [\n'
  for index in "${!checks[@]}"; do
    [[ "$index" == 0 ]] || printf ',\n'
    printf '    %s' "${checks[$index]}"
  done
  printf '\n  ],\n'
  printf '  "manual_checkpoints": [\n'
  for index in "${!manuals[@]}"; do
    [[ "$index" == 0 ]] || printf ',\n'
    printf '    %s' "${manuals[$index]}"
  done
  printf '\n  ]\n'
  printf '}\n'
} >"$tmp"

if [[ -n "${HEXROUTE_ACCEPTANCE_SECRET_CANARY:-}" ]] &&
  grep -Fq "$HEXROUTE_ACCEPTANCE_SECRET_CANARY" "$tmp"; then
  printf 'error: protected canary reached acceptance evidence\n' >&2
  exit 70
fi

mv "$tmp" "$evidence"
trap - EXIT

printf 'phase=%s overall=%s evidence=%s\n' "$phase" "$overall" "$evidence"
if [[ "$overall" == "pass" || "$overall" == "dry_run" ]]; then
  exit 0
fi
exit 1
