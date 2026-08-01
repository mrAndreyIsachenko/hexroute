#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
version="${1:-}"
output_directory="${2:-$repo_root/dist}"

case "$version" in
  0 | 0.*[!0-9.]* | *..* | .* | *.)
    printf 'error: version must be exact numeric semantic version\n' >&2
    exit 64
    ;;
esac
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' || {
  printf 'error: version must be exact numeric semantic version\n' >&2
  exit 64
}

mkdir -p "$output_directory"
output_directory="$(CDPATH= cd -- "$output_directory" && pwd)"
archive="$output_directory/hexroute-ingress-observer_${version}_linux_amd64.tar.gz"
checksum="$archive.sha256"
test ! -e "$archive" && test ! -L "$archive" &&
  test ! -e "$checksum" && test ! -L "$checksum" || {
  printf 'error: release output already exists\n' >&2
  exit 1
}

temporary="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-observer-release.XXXXXX")"
temporary="$(CDPATH= cd -- "$temporary" && pwd)"
trap 'rm -rf "$temporary"' EXIT HUP INT TERM
binary="$temporary/hexroute-ingress-observer"

(
  cd "$repo_root"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -buildvcs=false \
    -ldflags='-s -w -buildid=' \
    -o "$binary" \
    ./cmd/hexroute-ingress-observer
  go run ./cmd/hexroute-package-observer "$binary" "$archive"
)

digest="$(shasum -a 256 "$archive" | awk '{print $1}')"
printf '%s  %s\n' "$digest" "$(basename "$archive")" >"$checksum"
chmod 0644 "$checksum"
printf 'archive=%s\nchecksum=%s\nsha256=%s\n' "$archive" "$checksum" "$digest"
