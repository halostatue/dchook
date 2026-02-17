# dchook: `docker compose` Hook

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
with hard-coded defaults to reduce any potential attack surface.

- Constant-time HMAC signature verification algorithms (SHA-256, SHA-384,
  SHA-512) may be restricted
- Replay attacks are mitigated via microsecond timestamps (valid for ±5 minutes)
  and tracked for ten minutes
- Failed attempts apply fail2ban behaviour: two failures results in a one-hour
  rejection of any requests from the originating IP (v4 or normalized v6)
- Secret files must be FIFOs (bash process substitution `<(echo 1)`) or regular
  files with 0600/0400 permissions
- Compose files must exist on startup
- Docker socket access is verified on startup (via `docker version`)
- `dchook` and `dchook-notify` must match on major.minor versions, and if both
  versions match exactly, the git commit must match exactly

It has a health-check endpoint, is designed to be run as a non-root user, and
Docker compose updates are performed asynchronously after responding to the
webhook.

## Installation

Releases of `dchook` are signed with cosign and have GitHub SLSA attestations
during the build process for binaries on [releases][releases].

### Listener (dchook)

- Download from [releases][releases] (recommended)
- Build from source `go install github.com/halostatue/dchook/cmd/dchook@v1.0.0`
- Use the `dchook-listener` container image:
  `docker pull ghcr.io/halostatue/dchook/dchook-listener:v1.0.0`

  The container image uses the signed, attested binaries from
  [releases][releases] and is itself signed and attested.

### CLI Tool (dchook-notify)

- Download from [releases][releases]
- Build from source
  `go install github.com/halostatue/dchook/cmd/dchook-notify@v1.0.0`

## Configuration

### Listener (dchook)

`dchook` is configured via environment variables or command-line flags. Flags
take precedence.

| Variable                         | Flag                        | Required / Default     | Purpose                                         |
| -------------------------------- | --------------------------- | ---------------------- | ----------------------------------------------- |
| `DCHOOK_SECRET_FILE`             | `-s`                        | ✅                     | Path to file containing webhook secret          |
| `DCHOOK_COMPOSE_FILE`            | `-c`                        | ✅                     | Path to `docker-compose.yml` to manage          |
| `DCHOOK_PORT`                    | `-p`                        | 7999                   | HTTP port to listen on                          |
| `DCHOOK_ALLOWED_ALGORITHMS`      | `--algorithms`              | `sha256,sha384,sha512` | Comma-separated list of allowed HMAC algorithms |
| `DCHOOK_ENABLE_VERSION_ENDPOINT` | `--enable-version-endpoint` |                        | Enable `/version` endpoint                      |

### CLI Tool (dchook-notify)

`dchook-notify` is configured via environment variables or command-line flags.
Flags take precedence.

| Variable             | Flag | Required / Default | Purpose                                      |
| -------------------- | ---- | ------------------ | -------------------------------------------- |
| `DCHOOK_URL`         | `-u` | ✅                 | Webhook endpoint URL                         |
| `DCHOOK_SECRET_FILE` | `-s` | ✅                 | Path to file containing webhook secret       |
| `DCHOOK_ALGORITHM`   | `-a` | `sha256`           | Hash algorithm: `sha256`, `sha384`, `sha512` |

## Usage

### Systemd Service (Recommended)

After downloading the binary from [releases][releases], set up as a systemd
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

**Note:** Copy the generated secret to your CI/CD system (GitHub Secrets,
environment variables, password manager, etc.) before continuing.

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
    image: ghcr.io/halostatue/dchook/dchook-listener:v1.0.0
    ports:
      - "7999:7999"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - /opt/app:/compose/app:ro
    secrets:
      - webhook_secret
    environment:
      - DCHOOK_SECRET_FILE=/run/secrets/webhook_secret
      - DCHOOK_COMPOSE_FILE=/compose/app/docker-compose.yml
      - DCHOOK_PORT=7999
    # Grant docker socket access - replace 999 with your docker group ID
    # Find with: getent group docker | cut -d: -f3
    user: "65534:999"

secrets:
  webhook_secret:
    file: ./webhook_secret.txt
```

### Generate Webhook Secret

```bash
openssl rand -hex 32 > webhook_secret.txt
```

### Sending Webhooks

#### Using dchook-notify CLI

```bash
# Using environment variables
export DCHOOK_URL="https://webhook.yourdomain.com/deploy"
export DCHOOK_SECRET_FILE="/path/to/webhook_secret.txt"

echo '{"image":"ghcr.io/user/app:latest"}' | dchook-notify -
dchook-notify payload.json

# Using flags
dchook-notify -u https://webhook.yourdomain.com/deploy -s /path/to/secret payload.json

# With password manager (process substitution)
DCHOOK_SECRET_FILE=<(pass show webhook-secret) dchook-notify payload.json
DCHOOK_SECRET_FILE=<(op read op://MyServer/DCHook/secret) dchook-notify payload.json

# With different algorithm
dchook-notify -a sha512 payload.json
```

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
          gh release download v1.0.0 \
            --repo halostatue/dchook \
            --pattern 'dchook-notify_*_linux_amd64.tar.gz' \
            --output /tmp/dchook-notify.tar.gz
          gh attestation verify /tmp/dchook-notify.tar.gz \
            --repo halostatue/dchook
          tar -xzf /tmp/dchook-notify.tar.gz -C /tmp dchook-notify
          sudo mv /tmp/dchook-notify /usr/local/bin/
          sudo chmod 755 /usr/local/bin/dchook-notify
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

- `DCHOOK_URL` - Your webhook endpoint (e.g.,
  `https://webhook.yourdomain.com/deploy`)
- `DCHOOK_SECRET` - The webhook secret content

#### Manual cURL (without CLI)

```bash
# The webhook expects an envelope with version info and timestamp
# It's recommended to use dchook-notify instead of manual curl

SECRET=$(cat /path/to/webhook_secret.txt)
PAYLOAD='{"image":"ghcr.io/user/app:latest"}'

# Create envelope (simplified - real envelope includes version/commit/timestamp)
BODY=$(echo "$PAYLOAD" | jq -c '{dchook: {version: "v1.0.0", commit: "manual", timestamp: (now * 1000000 | floor)}, payload: .}')

# Generate signature
SIGNATURE="sha256:$(echo -n "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | cut -d' ' -f2)"

# Send webhook
curl -X POST https://webhook.yourdomain.com/deploy \
  -H "Content-Type: application/json" \
  -H "Updater-Signature: $SIGNATURE" \
  -d "$BODY"
```

## Endpoints

- `POST /deploy` - Trigger deployment (requires valid signature)
- `GET /health` - Health check (returns 200 OK)
- `GET /version` - Version information (only if enabled)

## Development

See the `example/` directory for a complete local testing environment with:
- Dockerfile for building a test image
- Docker Compose setup with dchook listener and managed app
- Justfile with recipes for common tasks (build, test, generate secrets)

Run `just --list` in the `example/` directory to see available commands.

[releases]: https://github.com/halostatue/dchook/releases
