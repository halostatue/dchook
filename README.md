# dchook: `docker compose` Hook

[![Go Report Card][shield-goreport]][goreport]
[![Apache 2.0][shield-licence]][licence] ![Coveralls][shield-coveralls]

Secure webhook receiver for updating Docker Compose deployments. Sits somewhere
between manual `scp` or `git pull`, and full orchestration -- perfect for
simpler production deployments that need automated updates from CI/CD.

`dchook` receives authenticated webhooks and triggers `docker compose pull` and
`docker compose up -d` to update and restart your services.

## Components

- **dchook**: The webhook listener that authenticates received webhooks and
  triggers Docker Compose updates
- **dchook-notify**: CLI tool for sending authenticated webhooks to the
  listener.

## Features

`dchook` is security-forward. The listener has minimal configuration by design
with hard-coded defaults to reduce the potential attack surface.

- Constant-time HMAC signature verification algorithms (SHA-256, SHA-384,
  SHA-512) may be restricted
- Replay attacks are mitigated via microsecond timestamps (valid for -5…+1
  minutes) and tracked for ten minutes
- Failed attempts apply strict banning behaviour: two failures results in a
  one-hour rejection of any requests from the originating IP (v4 or normalized
  v6)
- Client IP extraction respects `X-Forwarded-For` from trusted proxies (loopback
  and private network ranges), falling back to `RemoteAddr` for direct
  connections
- `dchook-notify` will send at most 1MiB as the payload, and `dchook` refuses
  any request body just over 1MiB
- Secret files must be FIFOs (bash process substitution `<(echo 1)`) or regular
  files with 0600/0400 permissions
- Compose files must exist on startup
- Docker socket access is verified on startup (via `docker version` and
  `docker compose versison`) but reported on access
