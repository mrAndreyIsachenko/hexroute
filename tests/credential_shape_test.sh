#!/usr/bin/env bash
# A credential-shaped literal must not reach a tracked file.
#
# This gate exists because one did. internal/objectstore signs requests to
# object storage, and its first test fixtures used the provider's documented
# example credentials verbatim. Every local gate passed: secretguard and
# repositoryguard read structured artifacts — JSON and YAML — and a literal
# in a Go test was never in their scope. The push was refused by the forge
# instead, which means the working tree and the local history already held
# the string by the time anything objected.
#
# Shape is what is checked here, not provenance. A documented example key and
# a live one are indistinguishable to a reader, to a scanner, and to anyone
# who copies the line; a fixture that cannot be mistaken for a credential
# costs nothing and removes the question.

set -euo pipefail
cd "$(dirname "$0")/.."

failed=0
report() {
	printf 'credential-shaped literal (%s):\n' "$1" >&2
	printf '%s\n' "$2" | sed 's/^/  /' >&2
	failed=1
}

tracked=$(git ls-files)

# Provider access key identifiers. The prefixes are assigned by AWS and are
# what the forge's own scanner matches; ASIA is the temporary-credential form.
hits=$(printf '%s\n' "$tracked" | grep -vFx "tests/credential_shape_test.sh" |
	xargs grep -nE '\b(AKIA|ASIA)[A-Z0-9]{16}\b' 2>/dev/null || true)
[ -n "$hits" ] && report "provider access key identifier" "$hits"

# The provider's documented example secret. It is published, which is exactly
# why it spreads: it looks like a real secret in every context but the page it
# came from.
hits=$(printf '%s\n' "$tracked" | grep -vFx "tests/credential_shape_test.sh" |
	xargs grep -nF 'wJalrXUtnFEMI/K7MDENG' 2>/dev/null || true)
[ -n "$hits" ] && report "documented example secret" "$hits"

# Private key material of any kind. Nothing in this repository has a reason to
# carry one, including as a fixture: a test that needs a key generates it.
hits=$(printf '%s\n' "$tracked" | grep -vFx "tests/credential_shape_test.sh" |
	xargs grep -nE 'BEGIN [A-Z ]*PRIVATE KEY' 2>/dev/null || true)
[ -n "$hits" ] && report "private key block" "$hits"

if [ "$failed" -ne 0 ]; then
	printf '\nUse a fixture that cannot be read as a credential.\n' >&2
	exit 1
fi

printf 'ok: no tracked file carries a credential-shaped literal\n'
