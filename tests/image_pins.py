#!/usr/bin/env python3
"""Hold every container image this repository pulls to a pinned digest.

The repository already pinned the image it ships — the Dockerfile's builder
carries a digest and `tests/container_contract_test.sh` holds it there. The
image the schema gate pulls was a bare tag held by nobody, so two runs a week
apart could apply the same migrations to two different servers and report
success both times, with nothing in either record saying which server it
measured. Pinning that one reference and writing its digest into a test would
leave the next reference unpinned, which is the shape of the defect. This reads
the references instead.

A reference is acceptable for one of three reasons, and the reasons are not
interchangeable:

  * it names its content by digest — the rule;
  * it is `scratch`, which is not an image and is never pulled;
  * it names an image this repository builds, read from the Makefile's
    CONTAINER_IMAGE rather than from a list written beside this gate, because a
    list beside a gate is the thing that does not get updated.

A shell script that pulls is held to more: it must take its image from a
declared list of at least two sources, every one of them pinned to the same
digest. One source means one registry can stop the gate, which is what happened
on 2026-10-09. Different digests would mean falling back to different content,
which is the one thing a digest exists to prevent.

Finding the image in a docker command needs to know which options take a
value, and that knowledge is a list, so the list is arranged to rot in the
harmless direction: an option this gate does not recognise is read as taking no
value, and its value is then read as the image. A missing entry therefore makes
the gate refuse a token and name it. The opposite arrangement — assuming every
option takes a value — would skip past an image and pass a bare tag in silence.

The first attempt at this scanned every token of every docker command, on the
argument that over-refusing is safe. It refused twenty-nine things, among them
`psql`, `pg_isready` and `export`, because `docker exec` is followed by the
command to run inside the container. A gate nobody can read the output of is
not a safe gate, so only the three subcommands that take an image are scanned,
and only up to the image: what follows it belongs to the container.
"""

from __future__ import annotations

import argparse
import os
import re
import sys

DIGEST = r"@sha256:[0-9a-f]{64}"
# A reference docker would accept: optional registry and path, a name, an
# optional tag, an optional digest.
REFERENCE = re.compile(
    r"^[a-z0-9][a-z0-9._-]*(?:/[a-z0-9][a-z0-9._-]*)*"
    r"(?::[A-Za-z0-9_][A-Za-z0-9._-]*)?"
    r"(?:" + DIGEST + r")?$"
)
PINNED = re.compile(DIGEST)
DIGEST_LITERAL = re.compile(r"@(sha256:[0-9a-f]{64})")

# The three subcommands that take an image. `docker exec`, `inspect`, `logs`
# and `rm` take a container, and scanning those is what produced the twenty-nine
# refusals described above.
IMAGE_COMMANDS = {"run", "create", "pull"}

# Options whose value is a separate token. An option missing from this set has
# its value read as the image, which the gate then refuses by name — see the
# docstring on which direction this list is arranged to rot in.
VALUE_FLAGS = {
    "--name", "--env", "-e", "--env-file", "--publish", "-p", "--volume", "-v",
    "--mount", "--tmpfs", "--network", "--net", "--user", "-u", "--workdir",
    "-w", "--entrypoint", "--label", "-l", "--add-host", "--cap-add",
    "--cap-drop", "--security-opt", "--health-cmd", "--health-interval",
    "--health-retries", "--restart", "--pids-limit", "--memory", "-m",
    "--cpus", "--platform", "--pull", "--build-arg", "--tag", "-t", "--file",
    "-f", "--format", "--ulimit", "--sysctl", "--device", "--dns", "--hostname",
    "-h", "--log-driver", "--log-opt", "--stop-signal", "--stop-timeout",
    "--shm-size", "--gpus", "--attach", "-a",
}
SEPARATORS = re.compile(r"\|\||&&|[|;]")


class Problem(Exception):
    pass


def read(path: str) -> str:
    with open(path, encoding="utf-8") as handle:
        return handle.read()


def built_image(root: str) -> str:
    """The image this repository builds, taken from the Makefile."""
    makefile = os.path.join(root, "Makefile")
    for line in read(makefile).splitlines():
        match = re.match(r"^CONTAINER_IMAGE\s*\??=\s*(\S+)", line)
        if match:
            return match.group(1)
    raise Problem(f"{makefile}: CONTAINER_IMAGE is not set, so the gate cannot "
                  "tell a locally built image from one it must refuse")


def join_continuations(text: str) -> list[tuple[int, str]]:
    """Logical lines with their starting line number."""
    joined: list[tuple[int, str]] = []
    pending = ""
    start = 0
    for number, line in enumerate(text.splitlines(), start=1):
        stripped = line.rstrip()
        if not pending:
            start = number
        if stripped.endswith("\\"):
            pending += stripped[:-1] + " "
            continue
        joined.append((start, pending + stripped))
        pending = ""
    if pending:
        joined.append((start, pending))
    return joined


HEREDOC = re.compile(r"<<-?\s*[\"']?(\w+)[\"']?")


