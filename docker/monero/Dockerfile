# syntax=docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
FROM --platform=$BUILDPLATFORM debian:trixie-slim@sha256:89c1ea91e897f0fe0a3731595330bc45b8bdef4047464a07116f891a94054b31 AS verified
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl gpg gpgv bzip2 && rm -rf /var/lib/apt/lists/*
WORKDIR /release
COPY docker/monero/binaryfate.asc docker/monero/hashes.asc ./
ARG TARGETARCH
RUN set -eu; \
    export GNUPGHOME=/tmp/release-keyring; mkdir -m 700 "$GNUPGHOME"; \
    fingerprint=$(gpg --batch --with-colons --show-keys binaryfate.asc | awk -F: '$1 == "fpr" { print $10; exit }'); \
    test "$fingerprint" = 81AC591FE9C4B65C5806AFC3F0AF4D462A0BDF92; \
    gpg --batch --output /tmp/release-key.gpg --dearmor binaryfate.asc; \
    gpgv --keyring /tmp/release-key.gpg hashes.asc; \
    case "$TARGETARCH" in amd64) platform=x64 ;; arm64) platform=armv8 ;; *) exit 1 ;; esac; \
    archive="monero-linux-${platform}-v0.18.5.4.tar.bz2"; \
    checksum=$(awk -v archive="$archive" '$2 == archive { print $1 }' hashes.asc); \
    test "${#checksum}" = 64; \
    curl --fail --location --proto '=https' --proto-redir '=https' --tlsv1.2 \
      --max-time 180 --retry 2 --output /tmp/monero.tar.bz2 "https://downloads.getmonero.org/cli/$archive"; \
    printf '%s  /tmp/monero.tar.bz2\n' "$checksum" | sha256sum --check; \
    mkdir /out; \
    tar -xjf /tmp/monero.tar.bz2 --strip-components=1 -C /out --wildcards \
      '*/monero-wallet-cli' '*/monero-wallet-rpc' '*/LICENSE'; \
    chmod 0555 /out/monero-wallet-cli /out/monero-wallet-rpc

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS guard-builder
WORKDIR /src
COPY go.mod go.sum ./
COPY link/go.mod link/go.sum ./link/
RUN go mod download
COPY internal/chain ./internal/chain
COPY internal/walletguard ./internal/walletguard
COPY cmd/monero-guard ./cmd/monero-guard
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /guard ./cmd/monero-guard

# Non-root, no shell or package manager. The CLI is also used by the
# startup guard to verify the encrypted wallet's type without exporting keys.
FROM gcr.io/distroless/cc-debian13:nonroot@sha256:3ee00291cf386ec52a552e6a858d555debd74776eb4bfa53fa3df2cd6677515e AS runtime
LABEL org.opencontainers.image.source=https://github.com/6586x57890143/cygnet \
      org.opencontainers.image.version=0.18.5.4
USER 65532:65532
ENV HOME=/tmp TMPDIR=/tmp
WORKDIR /wallet
COPY --from=verified /out/LICENSE /usr/share/licenses/monero/LICENSE

FROM runtime AS wallet-cli
COPY --from=verified /out/monero-wallet-cli /usr/local/bin/monero-wallet-cli
ENTRYPOINT ["/usr/local/bin/monero-wallet-cli"]

FROM runtime AS wallet-rpc
COPY --from=verified /out/monero-wallet-rpc /usr/local/bin/monero-wallet-rpc
COPY --from=verified /out/monero-wallet-cli /usr/local/bin/monero-wallet-cli
COPY --from=guard-builder /guard /usr/local/bin/monero-guard
ENTRYPOINT ["/usr/local/bin/monero-guard"]
