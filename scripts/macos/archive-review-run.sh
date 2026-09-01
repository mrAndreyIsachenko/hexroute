#!/bin/sh
# The weekly review, wrapped so that launchd sees a schedule and not a verdict.
#
# Two things this wrapper is for.
#
# It records the attempt before making it. A review that died mid-run would
# otherwise leave nothing at all, and "no report this week" and "no attempt
# this week" are different failures with different causes — one is the archive,
# the other is the schedule.
#
# It always exits zero. The review's own exit code is honest and is written
# down here; handing it to launchd would turn a week the archive could not be
# read into something that looks like a crashing job.
set -u

# The installed paths are the defaults. They are overridable so this wrapper
# can be exercised somewhere other than the machine it is installed on: a
# schedule whose failure behaviour was never run is a schedule whose failure
# behaviour is a hope.
ROOT="${HEXROUTE_REVIEW_ROOT:-/Library/Application Support/Hexroute/observe-root}"
BIN="${HEXROUTE_REVIEW_BIN:-$ROOT/bin/hexroute-archive-report}"
ARCHIVE="${HEXROUTE_REVIEW_ARCHIVE:-$ROOT/state/event-archive}"
REPORTS="${HEXROUTE_REVIEW_REPORTS:-$ROOT/state/archive-reports}"
ATTEMPTS="${HEXROUTE_REVIEW_ATTEMPTS:-$ROOT/state/archive-review-attempts.log}"

started="$(/bin/date -u '+%Y-%m-%dT%H:%M:%SZ')"
printf '%s attempted\n' "$started" >>"$ATTEMPTS"

"$BIN" --archive "$ARCHIVE" --out "$REPORTS"
code=$?

finished="$(/bin/date -u '+%Y-%m-%dT%H:%M:%SZ')"
printf '%s finished exit=%d\n' "$finished" "$code" >>"$ATTEMPTS"

# Keep the attempt log bounded. It is a record of whether the schedule ran,
# not of what it found, so the last two hundred lines answer every question it
# can answer.
if [ -f "$ATTEMPTS" ]; then
  /usr/bin/tail -n 200 "$ATTEMPTS" >"$ATTEMPTS.trimmed" 2>/dev/null &&
    /bin/mv "$ATTEMPTS.trimmed" "$ATTEMPTS"
fi

exit 0
