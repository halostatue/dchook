# Example Dockerfile for local testing
# Uses public base images instead of Docker Hardened Images (dhi.io)
# which require Docker Hub authentication.

FROM golang:1.26-trixie AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download
COPY internal ./internal
COPY cmd/dchook ./cmd/dchook

ARG VERSION=dev
ARG COMMIT=unknown

RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o dchook \
    ./cmd/dchook

FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    tini \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian trixie stable" > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -r -u 65532 -g nogroup nonroot

WORKDIR /app
COPY --from=builder /build/dchook /app/dchook

USER nonroot
ENTRYPOINT ["/usr/bin/tini", "--", "/app/dchook"]
