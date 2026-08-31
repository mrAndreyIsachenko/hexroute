#!/usr/bin/env bash
set -euo pipefail

# Measure a local model against defects whose answers are already known.
#
# An auditor is only worth running on code nobody has read if it can be shown
# to work on code somebody has. Every case here is a real defect from this
# repository, paired with the fix that closed it, so each one asks two
# questions: does the model see the defect, and does it clear the fix.
#
# The second question is the one that matters. A reviewer that reports a
# violation every time reports nothing, and costs more to read than it saves.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cases_dir="$repo_root/scripts/audit/cases"
model="${AUDIT_MODEL:-qwen3.5:9b}"

command -v ollama >/dev/null 2>&1 || {
  echo "ollama is not installed" >&2
  exit 1
}

# Two questions, because they find different things. Asking whether code
# satisfies a requirement finds a contradiction: the code says one thing and
# the requirement another. It cannot find an omission, because there is nothing
# to contradict — measured here, on a case where the whole subject was absent
# and the first question read it as satisfied.
#
# Asking which line implements the requirement makes an omission answerable.
# Most of what went wrong in this repository was omission, so the second
# question is the one that matters, and it took a measurement to learn that.
contradiction_question() {
  printf '\nQUESTION: Does this code satisfy the requirement? Answer in at most 4 lines:\n'
  printf 'VERDICT: HOLDS or VIOLATED\nWHY: one sentence\n'
}

omission_question() {
  printf '\nQUESTION: Which line of this code implements the requirement?\n'
  printf 'If nothing here does, the answer is NONE.\n'
  printf 'Answer in at most 3 lines:\n'
  printf 'VERDICT: HOLDS if a line implements it, VIOLATED if NONE\n'
  printf 'WHY: one sentence\n'
}

ask() {
  local requirement="$1" code="$2" question="$3"
  {
    printf 'You are auditing Go code against one written requirement.\n\n'
    printf 'REQUIREMENT (from the specification):\n'
    cat "$requirement"
    printf '\nCODE:\n'
    cat "$code"
    "$question"
  } | ollama run "$model" 2>/dev/null |
    tr -d '\r' | sed 's/\x1b\[[0-9;]*[A-Za-z]//g' |
    grep -oE 'VERDICT:[[:space:]]*(HOLDS|VIOLATED)' | tail -n 1 |
    grep -oE '(HOLDS|VIOLATED)' || printf 'UNREADABLE\n'
}

caught=0 missed=0 cleared=0 falsely=0 unreadable=0

# The control case was never defective, so both of its sides must read HOLDS.
score() {
  local name="$1" before="$2" after="$3"
  if [[ "$name" == *control* ]]; then
    [[ "$before" == HOLDS ]] && cleared=$((cleared + 1)) || falsely=$((falsely + 1))
    [[ "$after" == HOLDS ]] && cleared=$((cleared + 1)) || falsely=$((falsely + 1))
    return
  fi
  case "$before" in
    VIOLATED) caught=$((caught + 1)) ;;
    HOLDS) missed=$((missed + 1)) ;;
    *) unreadable=$((unreadable + 1)) ;;
  esac
  case "$after" in
    HOLDS) cleared=$((cleared + 1)) ;;
    VIOLATED) falsely=$((falsely + 1)) ;;
    *) unreadable=$((unreadable + 1)) ;;
  esac
}
printf '%-34s %-13s %-10s %-10s\n' CASE QUESTION BEFORE AFTER
printf '%-34s %-13s %-10s %-10s\n' ---- -------- ------ -----

for directory in "$cases_dir"/*/; do
  name="$(basename "$directory")"
  [[ -f "$directory/requirement.txt" ]] || continue

  for shape in contradiction omission; do
    before="$(ask "$directory/requirement.txt" "$directory/before.txt" "${shape}_question")"
    after="$(ask "$directory/requirement.txt" "$directory/after.txt" "${shape}_question")"
    printf '%-34s %-13s %-10s %-10s\n' "$name" "$shape" "$before" "$after"
    score "$name" "$before" "$after"
  done
  continue

done

printf '\nmodel %s\n' "$model"
printf 'defects seen        %d\n' "$caught"
printf 'defects missed      %d\n' "$missed"
printf 'fixes cleared       %d\n' "$cleared"
printf 'fixes falsely flagged %d\n' "$falsely"
printf 'unreadable answers  %d\n' "$unreadable"
printf '\nA run that flags everything is worth nothing however many defects it\n'
printf 'sees: read the two right-hand columns together, not the first one alone.\n'
