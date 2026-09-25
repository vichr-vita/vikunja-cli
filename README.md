# vikunja-cli

[![CI](https://github.com/vichr-vita/vikunja-cli/actions/workflows/ci.yml/badge.svg?event=pull_request)](https://github.com/vichr-vita/vikunja-cli/actions/workflows/ci.yml)
[![Release](https://github.com/vichr-vita/vikunja-cli/actions/workflows/release.yml/badge.svg?branch=main)](https://github.com/vichr-vita/vikunja-cli/actions/workflows/release.yml)

A small, dependency-free Go command-line client for the Vikunja API v2. It
manages projects, tasks, labels, and task comments, and provides a constrained
raw API command for other v2 endpoints. Commands emit one JSON value, so they
can be used from scripts as well as interactively.

## Install

Go 1.24 or newer is required. Clone this repository on each computer, then run:

```sh
go test ./...
go install ./cmd/vikunja-cli
```

The install command puts `vikunja-cli` in Go's binary directory (normally
`~/go/bin`); add that directory to your `PATH`. Use `make build` to build a
binary at `bin/vikunja-cli` without installing it on systems with Make.

## Configure

Create `~/.config/vikunja/config.json` on each computer. Use the base URL of
your own Vikunja instance and an API token with only the permissions needed:

```json
{"url":"https://vikunja.example.com/","token":"YOUR_API_TOKEN"}
```

On Unix, restrict the file to your account:

```sh
chmod 0600 ~/.config/vikunja/config.json
```

The CLI refuses non-regular or group/other-accessible credential files on Unix.
Set `VIKUNJA_CONFIG` to use a different protected file. The token is never
accepted as a command argument or environment variable. Network access to your
instance is required; this repository does not configure or expose a server.

## Commands

```text
vikunja-cli user get
vikunja-cli project list|get|create|update|delete
vikunja-cli task list|get|create|update|delete
vikunja-cli label list|get|create|update|delete|attach|detach
vikunja-cli comment list|get|create|update|delete
vikunja-cli api METHOD PATH
```

Examples:

```sh
vikunja-cli project list --page 1 --per-page 100
vikunja-cli task list --query passport --page 1 --per-page 100
vikunja-cli task create 7 --data '{"title":"Renew passport","due_date":"2026-10-01T16:00:00+02:00"}'
vikunja-cli task update 42 --data '{"done":true}'
vikunja-cli label attach 42 9
vikunja-cli api GET '/tasks?page=1&per_page=100&filter=done%20%3D%20false'
```

List commands return paginated envelopes with `items`, `total`, `page`,
`per_page`, and `total_pages`; fetch each page when you need a complete result.
Writes accept JSON through `--data`. Task timestamps require RFC 3339 with an
explicit timezone and are normalized to UTC.

Delete and detach commands require a target-specific `--confirm` value, for
example `vikunja-cli task delete 42 --confirm task:42`. Add `--help` to a
complete destructive command to see the required value without loading
credentials or making a request.

Raw API paths are relative to `/api/v2`, such as `/tasks/42`; absolute URLs,
path traversal, and redirects outside the configured API origin are rejected.
The API token's scopes remain the authorization boundary.

Success writes one newline-terminated JSON value to stdout. Failure writes a
JSON error to stderr and exits nonzero. Exit codes distinguish input (2),
configuration (3), transport (4), authentication (5), API (6), and invalid
server response (7) errors; 1 indicates an internal failure.

## Development

`make test` runs the offline Go tests and vet. `make smoke` performs a read-only
request for the authenticated user against the instance in your config file.

Pull requests target `dev`. CI runs the offline tests, vet, and build for each
pull request. Merging `dev` into `main` runs those checks again and publishes a
GitHub release with Linux, macOS, and Windows binaries plus SHA-256 checksums.
The first release is `v0.1.0`. Later releases use the commits since the previous
tag: `feat:` bumps the minor version, `!` or `BREAKING CHANGE:` bumps the major
version, and all other changes bump the patch version. The release tag points to
the commit on `main`.

## License

MIT. See [LICENSE](LICENSE).
