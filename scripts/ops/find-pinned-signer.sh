#!/usr/bin/env bash
# Print the signer public key whose fingerprint this host pins.
#
# By fingerprint, never by the name of a directory. There has been more than one
# signer on this machine and the one with the expected name was not the one the
# host trusts: a runbook that named a path sent an operator to the wrong key,
# and a wrong key is discovered at the moment of signing, after user presence.
#
# Prints one path on success and nothing on failure, so it can be assigned
# directly and a failure leaves the variable empty rather than wrong.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
config="${1:-$repo_root/private/root-observe.json}"

[ -f "$config" ] || { printf 'no configuration at %s\n' "$config" >&2; exit 1; }

/usr/bin/python3 - "$config" "$HOME/Library/Application Support/Hexroute" "$repo_root/.local" <<'PY'
import base64, glob, hashlib, json, os, sys

config, *roots = sys.argv[1:]
try:
    pinned = json.load(open(config))["policy_control"]["signer_fingerprint"]
except Exception as error:
    sys.stderr.write("this configuration pins no signer: %s\n" % error)
    raise SystemExit(1)


def fingerprints(raw):
    """Every reading of the file that could be a key, so a format change here
    does not read as a missing key."""
    yield hashlib.sha256(raw.encode()).hexdigest()
    for decode in (base64.b64decode, base64.urlsafe_b64decode):
        try:
            yield hashlib.sha256(decode(raw + "==")).hexdigest()
        except Exception:
            continue


found = []
for root in roots:
    for path in glob.glob(os.path.join(root, "*", "public-key")):
        try:
            if os.path.getsize(path) > 200:
                continue
            raw = open(path).read().strip()
        except Exception:
            continue
        if pinned in fingerprints(raw):
            found.append(path)

if not found:
    sys.stderr.write("no key on this machine matches the fingerprint this host pins\n")
    raise SystemExit(1)
if len(set(found)) > 1:
    sys.stderr.write("more than one key matches; refusing to choose:\n  %s\n"
                     % "\n  ".join(sorted(set(found))))
    raise SystemExit(1)
print(found[0])
PY
