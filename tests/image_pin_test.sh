#!/usr/bin/env bash
# Every image reference in this repository names its content by digest, and a
# gate that pulls one survives a registry refusing to serve it.
#
# The fixtures below are the ways this measurement goes wrong rather than a
# tour of the rules: a reference the reader walks past because it misread which
# token holds the image, a fallback that silently runs different content, and a
# scan that reports a clean repository because it looked in the wrong place.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
status=0

# --- The repository itself -------------------------------------------------
if ! python3 tests/image_pins.py; then
  status=1
fi

work="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-image-pins.XXXXXX")"
trap 'rm -rf "$work"' EXIT

# A fixture repository the reader can walk: its own Makefile, because the name
# of the image built here is read from there and not from a list.
new_fixture() {
  local root="$1"
  mkdir -p "$root/tests" "$root/deploy/container"
  printf 'CONTAINER_IMAGE ?= fixture-ingest:contract\n' >"$root/Makefile"
}

refuses() {
  local root="$1" description="$2"
  if python3 tests/image_pins.py --root "$root" >/dev/null 2>&1; then
    printf 'the image gate accepted %s\n' "$description" >&2
    status=1
  fi
}

accepts() {
  local root="$1" description="$2"
  if ! python3 tests/image_pins.py --root "$root" >/dev/null 2>&1; then
    printf 'the image gate refused %s\n' "$description" >&2
    python3 tests/image_pins.py --root "$root" >&2 || true
    status=1
  fi
}

# --- What must pass --------------------------------------------------------
#
# A repository referencing only `scratch`, its own built image and a pinned
# digest. Written first: a gate that refuses everything proves nothing by
# refusing the fixtures below.
clean="$work/clean"
new_fixture "$clean"
cat >"$clean/Dockerfile" <<'FIXTURE'
ARG BUILDER_IMAGE=golang:1.25.12-alpine3.23@sha256:cc985ef6f9c3bf9ece7488129c9abe0a150388ccdfa428d886fc709dca0b230a
FROM ${BUILDER_IMAGE} AS builder
FROM scratch AS runtime
FIXTURE
cat >"$clean/deploy/container/compose.yaml" <<'FIXTURE'
services:
  api:
    image: ${FIXTURE_CONTAINER_IMAGE:-fixture-ingest:contract}
FIXTURE
cat >"$clean/tests/runtime_test.sh" <<'FIXTURE'
#!/usr/bin/env bash
image="${1:-fixture-ingest:contract}"
docker run --rm --name fixture "$image"
FIXTURE
accepts "$clean" 'a repository whose references are scratch, its own image and a pinned digest'

# --- An unpinned reference, in each place one can be written ---------------
#
# `arg` and `dockerfile` are separate places: a build argument naming a default
# image is read even when no FROM mentions it, which is how the one reference
# this repository already pinned is written.
for place in dockerfile arg compose script; do
  root="$work/unpinned-$place"
  new_fixture "$root"
  cp "$clean/Dockerfile" "$root/Dockerfile"
  cp "$clean/deploy/container/compose.yaml" "$root/deploy/container/compose.yaml"
  cp "$clean/tests/runtime_test.sh" "$root/tests/runtime_test.sh"
  case "$place" in
    dockerfile)
      printf 'FROM postgres:17-alpine AS fixture\n' >>"$root/Dockerfile" ;;
    arg)
      printf 'ARG OTHER_IMAGE=postgres:17-alpine\n' >>"$root/Dockerfile" ;;
    compose)
      printf '  db:\n    image: postgres:17-alpine\n' \
        >>"$root/deploy/container/compose.yaml" ;;
    script)
      printf 'docker run --rm postgres:17-alpine\n' \
        >>"$root/tests/runtime_test.sh" ;;
  esac
  refuses "$root" "an unpinned reference in a $place"
done

# The reference the repository actually carried before this change, refused by
# the gate that did not exist then.
parent="$work/parent"
new_fixture "$parent"
git show HEAD:tests/postgres_migrations_test.sh \
  >"$parent/tests/postgres_migrations_test.sh"
refuses "$parent" "the reference this repository carried before the change"

# --- Reading the wrong token ------------------------------------------------
#
# An option this reader does not know takes a value: its value is then read as
# the image and refused by name. That is the direction the list of options is
# arranged to rot in, and this fixture is what holds it that way — the opposite
# arrangement walks past an image and passes a bare tag in silence.
hidden="$work/hidden-by-option"
new_fixture "$hidden"
cat >"$hidden/tests/runtime_test.sh" <<'FIXTURE'
#!/usr/bin/env bash
docker run --rm --some-option-this-gate-does-not-know postgres:17-alpine
FIXTURE
refuses "$hidden" 'an unpinned image standing where an unknown option would put its value'

