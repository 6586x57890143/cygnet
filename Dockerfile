# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
COPY link/go.mod link/go.sum ./link/
RUN go mod download
COPY . .
ARG TARGETOS
ARG TARGETARCH
# REVISION is the commit, for /status and /help: the build context has no
# .git to stamp it from.
ARG REVISION
# default.pgo in cmd/cygnet is picked up automatically when present.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags="-s -w -X github.com/6586x57890143/cygnet/internal/core.revision=$REVISION" -o /out/cygnet ./cmd/cygnet

# Pinned by digest: a rebuild of an old commit produces the same image,
# and the tag lookup is off the path.
# gcr.io/distroless/static-debian13:nonroot as of 2026-10-10.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
# Links the GHCR package to the repository, which is what lets the
# workflow's GITHUB_TOKEN push to it.
LABEL org.opencontainers.image.source=https://github.com/6586x57890143/cygnet
COPY --from=builder /out/cygnet /cygnet
USER nonroot:nonroot
ENTRYPOINT ["/cygnet"]
