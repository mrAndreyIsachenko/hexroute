# Design

## The digest is the index's, not a platform's

`postgres:17-alpine` is a multi-architecture index with sixteen entries. Pinning
one of those entries would pin an architecture, and the two machines that run
this gate do not share one: CI is `linux/amd64` and the development Mac is
`linux/arm64`. So the pin is the digest of the index —
`sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24` —
and the daemon selects the entry for the host it runs on. Measured 2026-10-09:
Docker Hub, `mirror.gcr.io/library/postgres` and
`public.ecr.aws/docker/library/postgres` return byte-identical manifests and
that same index digest.

## Docker Hub is asked first, on purpose

The obvious ordering is wrong. Putting a public mirror first would route past
whatever mirror the operator has configured in their own daemon — this
development Mac resolves Docker Hub through a private pull-through cache, and a
reference written as `mirror.gcr.io/...` bypasses it. The daemon's mirror
applies to Docker Hub references only.

So the order is Docker Hub, then `mirror.gcr.io`, then `public.ecr.aws`. An
operator's configured mirror keeps serving the gate, and the public mirrors are
reached for only when the origin refuses — which is the case this change exists
to survive. In CI the first request is spent on a registry that may answer with
`toomanyrequests`; that costs one request and the next source answers.

## The pull is separate from the run

Today the script pulls implicitly, inside `docker run`. A fallback cannot be
built around that: by the time `docker run` fails there is no way to retry
against another registry without also retrying the container. So the image is
pulled first, each source tried in turn, and the container is started from the
reference that answered. When every source refuses, the script reports what each
one said rather than the last one only — a gate that cannot start should name
the registry that would not serve it.

A digest already present locally is not pulled again. Under a rate limit even a
manifest request counts, and the gate asking for content it already has is how a
cached image still fails to start.

## The gate holds a rule, not a line

Pinning this one reference and writing the digest into a test would leave the
next reference unpinned, which is the shape of the defect being fixed: the
shipped image is pinned and held, and the gate's image was pinned by nobody.

`tests/image_pins.py` therefore scans the repository for image references and
requires each to fall into one of three categories, each for a stated reason:

- it carries `@sha256:` — the rule;
- it is `scratch` — not an image and never pulled;
- it names an image this repository builds itself, taken from the Makefile's
  `CONTAINER_IMAGE` rather than from a list written beside the gate.

The third category is derived rather than listed because a list is the thing
that is not updated. The reference `${HEXROUTE_CONTAINER_IMAGE:-hexroute-ingest:contract}`
in the compose contract resolves to that same built image, so its default is
read and checked, not skipped for being an expansion.

The gate is a Python reader with a shell harness, following
`tests/value_producers.py` and `tests/value_producer_test.sh`: the scanning is
regular-expression work over several file shapes, and a shell loop over a
variable holding several words does not split under `zsh`, which has produced a
false zero in this repository before.

## Two sources, one digest

The rule that a gate names more than one source is held by the same reader,
because it is the same subject. It requires the pulling script to name at least
two sources and every source to carry the same digest — a fallback to a
different digest would be a fallback to different content, which is the one
thing a digest pin exists to prevent.

So the digest is written out at each source rather than shared through a shell
variable. A variable would make "all sources carry the same digest" true by
construction, and a rule that cannot fail is not a rule; written out, it is a
claim with something to catch.

## What the reader reads, and what it learned by being wrong

Three decisions came from the reader refusing the wrong things.

It scanned every token of every docker command first, on the argument that
over-refusing is safe. It refused twenty-nine things — `psql`, `pg_isready`,
`export`, `sed` — because `docker exec` is followed by the command to run
inside the container and `docker logs --tail 50` is followed by a number. A
gate whose output nobody can read is not a safe gate. So it reads only the
three subcommands that take an image, and only up to the image: what follows
belongs to the container.

It then refused its own fixtures, because the harness writes fixture
Dockerfiles and fixture scripts as heredocs and the reader took their contents
for commands this repository runs. A heredoc body is data a script hands to a
file or a program, so the bodies are blanked — line for line, so a reference's
line number still points where it is written.

And it reported an empty scan before it reported failures, which said the
reader was lost when it had in fact found the defect: a script pulling an image
it never names produces a complaint and no reference. The empty-scan refusal
now comes last.

## What this does not do

The Dockerfile's builder is pinned by digest already and is pulled only by
`make container-build`, which CI does not run, so it keeps its single Docker Hub
reference. Giving `docker build` a registry fallback means passing
`--build-arg GO_BUILDER_IMAGE=` per attempt from the Makefile, which is a
different change with a different failure to measure first.

Dependabot watches `gomod`, `github-actions` and `terraform`. Neither digest is
updated by it, and the `docker` ecosystem would not see a digest written in a
shell script anyway. Nothing here tells the repository when a pin has gone
stale; that is recorded, not solved.
