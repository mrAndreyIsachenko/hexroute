#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$HOME/Library/Application Support/Hexroute/policy-qualification"
INSTALLED_BINARY="$ROOT_DIR/bin/hexroute-policy-qualification"
ROOT_SOCKET="/var/run/hexroute-observe/hexrouted.sock"
USER_SOCKET="$HOME/Library/Application Support/Hexroute/observe-user/state/userd.sock"

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

status_json=""
status_error=""

if [[ -x "$INSTALLED_BINARY" ]]; then
  error_file="$(/usr/bin/mktemp /private/tmp/hexroute-qualification-summary.XXXXXX)"
  if status_json="$("$INSTALLED_BINARY" status \
    --root "$ROOT_DIR" \
    --root-socket "$ROOT_SOCKET" \
    --user-socket "$USER_SOCKET" \
    --interval 60s \
    --max-gap 180s 2>"$error_file")"; then
    :
  else
    status_error="$(/bin/cat "$error_file")"
    status_json=""
  fi
  /bin/rm -f "$error_file"
else
  status_error="policy qualification binary is not installed"
fi

STATUS_JSON="$status_json" STATUS_ERROR="$status_error" ROOT_DIR="$ROOT_DIR" /usr/bin/python3 - <<'PY'
import json
import os
from datetime import datetime

def parse_time(value):
    value = value.replace("Z", "+00:00")
    if "." in value:
        prefix, suffix = value.split(".", 1)
        digits = []
        rest_index = 0
        for index, char in enumerate(suffix):
            if char.isdigit():
                digits.append(char)
                rest_index = index + 1
                continue
            rest_index = index
            break
        fraction = ("".join(digits) + "000000")[:6]
        value = prefix + "." + fraction + suffix[rest_index:]
    return datetime.fromisoformat(value)

def fallback_status(root, error):
    with open(os.path.join(root, "state", "current.json"), "r", encoding="utf-8") as handle:
        state = json.load(handle)
    binding = state["binding"]
    chain = os.path.join(root, "sessions", "session-" + binding["session_id"], "qualification.jsonl")
    progress = {
        "record_count": 0,
        "eligible_seconds": 0,
        "sleep_wake_cycles": 0,
        "reboot_observed": False,
        "invalid_signature": False,
        "selector_conflict": False,
        "stale_generation": False,
        "cross_domain_crash": False,
        "safety_comparisons": 0,
        "failed_evidence": False,
        "complete": False,
    }
    eligible_ns = 0
    with open(chain, "r", encoding="utf-8") as handle:
        for line in handle:
            line = line.strip()
            if not line:
                continue
            try:
                record = json.loads(line)
            except json.JSONDecodeError:
                continue
            progress["record_count"] += 1
            result = record.get("result")
            kind = record.get("kind")
            if result == "failed":
                progress["failed_evidence"] = True
            if result != "passed":
                continue
            if kind == "eligible_window":
                window = record["eligible_window"]
                eligible_ns += int(window["ended_monotonic_ns"]) - int(window["started_monotonic_ns"])
            elif kind == "sleep_wake":
                progress["sleep_wake_cycles"] += 1
            elif kind == "reboot":
                progress["reboot_observed"] = True
            elif kind in ("invalid_signature", "selector_conflict", "stale_generation", "cross_domain_crash"):
                progress[kind] = True
            elif kind == "safety_comparison":
                progress["safety_comparisons"] += 1
    progress["eligible_seconds"] = int(eligible_ns / 1_000_000_000)
    progress["complete"] = (
        progress["eligible_seconds"] >= 72 * 60 * 60
        and progress["sleep_wake_cycles"] >= 2
        and progress["reboot_observed"]
        and progress["invalid_signature"]
        and progress["selector_conflict"]
        and progress["stale_generation"]
        and progress["cross_domain_crash"]
        and progress["safety_comparisons"] >= 1
        and not progress["failed_evidence"]
    )
    return {
        "schema": "hexroute.policy-qualification-status.v1",
        "lifecycle": state["lifecycle"],
        "reason": state["reason"],
        "binding": binding,
        "progress": progress,
        "_fallback_error": error.strip(),
    }

if os.environ.get("STATUS_JSON"):
    status = json.loads(os.environ["STATUS_JSON"])
else:
    status = fallback_status(os.environ["ROOT_DIR"], os.environ.get("STATUS_ERROR", ""))

progress = status.get("progress", {})

required_seconds = 72 * 60 * 60
eligible_seconds = int(progress.get("eligible_seconds") or 0)
remaining_seconds = max(0, required_seconds - eligible_seconds)

def fmt(seconds):
    hours, rem = divmod(int(seconds), 3600)
    minutes, seconds = divmod(rem, 60)
    return f"{hours:02d}:{minutes:02d}:{seconds:02d}"

checks = [
    ("eligible time", eligible_seconds >= required_seconds,
     f"{fmt(eligible_seconds)} / 72:00:00",
     "wait for contiguous eligible samples"),
    ("sleep/wake cycles", int(progress.get("sleep_wake_cycles") or 0) >= 2,
     f"{int(progress.get('sleep_wake_cycles') or 0)} / 2",
     "run make policy-qualification-arm-sleep immediately before closing the lid"),
    ("reboot", bool(progress.get("reboot_observed")),
     "passed" if progress.get("reboot_observed") else "missing",
     "perform one ordinary reboot while the agent is installed"),
    ("invalid_signature fault", bool(progress.get("invalid_signature")),
     "passed" if progress.get("invalid_signature") else "missing",
     "run make policy-qualification-faults"),
    ("selector_conflict fault", bool(progress.get("selector_conflict")),
     "passed" if progress.get("selector_conflict") else "missing",
     "run make policy-qualification-faults"),
    ("stale_generation fault", bool(progress.get("stale_generation")),
     "passed" if progress.get("stale_generation") else "missing",
     "run make policy-qualification-faults"),
    ("cross_domain_crash fault", bool(progress.get("cross_domain_crash")),
     "passed" if progress.get("cross_domain_crash") else "missing",
     "run make policy-qualification-faults"),
    ("safety comparison", int(progress.get("safety_comparisons") or 0) >= 1,
     str(int(progress.get("safety_comparisons") or 0)),
     "wait for normal sampling; do not restart"),
]

print(f"lifecycle={status.get('lifecycle')} reason={status.get('reason')} complete={str(progress.get('complete')).lower()}")
print(f"session={status.get('binding', {}).get('session_id')}")
print(f"records={progress.get('record_count')} failed_evidence={str(progress.get('failed_evidence')).lower()}")
if status.get("_fallback_error"):
    print(f"warning: authoritative status failed; read-only fallback used: {status.get('_fallback_error')}")
print()

for name, passed, value, _ in checks:
    marker = "ok" if passed else "missing"
    print(f"{marker}: {name}: {value}")

print()
if status.get("lifecycle") == "invalid" or progress.get("failed_evidence"):
    print("next: inspect make logs-policy-qualification before restarting the session")
elif progress.get("complete"):
    print("next: qualification gate is complete")
else:
    missing = [check for check in checks if not check[1]]
    if remaining_seconds > 0:
        print(f"remaining eligible time: {fmt(remaining_seconds)}")
    seen = set()
    actions = []
    for _, _, _, action in missing:
        if action not in seen:
            seen.add(action)
            actions.append(action)
    print("next actions:")
    for action in actions:
        print(f"- {action}")
PY
