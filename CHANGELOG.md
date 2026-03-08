# `dchook` Changelog

## 1.2.2 / 2026-03-07

- Added proxy logic debugging. This may be removed in the future.
- Added experimental `DCHOOK_EXCEPT_SERVICES` environment variable to exclude
  specific services from updates (e.g., when dchook manages its own stack).

## 1.2.1 / 2026-03-02

- Improved logging on the listener.

## 1.2.0 / 2026-03-02

Improved deployment tracking and security validation.

### Security Enhancements

- Secret validation has been improved for both the listener and the notifier.
  The secret file _may_ be specified on the command-line via process
  substitution (`-s <(pass dchook.secret)`) or as a filename via the
  command-line or via `DCHOOK_SECRET_FILE`. When provided as a filename, the
  secret file must be a regular file (not a symbolic link) and may not be in
  `/etc/shadow`, `/etc/passwd`, `/proc`, `/sys`, or `/dev`.

  On the listener, the secret file must also be an absolute path.

- Applied comprehensive security and code quality audits.

### Listener (`dchook`)

- Compose files must not be symlinks, must be absolute paths, and must be
  regular files.

- The listener now maintains a buffer of the last ten deployments with a
  deployment ID, timestamp, status, and image pull and restart results, exit
  codes, output, and duration.

  Deployments are immediately queryable after being accepted with a `status`
  field that tracks progress: `"pending"`, `"pulling"`, `"restarting"`,
  `"complete"`, or `"failed"`.

  When `Accept: application/json` is supplied to the deploy endpoint, the
  response is JSON with the `deployment_id` included. This will be the default
  behaviour with `dchook` 2.x.

  The deployment buffer can be obtained by map or as a list:

  - `GET /deploy/status/{id}`: Get specific deployment details
  - `GET /deploy/status`: List recent deployments (newest first)

  Both queries require HMAC authentication. List requests include a random nonce
  in the signature (`timestamp:nonce`) to prevent replay attacks.

- Previously, the `listener` would fail early if Docker is unavailable, making
  it impossible to be aware of issues except through a gateway error response.
  This has been changed to check on startup and return 503 on `POST /deploy`
  when Docker is unavailable.

  `/health` will also return a 503 if Docker is unavailable.

- Switched to structured logging (JSON format via `slog`).

- The listener now supports explicit specification of the Docker compose project
  via `DCHOOK_COMPOSE_PROJECT` or the `--project` option. This is required when
  a custom project name is used via the Docker Compose command-line. (The
  project name must follow the Docker Compose rules.)

- Removed the `--enable-version-endpoint` flag. Version information is now
  included in status endpoint responses.

### Notifier (`dchook-notify`)

The notifier was refactored to support subcommands:

- `deploy`: Triggers a deployment (the default default behaviour when no command
  is supplied) The default behaviour is to display text, but the raw JSON
  response can be output with the new `-j` option (valid only for `deploy`).

- `status <deployment_id>`: Queries the deployment status and outputs the JSON
  response.

- `list`: Lists the recent deployments ordered by most recent deployment first,
  as JSON.

Additional exit codes were added for the new functionality (53 for Docker
unavailable / 503 and 44 for deployment ID not found).

### Deprecations

- `DCHOOK_URL` should be the base URL (e.g., `https://example.com`), not
  including `/deploy`. In v1.2, a warning is logged and the path is stripped. In
  v1.3+, this will be an error.

## 1.1.0 / 2026-02-26

- Fixed a bug with `docker` and `docker compose` not being present in the main
  Dockerfile where it differed in Docker setup from the `example/` Dockerfile.

  We are adding tests (using the `example/` Dockerfile) to reduce failure
  chances.

- Modified `dchook-notify` to return different exit codes for different failure
  cases.

- Added `-q` (quiet) option to suppress output strings.

## 1.0.0 / 2026-02-20

- Initial release.
