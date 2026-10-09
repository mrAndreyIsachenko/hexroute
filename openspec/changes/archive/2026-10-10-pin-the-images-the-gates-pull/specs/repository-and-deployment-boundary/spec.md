## MODIFIED Requirements

### Requirement: Immutable non-root cloud image

Cloud API, worker and migrator modes SHALL run from reviewed immutable image
content as non-root with a read-only root filesystem and explicit writable paths.

Every image reference in this repository that reaches a registry SHALL name its
content by digest, wherever the reference appears: a Dockerfile, a test script,
a compose file or a workflow. This holds for the images the repository's own
gates pull, not only for the image it ships. A gate that pulls a mutable tag can
apply the same migrations to two different servers a week apart and report
success both times, and nothing in the record would say which server either run
measured. A gate SHALL refuse an unpinned reference.

A gate that pulls an image SHALL NOT depend on a single registry being willing
to answer. It SHALL name more than one source for the digest it wants and take
the first that serves it, and it SHALL say which one did. Measured
2026-10-09, `make postgres-test` failed twice in fourteen minutes — once on
`main` and once on a pull request — because Docker Hub refused an anonymous pull
from a shared runner address, and no reference in the repository had caused it.
Because the sources are named by digest, a mirror cannot serve different content
than the origin, only refuse to serve; naming several is therefore an
availability decision and does not widen what the repository trusts.

The image a `docker build` resolves through its Dockerfile SHALL carry a digest
like any other, but is not yet held to naming several sources: giving a build a
registry fallback means passing the reference in from the caller per attempt,
and that is a separate change. Measured 2026-10-09, the only such image is the
Go builder, which CI does not pull.

#### Scenario: Deployment selects an image

- **WHEN** infrastructure references a release
- **THEN** it uses immutable image content rather than a mutable tag alone
- **AND** runtime filesystem and user restrictions remain enabled

#### Scenario: A gate pulls an image

- **WHEN** a gate needs a container image to run
- **THEN** it names that image by digest
- **AND** it reports which registry served the digest

#### Scenario: An image reference carries only a tag

- **WHEN** any file in the repository references a registry image without a digest
- **THEN** the gate refuses and names the file, the line and the reference

#### Scenario: The first registry refuses the pull

- **WHEN** the registry a gate asks first will not serve the digest
- **THEN** the gate asks the next source it names
- **AND** the gate runs against the same content, because the digest is the same

#### Scenario: No registry serves the digest

- **WHEN** every source a gate names refuses
- **THEN** the gate fails and reports what each registry said
- **AND** it does not substitute an image whose digest it did not ask for

#### Scenario: A gate is reduced to one registry

- **WHEN** a gate that pulls an image names fewer than two sources for its digest
- **THEN** the gate refuses
- **AND** so does a gate whose sources do not all carry the same digest
