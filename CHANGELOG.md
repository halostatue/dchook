# `dchook` Changelog

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
