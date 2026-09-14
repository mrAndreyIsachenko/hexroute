#!/usr/bin/env bash
# Every absolute path a runbook tells an operator to use must exist here.
#
# Written because "I will read paths off the machine" is an intention and not a
# mechanism, and the intention failed twice in two days: a ceremony naming a
# signing key that was not the one this host pins, and a runbook naming
# `make build-handover`, a target nobody wrote. Both were found by somebody
# following the document, which is the worst moment to find them.
#
# It runs on the machine, not in CI: the paths are the machine's, and CI has no
# /Library/Application Support, so a gate passing there would prove nothing.
#
# Absent and unreadable are reported apart. Most of what these runbooks name
# lives under a root-only directory, so an unprivileged run cannot see it — and
# a script that called that missing would be making exactly the mistake this
# repository keeps paying for, where a reading nobody could take is printed as a
# reading that came back empty. Run it under sudo to get an answer rather than
# an abstention.
set -uo pipefail
cd "$(dirname "$0")/../.."

documents=("$@")
if [ ${#documents[@]} -eq 0 ]; then
  documents=(docs/macos/*.md)
fi

privileged=0
[ "$(id -u)" -eq 0 ] && privileged=1

missing=0 unverifiable=0 checked=0 skipped=0
for document in "${documents[@]}"; do
  [ -f "$document" ] || { echo "no such document: $document" >&2; exit 2; }
  while read -r path; do
    [ -n "$path" ] || continue
    case "$path" in
      *'<'*|*'>'*|*'$'*) skipped=$((skipped + 1)); continue ;;
    esac
    checked=$((checked + 1))
    if [ -e "$path" ]; then
      continue
    fi
    # Absent, or behind a directory this user may not enter? Walk up to the
    # first ancestor that exists and ask whether it can be listed.
    parent="$(dirname "$path")"
    while [ ! -e "$parent" ] && [ "$parent" != "/" ]; do parent="$(dirname "$parent")"; done
    if (( privileged == 0 )) && [ ! -x "$parent" ]; then
      echo "$document: cannot tell, $parent is closed to this user: $path"
      unverifiable=$((unverifiable + 1))
    else
      echo "$document: names a path that is not on this machine: $path"
      missing=$((missing + 1))
    fi
  done < <(awk '/^```/ {fenced = !fenced; next} fenced' "$document" |
    sed -e :a -e '/\\$/N; s/\\\n//; ta' |
    grep -oE "'/[^']+'|\"/[^\"]+\"|(^|[[:space:]])/[A-Za-z]([^[:space:]'\"]|\\\\ )*" |
    sed -E "s/^[[:space:]]+//; s/^['\"]//; s/['\"]$//; s/\\\\ / /g; s/[\\\\]+$//" |
    sort -u)
done

echo "runbook-paths: $checked checked, $skipped placeholders skipped, $missing missing, $unverifiable unverifiable"
if (( missing > 0 )); then exit 1; fi
if (( unverifiable > 0 )); then
  echo "re-run under sudo to turn abstentions into answers" >&2
  exit 3
fi
