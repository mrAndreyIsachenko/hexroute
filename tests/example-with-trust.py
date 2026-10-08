#!/usr/bin/env python3
"""Write a copy of an example configuration with trust material supplied.

The examples record the shape a decoder accepts, and the repository must not
hold trust material: internal/repositoryguard refuses a tracked artifact whose
`pinned_public_key` or `signer_fingerprint` is anything but empty. So the
examples carry those keys empty, and anything that needs a configuration the
daemon will load fills them first.

A key of the right shape and no provenance: thirty-two fixed bytes, so the
result is a configuration the daemon will load and nothing anyone holds. The
same convention, for the same reason, is stated in
tests/install_reduction_guard_test.sh, which builds whole configurations rather
than filling one.

Usage: example-with-trust.py SOURCE DESTINATION
"""
import base64
import hashlib
import json
import sys


def main(source: str, destination: str) -> int:
    document = json.load(open(source))
    control = document.get("policy_control")
    if isinstance(control, dict):
        key = bytes(range(32))
        control["pinned_public_key"] = base64.b64encode(key).decode().rstrip("=")
        control["signer_fingerprint"] = hashlib.sha256(key).hexdigest()
    else:
        print(f"{source} carries no policy_control to fill", file=sys.stderr)
    with open(destination, "w") as handle:
        json.dump(document, handle, indent=2)
        handle.write("\n")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        raise SystemExit(2)
    raise SystemExit(main(sys.argv[1], sys.argv[2]))
