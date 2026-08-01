#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-observer-test.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
mkdir "$temporary/first" "$temporary/second" "$temporary/extract"

"$repo_root/scripts/build-ingress-observer-release.sh" 0.0.0 "$temporary/first" >/dev/null
"$repo_root/scripts/build-ingress-observer-release.sh" 0.0.0 "$temporary/second" >/dev/null

archive_name="hexroute-ingress-observer_0.0.0_linux_amd64.tar.gz"
first="$temporary/first/$archive_name"
second="$temporary/second/$archive_name"
cmp "$first" "$second"

listing="$(tar -tzf "$first")"
test "$listing" = "hexroute-ingress-observer"
tar -xzf "$first" -C "$temporary/extract"
test -x "$temporary/extract/hexroute-ingress-observer"
file "$temporary/extract/hexroute-ingress-observer" | grep -q 'ELF 64-bit.*x86-64'

first_digest="$(awk '{print $1}' "$first.sha256")"
second_digest="$(awk '{print $1}' "$second.sha256")"
test "$first_digest" = "$second_digest"
test "$first_digest" = "$(shasum -a 256 "$first" | awk '{print $1}')"

printf 'ingress observer release tests passed\n'
