# Production Dockerfile using pre-built binaries from GoReleaser
# This is used by the release workflow to create signed, attested images

FROM dhi.io/debian-base:trixie-debian13-dev
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    docker.io \
    tini \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy pre-built binary (platform-specific, set by buildx)
ARG TARGETARCH
COPY dist/linux_${TARGETARCH}/dchook /app/dchook

USER nonroot
ENTRYPOINT ["/usr/bin/tini", "--", "/app/dchook"]