def without_heredocs(text: str) -> str:
    """The script's own commands, with the text it merely writes blanked out.

    A heredoc body is data this script hands to a file or a program; the docker
    commands inside one are not commands this script runs. This gate's own
    harness writes fixture Dockerfiles and fixture scripts that way, and
    without this it refuses its own fixtures — which is the first thing it did.

    Lines are blanked rather than removed so that a reference's line number
    still points at the line it is written on.
    """
    lines = text.splitlines()
    kept: list[str] = []
    terminator: str | None = None
    for line in lines:
        if terminator is not None:
            kept.append("")
            if line.strip() == terminator:
                terminator = None
            continue
        kept.append(line)
        match = HEREDOC.search(line)
        if match:
            terminator = match.group(1)
    return "\n".join(kept)


def unquote(token: str) -> str:
    if len(token) >= 2 and token[0] == token[-1] and token[0] in "'\"":
        return token[1:-1]
    return token


def resolve(reference: str, arguments: dict[str, str]) -> str | None:
    """Resolve one level of expansion, or None when nothing names a value."""
    match = re.fullmatch(r"\$\{(\w+):-([^}]*)\}", reference)
    if match:
        return match.group(2)
    match = re.fullmatch(r"\$\{(\w+)\}|\$(\w+)", reference)
    if match:
        name = match.group(1) or match.group(2)
        return arguments.get(name)
    return reference


def dockerfile_references(path: str, text: str) -> list[tuple[int, str]]:
    arguments = {}
    for _, line in join_continuations(text):
        match = re.match(r"^ARG\s+(\w+)=(\S+)", line)
        if match:
            arguments[match.group(1)] = match.group(2)

    found = []
    for number, line in join_continuations(text):
        match = re.match(r"^FROM\s+(\S+)", line)
        if match:
            reference = match.group(1)
            resolved = resolve(reference, arguments)
            if resolved is None:
                raise Problem(f"{path}:{number}: FROM {reference} names no "
                              "value this gate can read")
            found.append((number, resolved))
        match = re.match(r"^ARG\s+(\w*IMAGE\w*)=(\S+)", line)
        if match:
            found.append((number, match.group(2)))
    return found


def yaml_references(path: str, text: str) -> list[tuple[int, str]]:
    found = []
    for number, line in enumerate(text.splitlines(), start=1):
        match = re.match(r"^\s*image:\s*(\S+)", line)
        if match:
            reference = unquote(match.group(1))
            resolved = resolve(reference, {})
            if resolved is None:
                raise Problem(f"{path}:{number}: image: {reference} names no "
                              "value this gate can read")
            found.append((number, resolved))
    return found


def docker_commands(text: str) -> list[tuple[int, str, str]]:
    """Image-taking docker commands, as (line, subcommand, command)."""
    commands = []
    for number, line in join_continuations(text):
        for segment in SEPARATORS.split(line):
            segment = segment.strip()
            words = segment.split()
            if len(words) < 2 or unquote(words[0]) != "docker":
                continue
            subcommand = unquote(words[1])
            if subcommand == "image" and len(words) > 2:
                subcommand = unquote(words[2])
            if subcommand in IMAGE_COMMANDS:
                commands.append((number, subcommand, segment))
    return commands


def command_image(command: str) -> str | None:
    """The first positional argument of a docker command: its image.

    Everything after it is the command the container runs, which is why the
    walk stops rather than reading on.
    """
    words = [unquote(word) for word in command.split()]
    index = 1
    while index < len(words) and words[index] in IMAGE_COMMANDS | {"image"}:
        index += 1
    while index < len(words):
        token = words[index]
        if token.startswith("-"):
            index += 2 if token in VALUE_FLAGS else 1
            continue
        if token[:1] in "<>&":
            index += 1
            continue
        return token
    return None


def shell_assignments(text: str) -> dict[str, str]:
    """One level of plain shell assignment, `${1:-default}` included."""
    assignments: dict[str, str] = {}
    for _, line in join_continuations(text):
        match = re.match(r'^\s*(?:local\s+)?(\w+)=(\S*)\s*$', line)
        if match:
            assignments[match.group(1)] = unquote(match.group(2))
    return assignments


def resolve_shell(token: str, assignments: dict[str, str]) -> str | None:
    """The reference a token names, or None when nothing in the file says."""
    seen = set()
    current = token
    while True:
        match = re.fullmatch(r'\$\{(\w+):-([^}]*)\}', current)
        if match:
            current = match.group(2)
            continue
        match = re.fullmatch(r'\$\{?(\w+)\}?', current)
        if match:
            name = match.group(1)
            if name in seen or name not in assignments:
                return None
            seen.add(name)
            current = assignments[name]
            continue
        match = re.fullmatch(r'\$\{?(\d+)(?::-([^}]*))?\}?', current)
        if match:
            return match.group(2)
        if not current or "$" in current:
            return None
        return current


