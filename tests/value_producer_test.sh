#!/usr/bin/env bash
# Every published value has something that emits it, or is written down.
#
# The measurement is easy to get wrong, and the fixtures below are the three
# ways it went wrong before it was written: a validity switch naming every
# value, a declaring file that also returns its values, and a bare constant
# name matching another package's constant of the same name.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"
status=0

# --- The repository itself -------------------------------------------------
if ! python3 tests/value_producers.py \
  --hold internal/connectivity,internal/connectivityreduce \
  --search internal,cmd; then
  status=1
fi

# --- The gate's own fixtures ------------------------------------------------
work="$(mktemp -d "${TMPDIR:-/tmp}/hexroute-producers.XXXXXX")"
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/vocab" "$work/user" "$work/other"

# A vocabulary this gate has never heard of, discovered from the code, with one
# value returned by its own declaring file and one emitted from another package.
cat >"$work/vocab/vocab.go" <<'SOURCE'
package vocab

type Shade string

const (
	ShadeFirst  Shade = "first"
	ShadeSecond Shade = "second"
	ShadeThird  Shade = "third"
)

// A validity switch names every value and emits none of them. Counting this
// made a first attempt report every value produced.
func (shade Shade) valid() bool {
	switch shade {
	case ShadeFirst, ShadeSecond, ShadeThird:
		return true
	default:
		return false
	}
}

// And the declaring file returns one of them, which is why it cannot simply be
// skipped.
func First() Shade {
	return ShadeFirst
}
SOURCE

cat >"$work/user/user.go" <<'SOURCE'
package user

import "example/vocab"

func Second() vocab.Shade {
	return vocab.ShadeSecond
}
SOURCE

# Another package declaring the same bare name, emitting its own. Counting a
# bare name here made `expired` look produced when nothing referenced it.
cat >"$work/other/other.go" <<'SOURCE'
package other

type Shade string

const ShadeThird Shade = "third"

func Third() Shade {
	return ShadeThird
}
SOURCE

printf 'Shade.third nothing in the held package emits it\n' >"$work/recorded"

run() {
  python3 tests/value_producers.py --hold "$work/vocab" --search "$1" \
    --recorded "$2" >/dev/null 2>&1
}

# first is returned by the declaring file, second from another package, and
# third only by a package that declares its own constant of that name.
if ! run "$work" "$work/recorded"; then
  printf 'the producer gate refused a fixture where every value is accounted for\n' >&2
  status=1
fi

# A value emitted by nothing and not written down.
printf '# nothing written down\n' >"$work/empty"
if run "$work" "$work/empty"; then
  printf 'the producer gate accepted a value emitted by nothing and not written down\n' >&2
  status=1
fi

# A value written down and emitted: the record must not outlive what it says.
# Shade.third is written down too, so the only thing left to refuse is the stale
# entry — a first version of this fixture refused for the other reason and would
# have passed with the check removed, which a mutation found.
printf 'Shade.third nothing in the held package emits it\nShade.first it is returned by its own declaring file\n' >"$work/stale"
if run "$work" "$work/stale"; then
  printf 'the producer gate accepted a written-down value that is emitted\n' >&2
  status=1
fi

# A written-down value no vocabulary declares.
printf 'Shade.third fine\nShade.absent nothing declares this\n' >"$work/absent"
if run "$work" "$work/absent"; then
  printf 'the producer gate accepted a written-down value nothing declares\n' >&2
  status=1
fi

# A fixture is not a producer: the same emitter, in a file named as one.
mkdir -p "$work/fixture-only/vocab"
cp "$work/vocab/vocab.go" "$work/fixture-only/vocab/vocab.go"
python3 - "$work/fixture-only/vocab/vocab.go" <<'FIXUP'
import sys
path = sys.argv[1]
source = open(path).read()
# take the declaring file's own emitter away, so the only one left is a fixture
open(path, "w").write(source.replace('''func First() Shade {
	return ShadeFirst
}''', ""))
FIXUP
mkdir -p "$work/fixture-only/user"
cat >"$work/fixture-only/user/fixture.go" <<'SOURCE'
package user

import "example/vocab"

func All() []vocab.Shade {
	return []vocab.Shade{vocab.ShadeFirst, vocab.ShadeSecond, vocab.ShadeThird}
}
SOURCE
printf 'Shade.second x\nShade.third x\n' >"$work/fixture-recorded"
if python3 tests/value_producers.py --hold "$work/fixture-only/vocab" \
  --search "$work/fixture-only" --recorded "$work/fixture-recorded" >/dev/null 2>&1; then
  printf 'the producer gate counted a fixture as a producer\n' >&2
  status=1
fi

# A whole vocabulary nothing emits is a broken read, not an unused vocabulary.
mkdir -p "$work/orphan"
cat >"$work/orphan/orphan.go" <<'SOURCE'
package orphan

type Lonely string

const (
	LonelyOne Lonely = "one"
	LonelyTwo Lonely = "two"
)
SOURCE
printf 'Lonely.one x\nLonely.two x\n' >"$work/orphan-recorded"
if python3 tests/value_producers.py --hold "$work/orphan" --search "$work/orphan" \
  --recorded "$work/orphan-recorded" >/dev/null 2>&1; then
  printf 'the producer gate accepted a vocabulary nothing emits at all\n' >&2
  status=1
fi

if [ "$status" -ne 0 ]; then
  exit 1
fi
printf 'ok: the producer gate holds every value and refuses each way of missing one\n'
