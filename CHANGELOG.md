# Changelog

All notable changes to repoMouse are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and this project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.1.0] - 2026-11-27

First tagged release: the 1.0 line was the initial implementation, this release
hardens it and makes the operational edges explicit.

### Added

- `sync` merges `authorized_keys` instead of replacing it. Only lines repoMouse
  wrote are refreshed, identified by a trailing `repoMouse:<username>` tag on the
  key comment; personal login keys and keys owned by other tools survive.
- `authorized_keys`, the config cache and hook scripts are written atomically
  (temp file + rename), so a crash cannot leave a truncated file that locks
  users out.
- `repair-hooks` regenerates a missing config cache from the admin repo instead
  of failing, since it is the recovery tool after moving the binary.
- Config validation: `server.repos_dir` must be absolute, repo names are checked
  for traversal, and `admin` is rejected as a repo name (it is the config repo).
- Pre-receive input validation: ref updates must be full SHA-1/SHA-256 object
  names and `REPOMOUSE_REPO` is validated before it is used in a path.
- End-to-end test suite (`cmd/repomouse/e2e_test.go`) that builds the binary and
  drives real `git push`/`clone` through the forced-command path; unit tests for
  `setup`, `sync`, `repair-hooks`, authorized_keys merging and cache handling.
- `repomouse --help`.
- `deploy.sh`: `--dry-run` and `--quick`, tests before shipping, atomic binary
  replacement, `-trimpath`, and an automatic `repair-hooks` + `sync` after the
  upload (as the repo-owning user).
- CI runs `gofmt`, `go vet` and `go test -race`; the Go version comes from
  `go.mod`.

### Fixed

- The pre-receive hook denied force pushes and ref deletions on the `admin` repo
  even for admins. Both the SSH path and the hook now share `access.CheckRepo`.
- README instructed cloning the admin repo under a nonexistent name (`repomouse`
  instead of `admin`).
- `.gitignore`'s unanchored `repomouse` pattern also matched the
  `cmd/repomouse/` directory, silently ignoring new source files there.
- Hook scripts embedded the binary path with Go `%q` instead of shell quoting,
  and forced commands in `authorized_keys` used `%q` instead of OpenSSH quoting;
  both could mangle unusual paths.
- `repair-hooks` only recognized hooks whose path matched case-sensitively.

### Changed

- Project branding is `repoMouse` (binary, config paths and env vars stay
  lowercase `repomouse`).
- README, AGENTS.md and ARCHITECTURE.md rewritten to match the implemented
  behaviour (commands, deploy flow, hook options, subcommand list).
- Deploy verification runs `repomouse --help` instead of the bare binary.