def declared_sources(path: str, text: str) -> list[str]:
    """The pinned references a pulling script names."""
    body = re.search(r"^\w*(?:sources|images)\w*=\(\s*$(.*?)^\)\s*$",
                     text, re.MULTILINE | re.DOTALL)
    if body is None:
        raise Problem(
            f"{path}: pulls an image it does not name in the file and declares "
            "no list of sources for it; one registry refusing should not stop "
            "a gate")
    return [unquote(line.strip()) for line in body.group(1).splitlines()
            if line.strip()]


def acceptable(reference: str, built: str) -> str | None:
    if reference == "scratch":
        return None
    if reference == built:
        return None
    if PINNED.search(reference):
        return None
    return "names no digest"


def scanned(root: str) -> list[str]:
    paths = []
    for directory, subdirectories, names in os.walk(root):
        subdirectories[:] = [
            name for name in sorted(subdirectories)
            if name not in {".git", "archive", "testdata", "vendor", "bin"}
        ]
        for name in sorted(names):
            path = os.path.join(directory, name)
            relative = os.path.relpath(path, root)
            if name == "Dockerfile" or name.startswith("Dockerfile."):
                paths.append(relative)
            elif name.endswith((".yml", ".yaml")) and (
                    relative.startswith(".github/")
                    or relative.startswith("deploy/")):
                paths.append(relative)
            elif name.endswith(".sh") and (
                    relative.startswith("tests/")
                    or relative.startswith("scripts/")):
                paths.append(relative)
    return paths


def shell_findings(path: str, text: str) -> tuple[list[tuple[int, str]], bool]:
    """References a shell script names, and whether it pulls one it does not.

    A script whose image resolves to the repository's own built artifact is not
    pulling from a registry and is held to nothing further. A script whose
    image the file does not name — assigned inside a function, read from a
    list — is pulling, and must say which registries can serve it.
    """
    assignments = shell_assignments(text)
    found: list[tuple[int, str]] = []
    unresolved = False
    for number, _, command in docker_commands(text):
        token = command_image(command)
        if token is None:
            continue
        reference = resolve_shell(token, assignments)
        if reference is None:
            unresolved = True
            continue
        found.append((number, reference))
    return found, unresolved


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", default=os.path.join(
        os.path.dirname(os.path.abspath(__file__)), os.pardir))
    parser.add_argument("--list", action="store_true",
                        help="print every reference found and where")
    arguments = parser.parse_args()
    root = os.path.abspath(arguments.root)

    failures: list[str] = []
    listing: list[str] = []
    references = 0
    pinned = 0
    pulling_scripts = 0

    try:
        built = built_image(root)
    except Problem as problem:
        print(f"refused: {problem}", file=sys.stderr)
        return 1

    for relative in scanned(root):
        path = os.path.join(root, relative)
        text = read(path)
        found: list[tuple[int, str]] = []
        unresolved = False
        body = text
        try:
            if os.path.basename(relative).startswith("Dockerfile"):
                found = dockerfile_references(relative, text)
            elif relative.endswith((".yml", ".yaml")):
                found = yaml_references(relative, text)
            else:
                body = without_heredocs(text)
                found, unresolved = shell_findings(relative, body)
        except Problem as problem:
            failures.append(str(problem))
            continue

        if unresolved:
            pulling_scripts += 1
            try:
                sources = declared_sources(relative, body)
            except Problem as problem:
                failures.append(str(problem))
                sources = []
            if sources and len(sources) < 2:
                failures.append(
                    f"{relative}: names {len(sources)} source for its image; "
                    "a gate that pulls must survive one registry refusing")
            digests = set()
            for source in sources:
                found.append((0, source))
                match = DIGEST_LITERAL.search(source)
                if match is None:
                    continue
                digests.add(match.group(1))
            if len(digests) > 1:
                failures.append(
                    f"{relative}: sources name {len(digests)} different "
                    "digests, so a fallback would run different content: "
                    + ", ".join(sorted(digests)))

        for number, reference in found:
            references += 1
            where = f"{relative}:{number}" if number else relative
            listing.append(f"{where}\t{reference}")
            if PINNED.search(reference):
                pinned += 1
            complaint = acceptable(reference, built)
            if complaint is not None:
                failures.append(f"{where}: {reference} {complaint}")

    if arguments.list:
        for line in listing:
            print(line)

    if failures:
        print("refused:", file=sys.stderr)
        for failure in sorted(failures):
            print(f"  {failure}", file=sys.stderr)
        return 1

    # A scan that finds nothing has not found that the repository is clean; it
    # has found that it was looking in the wrong place. This repository builds
    # and pulls images, so zero references is a defect in the reader.
    #
    # This is reported after the failures and not before them: a script that
    # pulls an image it never names produces a complaint and no reference, and
    # reporting the empty scan first said the reader was lost when it had in
    # fact found the thing it was looking for.
    if not references:
        print("refused: no image reference found anywhere under "
              f"{root}; the reader is looking in the wrong place",
              file=sys.stderr)
        return 1

    print(f"ok: {references} image references, {pinned} pinned by digest, "
          f"{pulling_scripts} script(s) pulling from a registry")
    return 0


if __name__ == "__main__":
    sys.exit(main())
