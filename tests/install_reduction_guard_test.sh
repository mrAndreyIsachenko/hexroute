#!/usr/bin/env bash
# An installer must not replace a live configuration with less than it has.
#
# On 2026-09-10 both daemons were installed from stale working copies. Neither
# carried `policy_control` and root's had lost `pritunl_service_label`, so two
# runtimes holding a signed generation 4 became runtimes holding none. Every
# check in the path reported success, because a configuration that drops an
# authority is valid — it is simply a different configuration.
#
# This gate checks behaviour rather than wording: the check is run against
# fixtures and judged by exit status, and the installers are checked for asking
# before they touch anything.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# macOS ships bash 3.2, where `set -e` does not fire on a failing `[[ ]]`. An
# assertion written as a bare conditional is a comment there: it evaluates,
# reports false, and the script keeps going to its success message. So every
# assertion below says what to do when it fails.
fail() {
  printf 'install-reduction-guard: %s\n' "$1" >&2
  exit 1
}

cd "$ROOT"
go build -o "$WORK/hexrouted" ./cmd/hexrouted
go build -o "$WORK/hexroute-userd" ./cmd/hexroute-userd

# The fixtures are the shipped examples plus the settings that carry authority,
# with placeholders where real material would be. They have to be
# configurations the daemon accepts on their own: validity is judged first, and
# a file that is also malformed should say that rather than say it is reduced.
/usr/bin/python3 - "$ROOT" "$WORK" <<'FIXTURES'
import base64, hashlib, json, os, sys
root, work = sys.argv[1], sys.argv[2]

# A key of the right shape and no provenance: thirty-two fixed bytes, so the
# fixture is a configuration the daemon will load and nothing anyone holds.
# A real pinned key in a fixture would be the leak this repository guards.
key = bytes(range(32))
pinned = base64.b64encode(key).decode().rstrip("=")
fingerprint = hashlib.sha256(key).hexdigest()
# Digests of fixed text: the right shape, and nothing this machine produced.
static = hashlib.sha256(b"fixture static envelope").hexdigest()
compiler = hashlib.sha256(b"fixture compiler").hexdigest()

def control(domain):
    return {
        "schema": "hexroute.policy-daemon-static.v1",
        "installed": {
            "domain": domain,
            "minimum_policy_schema": 1,
            "maximum_policy_schema": 1,
            "current_policy_schema": 1,
            "static_sha256": static,
            "trusted_compiler_sha256": [compiler],
        },
        "pinned_public_key": pinned,
        "signer_fingerprint": fingerprint,
    }

def write(name, value):
    json.dump(value, open(os.path.join(work, name), "w"), indent=2)

example = json.load(open(os.path.join(root, "deploy/macos/root-observe.example.json")))
installed = dict(example)
installed["policy_control"] = control("root")
installed["pritunl_service_label"] = "com.example.service"
write("installed.json", installed)

# The stale working copy: valid on its own, carrying neither authority.
write("stale.json", example)

richer = dict(installed)
richer["expected_sing_box_parent_pid"] = 1
write("richer.json", richer)

changed = dict(installed)
changed["observation_interval_seconds"] = 30
write("changed.json", changed)

user_example = json.load(open(os.path.join(root, "deploy/macos/user-observe.example.json")))
user_installed = dict(user_example)
user_installed["policy_control"] = control("user")
write("user-installed.json", user_installed)
write("user-stale.json", user_example)
FIXTURES

check() {
  local candidate="$1"
  local status=0
  "$WORK/hexrouted" --check \
    --config "$candidate" --installed "$WORK/installed.json" \
    >"$WORK/out" 2>/dev/null || status=$?
  printf '%s' "$status"
}

# A candidate that drops settings is refused, with its own exit rather than the
# one a malformed file gets.
[[ "$(check "$WORK/stale.json")" == "3" ]] || fail "'$(check '$WORK/stale.json')' == '3'"
grep -q 'would lose policy_control$' "$WORK/out"
grep -q 'would lose policy_control.pinned_public_key$' "$WORK/out"
grep -q 'would lose pritunl_service_label$' "$WORK/out"

# What must not be refused.
[[ "$(check "$WORK/installed.json")" == "0" ]] || fail "'$(check '$WORK/installed.json')' == '0'"
[[ "$(check "$WORK/richer.json")" == "0" ]] || fail "'$(check '$WORK/richer.json')' == '0'"
[[ "$(check "$WORK/changed.json")" == "0" ]] || fail "'$(check '$WORK/changed.json')' == '0'"

# A malformed candidate is a different refusal from a reduced one.
printf '{' > "$WORK/broken.json"
[[ "$(check "$WORK/broken.json")" == "2" ]] || fail "'$(check '$WORK/broken.json')' == '2'"

# Both daemons answer the same question. An installer that guards one domain
# and not the other guards nothing.
user_status=0
"$WORK/hexroute-userd" --check \
  --config "$WORK/user-stale.json" --installed "$WORK/user-installed.json" \
  >/dev/null 2>&1 || user_status=$?
[[ "$user_status" == "3" ]] || fail "'$user_status' == '3'"

for installer in \
  "$ROOT/scripts/macos/observe-root-launchd.sh" \
  "$ROOT/scripts/macos/observe-user-launchd.sh"; do
  grep -q -- '--installed' "$installer"
  grep -q 'HEXROUTE_ALLOW_REDUCED_CONFIG' "$installer"
  grep -q '\.replaced' "$installer"

  # The status must come from the command. Under `if ! cmd` the shell reports
  # the negation, so a refusal to compare and a refusal on the comparison would
  # arrive as the same 1.
  grep -q 'if ! "$binary" --check' "$installer" &&
    fail "$installer takes the status from the negation"

  # Refusing after the binary is replaced leaves a half-installed daemon whose
  # loaded job is still the old one, so the comparison has to come first.
  #
  # The line to compare against is the one that replaces the binary, not the
  # first `install` in the file: these scripts create directories in helpers
  # that run earlier, and creating a directory replaces nothing. Written the
  # loose way this assertion was false and said nothing, because a bare
  # conditional cannot fail under the bash these gates run with.
  guard_line="$(grep -n -- '--installed' "$installer" | head -n 1 | cut -d: -f1)"
  binary_line="$(grep -n '"\$binary" "\$BIN_DIR/' "$installer" | head -n 1 | cut -d: -f1)"
  [[ -n "$binary_line" ]] || fail "$installer never replaces the binary"
  [[ "$guard_line" -lt "$binary_line" ]] ||
    fail "$installer asks at line $guard_line, after replacing the binary at $binary_line"

  # And the copy that is kept must be taken before the replacement lands.
  keep_line="$(grep -n '\.replaced"' "$installer" | head -n 1 | cut -d: -f1)"
  [[ "$keep_line" -gt "$guard_line" ]] || fail "[[ '$keep_line' -gt '$guard_line' ]]"
done

printf 'install-reduction-guard: ok\n'
