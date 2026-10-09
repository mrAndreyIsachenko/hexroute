# Pin the images the gates pull

## Why

The schema gate cannot start. `make postgres-test` failed twice in fourteen
minutes — on `main` at 2026-10-09T20:52Z (run 37989922820) and on PR 175 at
21:06Z (run 37991360218) — both with the same first line:

```
docker: Error response from daemon: toomanyrequests: You have reached your
unauthenticated pull rate limit.
```

It is pulling `postgres:17-alpine` from Docker Hub anonymously. That budget is
counted per source address and GitHub-hosted runners share addresses with every
other customer on the host, so the gate's ability to run depends on strangers.
`main` is red for this reason and nothing in the repository caused it.

The second half is older and quieter. The repository makes two image references
that reach a registry. The one that ships is pinned by digest and held by
`tests/container_contract_test.sh`:

```
ARG GO_BUILDER_IMAGE=golang:1.25.12-alpine3.23@sha256:cc985ef6...
```

The one the schema gate pulls is a bare tag held by nothing. So the migrations
run against whatever `17-alpine` pointed at that morning, and two runs a week
apart can apply the same migrations to different servers and both report
success. `repository-and-deployment-boundary` already requires immutable image
content "rather than a mutable tag alone" — but it says that about selecting a
release for deployment, and the image a gate pulls fell outside it.

## What Changes

- The postgres image is referenced by the digest of its image index,
  `sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24`,
  and the migrations gate says which registry served it.
- The gate names more than one source for that digest and takes the first that
  answers. `mirror.gcr.io/library/postgres`, `public.ecr.aws/docker/library/postgres`
  and Docker Hub were measured serving byte-identical manifests and the same
  index digest, so they are interchangeable transports. A digest makes a mirror
  unable to serve different content — only to refuse — which is why a fallback
  is an availability decision and not a trust decision.
- A new gate holds the rule rather than the instance: every image reference in
  this repository that reaches a registry is pinned by digest, and the gate
  refuses an unpinned one wherever it appears — a Dockerfile, a test script, a
  compose file, a workflow.
- The migrations gate is held to naming at least two sources, all carrying the
  same digest, so trimming it back to one registry is refused.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `repository-and-deployment-boundary` — the immutable-image requirement
  currently speaks about deployment selecting a release. It gains the images the
  repository's own gates pull: pinned by digest, and not dependent on a single
  registry being willing to answer.

## Impact

- `tests/postgres_migrations_test.sh` — the image reference, the pull, and the
  line saying which registry answered.
- `tests/image_pin_test.sh` (new) and its registration in the Makefile's
  `shell-test` list.
- No runtime code, no daemon, no installed configuration. Nothing is installed
  and no runtime is restarted.
- Dependabot watches `gomod`, `github-actions` and `terraform`, not `docker`, so
  both digests are updated by hand. This change does not alter that; it records
  it.
