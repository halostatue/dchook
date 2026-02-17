# Dchook

[![Go Reference][shield-godoc]][godoc] [![Go Report Card][shield-goreport]][goreport]
[![Apache 2.0][shield-licence]][licence]

- code :: <https://github.com/halostatue/dchook>
- issues :: <https://github.com/halostatue/dchook/issues>

A listener for a webhook that will execute `docker compose pull` and `docker compose up`.

## Installation

```bash
go get github.com/halostatue/dchook
```

## Usage

```go
import "github.com/halostatue/dchook"
```

## Semantic Versioning

Dchook follows [Semantic Versioning 2.0][semver].

[godoc]: https://pkg.go.dev/github.com/halostatue/dchook
[goreport]: https://goreportcard.com/report/github.com/halostatue/dchook
[licence]: https://github.com/halostatue/dchook/blob/main/LICENCE.md
[semver]: https://semver.org/
[shield-godoc]: https://pkg.go.dev/badge/github.com/halostatue/dchook.svg
[shield-goreport]: https://goreportcard.com/badge/github.com/halostatue/dchook
[shield-licence]: https://img.shields.io/github/license/halostatue/dchook.svg?style=for-the-badge