- Version compatibility is enforced (see
  [Versioning Policy](#versioning-policy))

It has a health-check endpoint, is designed to be run as a non-root user, and
Docker compose updates are performed asynchronously after responding to the
webhook. Deployment tracking maintains a history of the last 10 deployments with
their results, accessible via authenticated status endpoints.

## Versioning Policy

`dchook` uses semantic versioning with strict compatibility requirements between
the listener and CLI tool. This is because feature compatibility is not
guaranteed to be backwards compatible and neither the server nor client will
guarantee such compatibility.

- The **major** and **minor** versions must match exactly. Client version
  `1.2.x` can only communicate with server version `1.2.y` (not `1.3.z`).
- **Patch** versions are both forward and backwards compatible. Within the same
  major.minor, any patch version works (e.g., client `1.2.3` works with server
  `1.2.5`).
- Exact version matches also require commit match. If versions are identical
  (e.g., both `1.2.3`), git commits must also match.

This is a result of a strict modified semantic versioning policy is in place:

- The **major** version increments when core protocol or security changes are
  required (HMAC algorithm change, signature format change, etc.).
- The **minor** version increments when there are new features.
- The **patch** version increments with bug fixes.

## Installation

Releases of `dchook` are signed with cosign and have GitHub SLSA attestations
during the build process for binaries on [releases][releases].

### Listener (`dchook`)

- Download from [releases][releases]
- Use the `dchook-listener` container image:
  `docker pull ghcr.io/halostatue/dchook/dchook-listener:1.2.0`

  The container image uses the signed, attested binaries from
  [releases][releases] and is itself signed and attested.
- Build from source `go install github.com/halostatue/dchook/cmd/dchook@v1.2.0`

### CLI Tool (`dchook-notify`)

- Download from [releases][releases]
- Build from source
  `go install github.com/halostatue/dchook/cmd/dchook-notify@v1.2.0`

## Configuration

### Listener (dchook)

`dchook` is configured via environment variables or command-line flags. Flags
take precedence.

| Variable                    | Flag           | Required / Default     | Purpose                                         |
| --------------------------- | -------------- | ---------------------- | ----------------------------------------------- |
| `DCHOOK_SECRET_FILE`        | `-s`           | ✅                     | Path to file containing webhook secret          |
| `DCHOOK_COMPOSE_FILE`       | `-c`           | ✅                     | Path to `docker-compose.yml` to manage          |
| `DCHOOK_COMPOSE_PROJECT`    | `--project`    |                        | Docker Compose project name (optional)          |
| `DCHOOK_BIND_ADDRESS`       | `-b`           | `127.0.0.1`            | Bind address (use `0.0.0.0` for all interfaces) |
| `DCHOOK_PORT`               | `-p`           | 7999                   | HTTP port to listen on                          |
| `DCHOOK_ALLOWED_ALGORITHMS` | `--algorithms` | `sha256,sha384,sha512` | Comma-separated list of allowed HMAC algorithms |

**Security Requirements:**

- **Secret file** (`DCHOOK_SECRET_FILE`):
  - On Unix: Must be a regular file or named pipe
  - Must not be a symlink
  - Must be an absolute path
  - On Unix: Must have 0600 or 0400 permissions
  - Cannot be in `/etc/shadow`, `/etc/passwd`, `/proc`, `/sys`, or `/dev`

- **Compose file** (`DCHOOK_COMPOSE_FILE`):
  - Must not be a symlink
  - Must be an absolute path
  - Must be a regular file

- **Project name** (`DCHOOK_COMPOSE_PROJECT`):
  - Must start with lowercase letter or digit
  - Can only contain lowercase letters, digits, dashes, and underscores

> [!WARNING]
>
> By default, `dchook` binds to `127.0.0.1` (localhost only). The bind address
> can be modified with `DCHOOK_BIND_ADDRESS` or `-b`. `dchook` does _not_
> perform TLS termination, so the use of a reverse proxy for TLS termination is
> strongly recommended.

### CLI Tool (dchook-notify)

`dchook-notify` is configured via environment variables or command-line flags.
Flags take precedence.

| Variable             | Flag | Required / Default | Purpose                                         |
| -------------------- | ---- | ------------------ | ----------------------------------------------- |
| `DCHOOK_URL`         | `-u` | ✅                 | Listener base URL (e.g., `https://example.com`) |
| `DCHOOK_SECRET_FILE` | `-s` | ✅                 | Path to file containing webhook secret          |
| `DCHOOK_ALGORITHM`   | `-a` | `sha256`           | Hash algorithm: `sha256`, `sha384`, `sha512`    |

**Security Requirements:**

- **Secret file** (`DCHOOK_SECRET_FILE`):
  - Must not be a symlink
  - On Unix: Cannot be in `/etc/shadow`, `/etc/passwd`, `/proc`, `/sys`, or
    `/dev`

> [!NOTE]
> `DCHOOK_URL` should be the base URL of the listener, not including `/deploy`.
> For backwards compatibility, v1.2 will strip `/deploy` if present with a
> warning. This will become an error in v1.3+.

**Subcommands:**

- `deploy` (default): Trigger a deployment
- `status <deployment_id>`: Query status of a specific deployment
- `list`: List recent deployments

**Flags:**

- `-q`: Quiet mode (suppress output, exit code only)
- `-j`: JSON output mode (machine-readable, outputs `deployment_id` for
  `deploy`)

## Usage

### As a `systemd` Service

After downloading the binary from [releases][releases], set up as a `systemd`
service:

```bash
# Create user and group
sudo useradd -r -s /bin/false dchook
sudo usermod -aG docker dchook

# Install binary
sudo cp dchook /usr/local/bin/
sudo chmod 755 /usr/local/bin/dchook

# Create secret (save this value for CI/CD configuration)
sudo mkdir -p /etc/dchook
sudo openssl rand -hex 32 | sudo tee /etc/dchook/webhook_secret
sudo chmod 400 /etc/dchook/webhook_secret
sudo chown dchook:dchook /etc/dchook/webhook_secret

# Install and start service
sudo cp dchook.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now dchook
```

> [!NOTE]
>
> Copy the generated secret to your CI/CD system (GitHub Secrets, environment
> variables, password manager, etc.) before continuing.

**Managing the service:**

```bash
# Check status
sudo systemctl status dchook

# View logs
sudo journalctl -u dchook -f

# Restart service
sudo systemctl restart dchook
```

### Docker Compose Deployment

For containerized environments:

```yaml
services:
  webhook:
    image: ghcr.io/halostatue/dchook/dchook-listener:1.2.0
    ports:
      - "7999:7999"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/app:/compose/app:ro
      # Mount Docker config for private registry authentication
      - ~/.docker/config.json:/home/nonroot/.docker/config.json:ro
    secrets:
      - webhook_secret
    environment:
      - DCHOOK_SECRET_FILE=/run/secrets/webhook_secret
      - DCHOOK_COMPOSE_FILE=/compose/app/docker-compose.yml
      - DCHOOK_BIND_ADDRESS=0.0.0.0
      - DCHOOK_PORT=7999
    # Grant docker socket access: replace 999 with your docker group ID
    # Find with: getent group docker | cut -d: -f3
    user: "65534:999"

secrets:
  webhook_secret:
    file: ./webhook_secret.txt
```

> [!NOTE]
> **Private Registry Authentication**: If your compose file references images from
> private registries, mount your Docker config file (shown above) or run
> `docker login <registry>` on the host before starting dchook. The container
> user needs access to these credentials to pull images.

### Generate Webhook Secret

```bash
openssl rand -hex 32 > webhook_secret.txt
```

### Sending Webhooks

#### Using dchook-notify CLI

The CLI accepts JSON payloads or plain text. Payloads can be from files,
standard input, or process substitution (FIFOs). If the payload is larger than
1MiB in size, `dchook-notify` will terminate with an error.

```bash
# Using environment variables
export DCHOOK_URL="https://webhook.yourdomain.com/deploy"
export DCHOOK_SECRET_FILE="/path/to/webhook_secret.txt"

# Trigger deployment (default subcommand)
echo '{"image":"ghcr.io/user/app:1.23.4"}' | dchook-notify -
dchook-notify payload.json

# Trigger deployment with JSON output (for scripting)
deployment_id=$(dchook-notify -j payload.json | jq -r '.deployment_id')

# Query deployment status
dchook-notify status abc123def456

# List recent deployments
dchook-notify list

# Using flags
dchook-notify -u https://webhook.yourdomain.com/deploy -s /path/to/secret payload.json

# With password manager (process substitution)
DCHOOK_SECRET_FILE=<(pass show webhook-secret) dchook-notify payload.json
DCHOOK_SECRET_FILE=<(op read op://MyServer/DCHook/secret) dchook-notify payload.json

# With process substitution for payload (FIFO)
dchook-notify <(echo '{"dynamic":"payload"}')

# With different algorithm
dchook-notify -a sha512 payload.json

# Quiet mode (exit code only)
dchook-notify -q payload.json && echo "Success" || echo "Failed"
```

**Payload Requirements:**

- Maximum size: 1MiB
- Must be valid JSON or printable UTF-8 text
- Non-JSON text is automatically wrapped as a JSON string
- Binary data or non-printable characters are rejected

#### GitHub Actions Example

```yaml
name: Build and Deploy

on:
  push:
    branches: [main]

env:
  REGISTRY: ghcr.io
  IMAGE_NAME: ${{ github.repository }}

jobs:
  build-and-deploy:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      packages: write

    steps:
      - uses: actions/checkout@de0fac2e4500dabe0009e67214ff5f5447ce83dd # v6.0.2

      - name: Log in to GHCR
        uses: docker/login-action@c94ce9fb468520275223c153574b00df6fe4bcc9 # v3.7.0
        with:
          registry: ${{ env.REGISTRY }}
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build and push
        uses: docker/build-push-action@263435318d21b8e681c14492fe198d362a7d2c83 # v6.18.0
        with:
          push: true
          tags: ${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:latest

      - name: Install dchook-notify
        run: |
          gh release download v1.2.0 \
            --repo halostatue/dchook \
            --pattern 'dchook-notify_Linux_x86_64.tar.gz'
          gh attestation verify dchook-notify_Linux_x86_64.tar.gz \
            --repo halostatue/dchook
          tar -xzf dchook-notify_Linux_x86_64.tar.gz \
            --strip-components=1 \
            -C /usr/local/bin \
            dchook-notify_Linux_x86_64/dchook-notify
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

      - name: Trigger deployment
        env:
          DCHOOK_URL: ${{ secrets.DCHOOK_URL }}
          DCHOOK_SECRET: ${{ secrets.DCHOOK_SECRET }}
        run: |
          echo "$DCHOOK_SECRET" > webhook_secret.txt
          chmod 400 webhook_secret.txt
          echo '{"image":"${{ env.REGISTRY }}/${{ env.IMAGE_NAME }}:latest","commit":"${{ github.sha }}"}' | \
            dchook-notify -s webhook_secret.txt -
```

**Required GitHub Secrets:**

- `DCHOOK_URL`: Your listener base URL (e.g., `https://webhook.yourdomain.com`)
- `DCHOOK_SECRET`: The webhook secret content

#### Manual cURL (without CLI)

```bash
# The webhook expects an envelope with version info and timestamp.
# It's strongly recommended to use dchook-notify instead of manual curl

SECRET=$(cat /path/to/webhook_secret.txt)
PAYLOAD='{"image":"ghcr.io/user/app:latest"}'

# Create envelope
# Structure: {dchook: {version, commit, timestamp}, payload: <your-data>}
# timestamp must be a string (Unix microseconds) to avoid JSON precision loss
TIMESTAMP=$(date +%s%6N)
BODY=$(echo "$PAYLOAD" | jq -c --arg ts "$TIMESTAMP" '{dchook: {version: "v1.2.0", commit: "manual", timestamp: $ts}, payload: .}')

# Generate signature
SIGNATURE="sha256:$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | cut -d' ' -f2)"

# Send webhook
curl -X POST https://webhook.yourdomain.com/deploy \
  -H "Content-Type: application/json" \
  -H "Dchook-Signature: $SIGNATURE" \
  -d "$BODY"
```

**Webhook Payload Structure:**

```json
{
  "dchook": {
    "version": "v1.2.0",
    "commit": "abc123",
    "timestamp": "1739923200000000"
  },
  "payload": {
    "image": "ghcr.io/user/app:latest",
    "commit": "def456"
  }
}
```

- `dchook.version`: Client version (must match server major.minor)
- `dchook.commit`: Client commit (must match if versions are identical)
- `dchook.timestamp`: Unix microseconds as string (valid for -5…+1 minutes)
- `payload`: Your application data (any valid JSON value or printable Unicode)
  up to 1MiB in size

## Endpoints

### Webhook Endpoints

- `POST /deploy`: Trigger deployment (requires valid signature)
  - Returns `202 Accepted` with deployment ID
  - Accepts `Accept: application/json` header for JSON response
  - Without header, returns plain text (backwards compatible)

The JSON response will become the default response in v1.3.

### Status Endpoints

- `GET /deploy/status/{id}`: Get deployment status by ID
  - Requires HMAC authentication via headers
  - Returns deployment details including:
    - `status`: Current state (`"pending"`, `"pulling"`, `"restarting"`, `"complete"`, `"failed"`)
    - `pull`: Pull operation results (exit code, output, duration)
    - `restart`: Restart operation results (exit code, output, duration)
    - `timestamp`: When deployment was triggered
    - `request`: Original webhook payload
- `GET /deploy/status`: List recent deployments
  - Requires HMAC authentication via headers
  - Returns last 10 deployments, sorted by timestamp (newest first)
  - Each deployment includes the same fields as the single deployment endpoint

### Health & Info

- `GET /health`: Health check
  - Returns `200 OK` if Docker is available
  - Returns `503 Service Unavailable` if Docker is not accessible
  - Includes deployment success/failure counts

### Status Endpoint Authentication

Status endpoints require HMAC authentication using request headers:

- `X-Dchook-Timestamp`: Current Unix microseconds (as string)
- `X-Dchook-Signature`: HMAC signature of the payload
- `X-Dchook-Nonce`: Random nonce (for list requests only)

**Signature payload:**

- For `/deploy/status/{id}`: `timestamp:deploymentID`
- For `/deploy/status`: `timestamp:nonce`

**Example using dchook-notify:**

```bash
# Query specific deployment
dchook-notify status abc123def456

# List all recent deployments
dchook-notify list
```

### Exit Codes

`dchook-notify` returns meaningful exit codes for scripting:

| Exit Code | HTTP Status | Meaning                          |
| --------- | ----------- | -------------------------------- |
| 0         | 202         | Success                          |
| 1         | -           | Configuration error              |
| 2         | -           | Payload error                    |
| 3         | -           | Request error                    |
| 40        | 400         | Bad request                      |
| 41        | 401         | Unauthorized (invalid signature) |
| 43        | 403         | Forbidden (banned IP)            |
| 44        | 404         | Not found                        |
| 13        | 413         | Payload too large                |
| 29        | 429         | Rate limited                     |
| 50        | 500         | Server error                     |
| 53        | 503         | Service unavailable              |
| 99        | -           | Unknown status                   |

## Development

See the `example/` directory for a complete local testing environment with:

- Dockerfile for building a test image
- Docker Compose setup with dchook listener and managed app
- Justfile with recipes for common tasks (build, test, generate secrets)

Run `just --list` in the `example/` directory to see available commands.

[releases]: https://github.com/halostatue/dchook/releases
[godoc]: https://pkg.go.dev/github.com/halostatue/dchook
[goreport]: https://goreportcard.com/report/github.com/halostatue/dchook
[licence]: https://github.com/halostatue/dchook/blob/main/LICENCE.md
[shield-godoc]: https://img.shields.io/badge/go-reference-blue.svg?style=for-the-badge "Go Reference"
[shield-goreport]: https://goreportcard.com/badge/github.com/halostatue/dchook?style=for-the-badge "Go Report Card"
[shield-licence]: https://img.shields.io/github/license/halostatue/dchook?style=for-the-badge&label=licence "Apache 2.0"
[shield-coveralls]: https://img.shields.io/coverallsCoverage/github/halostatue/dchook?style=for-the-badge "Coverage"
