#!/usr/bin/env bash
set -euo pipefail

# The weekly review runs unattended, which makes two failures likely and both
# silent: a review that dies leaving no trace it was attempted, and a review
# whose failure launchd reads as a crashing job. Both are exercised here rather
# than reasoned about.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

label="com.hexroute.observe.archive-review"
plist="deploy/macos/$label.plist"
installer="scripts/macos/archive-review-launchd.sh"
wrapper="scripts/macos/archive-review-run.sh"

[[ -f "$plist" ]]
[[ -x "$installer" ]]
[[ -x "$wrapper" ]]

if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$plist" >/dev/null
fi

grep -q "<string>$label</string>" "$plist"

# It owns its own interval. A calendar entry can fire twice across a clock
# change; an interval cannot.
grep -q '<key>StartInterval</key>' "$plist"
grep -q '<integer>604800</integer>' "$plist" ||
  { echo "the review is not scheduled weekly" >&2; exit 1; }
if grep -q '<key>StartCalendarInterval</key>' "$plist"; then
  echo "a calendar entry can fire twice across a clock change" >&2
  exit 1
fi

# A review that ends is a review that finished.
if grep -q '<key>KeepAlive</key>' "$plist"; then
  echo "the review would be restarted on exit" >&2
  exit 1
fi

# The plist runs the wrapper, not the command. The command's exit code is
# honest, and handing it to launchd is what the wrapper exists to prevent.
grep -q 'hexroute-archive-review-run.sh' "$plist" ||
  { echo "the schedule does not run through the wrapper" >&2; exit 1; }

# Disjoint from Twilight, AdGuard and the policy runtime.
for foreign in twilight adguard com.hexroute.policy; do
  if grep -qi "$foreign" "$plist"; then
    echo "the review plist names $foreign" >&2
    exit 1
  fi
done

# No session identity, endpoint or credential is committed with it.
if grep -qE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' "$plist"; then
  echo "the review plist carries an identity every install would share" >&2
  exit 1
fi

# The wrapper is exercised, not read.
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT

attempts="$scratch/attempts.log"
reports="$scratch/reports"

# A review whose command cannot run at all still records that it was attempted
# and still exits zero.
# Captured with `|| status=$?` rather than after the call: under `set -e` a
# non-zero wrapper would end this script before any line that checked for one,
# and the check would pass for the whole life of the file without ever running.
missing_bin="$scratch/not-installed"
status=0
HEXROUTE_REVIEW_BIN="$missing_bin" \
  HEXROUTE_REVIEW_ARCHIVE="$scratch/archive" \
  HEXROUTE_REVIEW_REPORTS="$reports" \
  HEXROUTE_REVIEW_ATTEMPTS="$attempts" \
  "$wrapper" >/dev/null 2>&1 || status=$?
if [[ "$status" -ne 0 ]]; then
  echo "a failed review exited $status; launchd would read that as a crash" >&2
  exit 1
fi
[[ -f "$attempts" ]] ||
  { echo "a failed review left no record that it was attempted" >&2; exit 1; }
grep -q 'attempted' "$attempts" ||
  { echo "the attempt was not recorded before the review ran" >&2; exit 1; }
grep -q 'finished exit=' "$attempts" ||
  { echo "the review's own exit code was not written down" >&2; exit 1; }

# The attempt is recorded before the outcome, so a review killed mid-run still
# leaves evidence it started.
attempted_line="$(grep -n 'attempted' "$attempts" | head -1 | cut -d: -f1)"
finished_line="$(grep -n 'finished' "$attempts" | head -1 | cut -d: -f1)"
if [[ "$attempted_line" -ge "$finished_line" ]]; then
  echo "the attempt is not recorded before the review is made" >&2
  exit 1
fi

# A real review over a real archive succeeds and writes one report.
if go build -o "$scratch/hexroute-archive-report" ./cmd/hexroute-archive-report; then
  mkdir -p "$scratch/archive"
  HEXROUTE_REVIEW_BIN="$scratch/hexroute-archive-report" \
    HEXROUTE_REVIEW_ARCHIVE="$scratch/archive" \
    HEXROUTE_REVIEW_REPORTS="$reports" \
    HEXROUTE_REVIEW_ATTEMPTS="$attempts" \
    "$wrapper" >/dev/null 2>&1
  written="$(ls -1 "$reports" 2>/dev/null | wc -l | tr -d ' ')"
  [[ "$written" == "1" ]] ||
    { echo "a successful review wrote $written reports, want 1" >&2; exit 1; }
  grep -q 'finished exit=0' "$attempts" ||
    { echo "a successful review did not record a zero exit" >&2; exit 1; }
fi

# Uninstall is scoped to what install put there. Reports and attempts are the
# record of the reviews that did run, and removing a schedule is not a reason
# to lose them.
grep -q 'rm -f "$PLIST_DEST"' "$installer"
grep -q 'rm -f "$BIN_DIR/hexroute-archive-report"' "$installer"
grep -q 'rm -f "$BIN_DIR/hexroute-archive-review-run.sh"' "$installer"
if grep -qE 'rm -rf|rm -f "\$REPORT_DIR|rm -f "\$STATE_DIR' "$installer"; then
  echo "uninstall removes more than it installed" >&2
  exit 1
fi

printf 'ok: the weekly archive review is scheduled, records its attempts and never reports a crash\n'
