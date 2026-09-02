#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

# tests/package_reachability_test.sh asks whether a package is in a binary.
# That question missed internal/telemetry for months: it sits in the cloud
# ingest binary's dependency graph, and nothing constructs an uploader, so the
# host has never uploaded anything and the package looked connected the whole
# time.
#
# This asks the narrower question that would have caught it. A constructor no
# production line calls is a capability that cannot run, whatever the specs say
# about it — which is the shape of every piece of work here that was finished
# to its last step and stopped.
#
# Entries below are accepted, each with what it is waiting for. Adding one is a
# decision someone writes down; that is the point.

python3 - <<'PY'
import os
import re
import sys

# What is waiting, and for what. A reason that names a task or a cutover can be
# checked against it later; "not yet" cannot.
ACCEPTED = {
    # Built, and blocked on object storage configured outside this repository.
    # add-private-incident-bundles tasks 2.1, 2.2 and 3.1.
    "internal/incidentbundle.NewCreator":
        "add-private-incident-bundles 2.1 — needs private object storage",
    "internal/incidentbundle.NewExpiryWorker":
        "add-private-incident-bundles 2.2 — needs private object storage",

    # Held behind their own cutovers, recorded in the unwired list.
    "internal/credentials.NewKeychainSource":
        "user-domain cutover; Twilight owns Keychain-backed Pritunl today",
    "internal/pritunlrescue.NewRequest":
        "OTP-watchdog cutover",

    # The host has never uploaded. add-local-event-archive covers durable local
    # retention instead; upload is a separate decision nobody has taken.
    "internal/telemetry.NewUploader":
        "no host binary uploads; the local archive covers retention",
    "internal/cloudingest.NewHTTPTransport":
        "the transport an uploading host would speak to",

    # A second entry point to something that is done another way. Signature
    # verification runs through signing.VerifyAuthenticity; the Verifier type
    # is an alternative API nothing has adopted.
    "internal/signing.NewVerifier":
        "verification goes through VerifyAuthenticity; this API is unadopted",

    # The sentinel's recovery machinery. sentinel.Run is its only entry point
    # and does not construct any of these, so none of it has ever run.
    "internal/sentinel.NewRecoveryPlanner":
        "sentinel.Run does not construct it; recovery has never run",
    "internal/sentinel.NewRecoveryController":
        "sentinel.Run does not construct it; recovery has never run",
    "internal/sentinel.NewMacOSRootRestarter":
        "sentinel.Run does not construct it; recovery has never run",

    # A fixture builder that lives outside _test.go so several test packages
    # can share it.
    "internal/reconciler.NewCrashFixtureSyntheticAdapter":
        "crash fixture shared across test packages",
}

production = {}
for root, directories, files in os.walk("."):
    directories[:] = [d for d in directories
                      if not d.startswith(".") and d != "vendor"]
    for name in files:
        if name.endswith(".go") and not name.endswith("_test.go"):
            path = os.path.join(root, name)
            production[path] = open(path).read()

declared = {}
for path, source in production.items():
    package = os.path.dirname(path).lstrip("./")
    for match in re.finditer(r"^func ((?:New|Open)\w*)\(", source, re.M):
        declared.setdefault(f"{package}.{match.group(1)}", match.group(1))

unbuilt = []
for identity, name in sorted(declared.items()):
    calls = 0
    for text in production.values():
        for hit in re.finditer(rf"\b{name}\(", text):
            start = text.rfind("\n", 0, hit.start()) + 1
            if text[start:].startswith("func "):
                continue
            calls += 1
            break
        if calls:
            break
    if calls == 0:
        unbuilt.append(identity)

status = 0
for identity in unbuilt:
    if identity in ACCEPTED:
        continue
    print(f"{identity} is constructed by nothing outside tests, so whatever it "
          f"does cannot happen.\n  Call it, or record what it is waiting for in "
          f"tests/unbuilt_capability_test.sh", file=sys.stderr)
    status = 1

for identity in sorted(ACCEPTED):
    if identity not in declared:
        print(f"{identity} is recorded as unbuilt and no longer exists",
              file=sys.stderr)
        status = 1
    elif identity not in unbuilt:
        print(f"{identity} is recorded as unbuilt and is now constructed; "
              f"remove it from the list", file=sys.stderr)
        status = 1

if status == 0:
    print(f"ok: every constructor is called, or is recorded with what it is "
          f"waiting for ({len(ACCEPTED)} waiting)")
sys.exit(status)
PY
