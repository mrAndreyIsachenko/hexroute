#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# The sentinel watches the root daemon from outside it. Two things would make
# that pointless and both are easy to get wrong: sharing the daemon's paths, so
# one failure takes the watcher's evidence with it, and installing the
# committed example configuration, which names a reserved address and would
# report a broken data path forever.

label="com.hexroute.observe.sentinel"
plist="deploy/macos/$label.plist"
installer="scripts/macos/observe-sentinel-launchd.sh"
example="deploy/macos/sentinel-observe.example.json"

[[ -f "$plist" ]]
[[ -x "$installer" ]]
[[ -f "$example" ]]

if command -v plutil >/dev/null 2>&1; then
  plutil -lint "$plist" >/dev/null
fi

# Resident, not scheduled. A watcher that stays down after its first exit stops
# watching exactly when something has gone wrong.
grep -q '<key>KeepAlive</key>' "$plist" ||
  { echo "the sentinel would stay down after its first exit" >&2; exit 1; }
if grep -q '<key>StartInterval</key>' "$plist"; then
  echo "the sentinel is resident and must not also be scheduled" >&2
  exit 1
fi

# Disjoint from the root daemon it watches. A watcher sharing a directory with
# the thing it watches loses its own evidence to the same failure.
if grep -q 'observe-root' "$plist"; then
  echo "the sentinel writes under the root daemon's own paths" >&2
  exit 1
fi
grep -q 'observe-sentinel' "$plist" ||
  { echo "the sentinel has no paths of its own" >&2; exit 1; }

# It reads the root daemon's heartbeat and nothing else of its. The path is in
# the configuration, not the job.
if grep -qE 'socks5|proxy|heartbeat' "$plist"; then
  echo "the plist carries what belongs in a private configuration" >&2
  exit 1
fi

# Nothing live is committed with it.
if grep -qE '([0-9]{1,3}\.){3}[0-9]{1,3}' "$plist"; then
  echo "the sentinel plist carries an address" >&2
  exit 1
fi

# The committed example is synthetic and must stay that way: a reserved
# documentation address and an invalid server name, so it cannot resolve to
# anything real if it were ever installed by mistake.
grep -q '203\.0\.113\.' "$example" ||
  { echo "the example endpoint is not a reserved documentation address" >&2; exit 1; }
grep -q '\.invalid"' "$example" ||
  { echo "the example server name is not in the invalid TLD" >&2; exit 1; }
grep -q '"mode": "observe-only"' "$example" ||
  { echo "the example does not declare observe-only" >&2; exit 1; }

# And the installer refuses it, because a valid synthetic configuration passes
# --check and would then report a broken data path forever.
grep -q 'sentinel-observe.example.json' "$installer" ||
  { echo "the installer does not refuse the committed example" >&2; exit 1; }
grep -q -- '--check' "$installer" ||
  { echo "the installer does not validate the configuration first" >&2; exit 1; }

# Uninstall removes what install put there and nothing else. The private
# configuration is the operator's and the logs are what it was installed for.
grep -q 'rm -f "$PLIST_DEST"' "$installer"
grep -q 'rm -f "$BIN_DIR/hexroute-sentinel"' "$installer"
if grep -qE 'rm -rf|rm -f "\$CONFIG_DIR|rm -f "\$LOG_DIR' "$installer"; then
  echo "uninstall removes more than it installed" >&2
  exit 1
fi

# Disjoint from Twilight and AdGuard by name as well as by path.
for foreign in twilight adguard; do
  if grep -qi "$foreign" "$plist"; then
    echo "the sentinel plist names $foreign" >&2
    exit 1
  fi
done

printf 'ok: the sentinel is resident, disjoint from what it watches, and refuses the synthetic example\n'
