# Production Dockerfile using pre-built binaries from GoReleaser
# This is used by the release workflow to create signed, attested images

FROM dhi.io/debian-base:trixie-debian13-dev

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        curl \
        gnupg && \
    install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc && \
    chmod a+r /etc/apt/keyrings/docker.asc && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian trixie stable" > /etc/apt/sources.list.d/docker.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
        docker-ce-cli \
        docker-compose-plugin \
        tini && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy pre-built binary (platform-specific, set by buildx)
ARG TARGETARCH
COPY dist/linux_${TARGETARCH}/dchook /app/dchook

USER nonroot
ENTRYPOINT ["/usr/bin/tini", "--", "/app/dchook"]
