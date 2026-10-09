# Tasks

## 1. Read what the repository references

- [x] 1.1 List every image reference in the repository that reaches a registry
      and say, for each, whether it is pinned and what holds it; verified by
      reading the files, not by trusting the survey.

      Two references reach a registry. `Dockerfile:1` pins the Go builder by
      digest and `tests/container_contract_test.sh` holds it there.
      `tests/postgres_migrations_test.sh:21` named `postgres:17-alpine` and
      nothing held it. `FROM scratch` is not an image, and
      `hexroute-ingest:contract` in the compose contract and the runtime test
      is built here. The reader's `--list` prints eleven references, and all
      eleven were read by hand against the files before the gate was trusted.
- [x] 1.2 Confirm which of them CI pulls, from the workflow's jobs.

      `.github/workflows/check.yml` runs three jobs: `make check` on macOS,
      the Go gate on Linux, and `make postgres-test`. `container-build` and
      `container-test` are not among them, so the only image CI pulls is the
      postgres one — which is the one that was unpinned.

## 2. Pin and mirror the schema gate's image

- [x] 2.1 Replace the tag reference with the index digest and name three
      sources for it, Docker Hub first; verified by the gate running.

      The index digest, not a platform's: the index has sixteen entries and
      CI is `linux/amd64` while the development Mac is `linux/arm64`.
- [x] 2.2 Pull before running the container, trying each source in turn, and
      report which one served it.

      Measured both ways. With no local copy: `postgres image pulled from
      docker.io/library/postgres`, gate exit 0. With the first source replaced
      by an address that refuses the connection: `postgres image pulled from
      mirror.gcr.io/library/postgres`, gate exit 0 — the fallback runs and the
      migrations pass through it.
- [x] 2.3 Report every registry's refusal when none serves the digest, not the
      last one only.

      With all three sources pointing at addresses that refuse, the gate
      exits 1 and prints three lines, one per registry, each naming the
      registry and what it said.
- [x] 2.4 Skip the pull when the digest is already present locally.

      With the image on disk the gate prints `postgres image already
      present, served by docker.io/library/postgres` and makes no request.
- [x] 2.5 Run `make postgres-test` and confirm it passes against the pinned
      digest; verified by its exit status.

      Exit 0, three times: against a local copy, against a fresh pull, and
      against the fallback.

## 3. Hold the rule

- [x] 3.1 Write `tests/image_pins.py`: every image reference carries a digest,
      is `scratch`, or names an image this repository builds, with the built
      name taken from the Makefile.

      `ok: 11 image references, 5 pinned by digest, 1 script(s) pulling from
      a registry`.
- [x] 3.2 Hold the pulling script to naming at least two sources, all carrying
      the same digest.

      And the digest is written out at each source rather than shared through
      a variable, so the rule has something it can catch.
- [x] 3.3 Refuse an unpinned reference naming the file, the line and the
      reference.

      For example `Dockerfile:4: postgres:17-alpine names no digest`.
- [x] 3.4 Register the gate in the Makefile's `shell-test` list.

      Beside `tests/container_contract_test.sh`, which holds the other pin.

## 4. Prove the gate refuses

- [x] 4.1 Fixtures for each refusal: an unpinned `FROM`, an unpinned `image:`,
      an unpinned `docker run`, a single-source pulling script, and sources
      carrying different digests.

      Thirteen fixtures, and each was read for *why* it was refused rather
      than *that* it was. Three were refused for the wrong reason at first —
      three pulling fixtures were refused for containing no reference at all,
      which hid the rule they were written for — and each now carries a
      reference the reader does find, so only its own rule can refuse it. Also
      added beyond the list: an unpinned `ARG`, a digest truncated to a prefix
      of itself, an unpinned image standing where an unknown option would put
      its value, and a repository that does not say which image it builds.
- [x] 4.2 A fixture that must pass: a reference to the repository's own built
      image, and `scratch`.

      Two: a repository whose references are `scratch`, its own built image
      and a pinned digest; and a script whose `docker run` is followed by the
      command the container runs, with `docker exec`, `docker logs` and
      `docker inspect` beside it. The second exists because the first version
      of the reader refused `psql`, `pg_isready`, `export` and `sed` — twenty-
      nine refusals in all.
- [x] 4.3 Prove it fails before the fix — run the new gate against the parent
      commit's `tests/postgres_migrations_test.sh`; verified by exit status.

      The fixture takes the file from `git show HEAD:` and the gate exits 1
      with `tests/postgres_migrations_test.sh:17: postgres:17-alpine names no
      digest`. It is a fixture rather than a note, so it keeps proving it.
- [x] 4.4 Mutation run over the reader, each survivor closed or recorded.

      Fourteen mutations, none invalid. Eleven killed on the first run and
      three survived: a reader that never notices an unresolved image, one that
      guesses the built name instead of reading the Makefile, and one that
      reads `@sha256:` without the sixty-four characters. All three survived
      because a fixture was refused for a reason other than its own. After
      isolating them, 14 killed, 0 survived.

## 5. Gates

- [x] 5.1 `make check`; verified by its exit status.

      Exit 0.
- [x] 5.2 `make postgres-test`; verified by its exit status.

      Exit 0.
- [x] 5.3 Report any gate that did not run and why.

      `make container-build` and `make container-test` did not run: they
      build and run the shipped image, which this change does not touch, and
      the Dockerfile's reference is held by `container_contract_test.sh` inside
      `make check`. The two terraform gates and the `policy-qualification`
      commands did not run and are not applicable — no Terraform file changed,
      nothing was installed and no runtime was restarted. Docker and the
      Terraform CLI are both present, so these were skipped for scope and not
      for availability.

## 6. Close

- [x] 6.1 Sync the delta into the baseline, validate and archive, with every
      task that can be ticked beforehand ticked; verified by the drift gate.

      One requirement replaced in `repository-and-deployment-boundary`,
      carrying the scenario it had plus five for the images a gate pulls. Every
      other task was ticked before archiving; this one describes the archive.
- [x] 6.2 Record what this leaves open: no mirror for the Dockerfile's builder,
      and nothing saying when either digest has gone stale.

      **The Dockerfile's builder has one registry.** It is pinned, and CI
      does not pull it, but a developer's `make container-build` depends on
      Docker Hub answering. Giving a build a fallback means the Makefile
      passing `--build-arg GO_BUILDER_IMAGE=` per attempt, which is a different
      change with its own failure to measure first.

      **Nothing says when a pin has gone stale.** Dependabot watches `gomod`,
      `github-actions` and `terraform`; its `docker` ecosystem reads
      Dockerfiles and would not see a digest in a shell script anyway. Both
      digests are updated by hand and nothing asks whether `17-alpine` has
      moved on.

      **A reference built from pieces is not read.** The reader resolves one
      level of shell assignment and `${VAR:-default}`. A reference assembled by
      string concatenation would come back unresolved, which makes the script
      answer to the several-sources rule — so it is refused rather than missed,
      but it is refused for a reason that does not describe it.

      **A heredoc is not scanned.** A script that writes another script
      containing an unpinned reference passes. Nothing does that today, and the
      written file is not in the repository.

      **Carried, untouched:** `readmodel/checkpoints` bounded by nothing;
      HEX-19; HEX-11; no path from a host event to an alert; nine of the ten
      causes a root cycle can name unseen on this machine; and the two ingress
      routes on each other's links.