# The command a container runs is not an image. `docker exec ... psql` and
# `docker logs --tail 50` are what an earlier reader refused, twenty-nine
# times, which made its output unreadable.
after="$work/container-command"
new_fixture "$after"
cat >"$after/tests/runtime_test.sh" <<'FIXTURE'
#!/usr/bin/env bash
image="${1:-fixture-ingest:contract}"
docker run --rm "$image" psql --username postgres --command 'select 1'
docker exec fixture pg_isready --username postgres
docker logs --tail 50 fixture
docker inspect --format '{{.State.Status}}' fixture
FIXTURE
accepts "$after" 'the command a container runs, which is not an image'

# --- A gate that pulls -----------------------------------------------------
#
# One source means one registry can stop the gate, which is the failure this
# change was opened for.
single="$work/single-source"
new_fixture "$single"
# A reference the reader does find, so that the only thing left to refuse is
# the single source. Without it the fixture is refused for finding nothing at
# all, and a reader that never notices an unresolved image passes — which is
# what a mutation found.
printf 'FROM scratch AS runtime\n' >"$single/Dockerfile"
cat >"$single/tests/pull_test.sh" <<'FIXTURE'
#!/usr/bin/env bash
image_sources=(
  "docker.io/library/postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24"
)
image=""
docker pull --quiet "${image_sources[0]}"
docker run --rm "$image"
FIXTURE
refuses "$single" 'a pulling gate that names one registry'

# Two sources, two digests: a fallback to different content, which is the one
# thing a digest is for.
divergent="$work/divergent-digests"
new_fixture "$divergent"
printf 'FROM scratch AS runtime\n' >"$divergent/Dockerfile"
cat >"$divergent/tests/pull_test.sh" <<'FIXTURE'
#!/usr/bin/env bash
image_sources=(
  "docker.io/library/postgres@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24"
  "mirror.gcr.io/library/postgres@sha256:0000000000000000000000000000000000000000000000000000000000000000"
)
image=""
docker run --rm "$image"
FIXTURE
refuses "$divergent" 'sources whose digests differ, so the fallback runs other content'

# Pulling an image the file does not name, with no sources declared at all.
undeclared="$work/undeclared"
new_fixture "$undeclared"
printf 'FROM scratch AS runtime\n' >"$undeclared/Dockerfile"
cat >"$undeclared/tests/pull_test.sh" <<'FIXTURE'
#!/usr/bin/env bash
image=""
image="$(printf 'postgres:17-alpine')"
docker run --rm "$image"
FIXTURE
refuses "$undeclared" 'a gate pulling an image it never names'

# --- A clean answer from the wrong place ------------------------------------
#
# A scan finding nothing has not found a clean repository. It has found that it
# was looking somewhere else.
nowhere="$work/nowhere"
new_fixture "$nowhere"
refuses "$nowhere" 'a repository where it found no reference at all'

# Without the Makefile there is nothing to tell a locally built image from one
# that must be refused, and guessing is how an unpinned reference passes.
#
# Its one reference is `scratch`, which every rule accepts, so the only thing
# that can refuse this fixture is the Makefile saying nothing. A reader that
# guesses the built name instead of reading it passes here.
nameless="$work/nameless"
mkdir -p "$nameless/tests"
printf 'all:\n\t@true\n' >"$nameless/Makefile"
printf 'FROM scratch AS runtime\n' >"$nameless/Dockerfile"
refuses "$nameless" 'a repository that does not say which image it builds'

# A digest that is not one. Sixty-four hexadecimal characters is the digest; a
# prefix of them is a reference to nothing, and reading `@sha256:` as enough
# accepts it.
truncated="$work/truncated-digest"
new_fixture "$truncated"
cat >"$truncated/tests/pull_test.sh" <<'FIXTURE'
#!/usr/bin/env bash
image_sources=(
  "docker.io/library/postgres@sha256:b0f9560a"
  "mirror.gcr.io/library/postgres@sha256:b0f9560a"
)
image=""
docker run --rm "$image"
FIXTURE
refuses "$truncated" 'a digest truncated to a prefix of itself'

if [ "$status" -ne 0 ]; then
  exit 1
fi
printf 'ok: every image reference is pinned and a pulling gate survives one registry\n'
