# dchook Example

This directory contains a working example of dchook for local testing.

## Quick Start

Using [just](https://github.com/casey/just):

```bash
# Start services
just up

# Test successful webhook
just success

# Test rate limiting
just rate-limited

# Test failed authentication
just failed

# Test ban after failures
just banned

# View logs
just logs

# Stop services
just down
```

## Manual Setup

The webhook secret has been generated in `webhook_secret.txt` (permissions:
0600).

Start the services:

```bash
docker compose up -d
```

Check the services are running:

```bash
docker compose ps
```

Visit the web service:

```bash
curl http://localhost:8081
```

Check dchook health:

```bash
curl http://localhost:7999/health
curl http://localhost:7999/version
```

## Testing Webhook

Build the notifier (from project root):

```bash
go build -o dchook-notify ./cmd/dchook-notify
```

Send a test webhook:

```bash
# From the example directory
export DCHOOK_URL="http://localhost:7999/deploy"
export DCHOOK_SECRET_FILE="./webhook_secret.txt"

../dchook-notify payload.json
```

Or using flags:

```bash
../dchook-notify -u http://localhost:7999/deploy -s ./webhook_secret.txt payload.json
```

Watch the logs:

```bash
docker compose logs -f dchook
```

## Test Scenarios

The Justfile includes recipes for testing different scenarios:

- `just success` - Send a valid webhook (should succeed)
- `just rate-limited` - Send 2 webhooks quickly (second should be rate limited)
- `just failed` - Send webhook with wrong secret (should fail)
- `just banned` - Send 2 failed webhooks (should trigger 1-hour ban)

## Cleanup

```bash
just clean
# or
docker compose down
```

## Notes

- The example uses a separate `Dockerfile` with public base images
  (`golang:alpine`, `alpine:latest`) for easy local testing without Docker Hub
  authentication
- The production `Dockerfile` at the repo root uses Docker Hardened Images
  (`dhi.io`) which require authentication
- The dchook service has access to the Docker socket to manage containers
- The webhook secret is mounted as a Docker secret with proper permissions
- The compose file is mounted read-only into the dchook container
- Rate limit: 1 successful deployment per minute
- Ban: 2 failed attempts trigger 1-hour ban (restart services to clear)
- In production, use HTTPS and proper network isolation
