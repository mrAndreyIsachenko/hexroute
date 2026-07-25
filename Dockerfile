ARG GO_BUILDER_IMAGE=golang:1.25.12-alpine3.23@sha256:cc985ef6f9c3bf9ece7488129c9abe0a150388ccdfa428d886fc709dca0b230a

FROM ${GO_BUILDER_IMAGE} AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/hexroute-ingest ./cmd/hexroute-ingest
COPY internal ./internal

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown

RUN test -n "${TARGETARCH}" && \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" \
    go build \
      -buildvcs=false \
      -mod=readonly \
      -trimpath \
      -ldflags="-s -w -X github.com/mrAndreyIsachenko/hexroute/internal/buildinfo.Version=${VERSION} -X github.com/mrAndreyIsachenko/hexroute/internal/buildinfo.Commit=${COMMIT}" \
      -o /out/hexroute-ingest \
      ./cmd/hexroute-ingest

FROM scratch AS runtime

LABEL org.opencontainers.image.title="Hexroute cloud runtime" \
      org.opencontainers.image.description="Telemetry-only API and worker runtime" \
      org.opencontainers.image.licenses="Apache-2.0"

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chmod=0555 /out/hexroute-ingest /usr/local/bin/hexroute-ingest

USER 65532:65532
WORKDIR /

ENV HOME=/tmp/hexroute-home \
    TMPDIR=/tmp

EXPOSE 8080
STOPSIGNAL SIGTERM

ENTRYPOINT ["/usr/local/bin/hexroute-ingest"]
