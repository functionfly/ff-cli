# Contributing to FunctionFly CLI

Thanks for your interest in contributing to `ff-cli`! This guide covers everything you need to get started.

## Prerequisites

- Go 1.22 or later
- Git
- golangci-lint (for linting)

Optional (for integration tests):
- Rust toolchain with `wasm32-wasi` target
- Node.js / npm

## Getting Started

```bash
git clone https://github.com/functionfly/ff-cli.git
cd ff-cli
go mod download
go build ./cmd/ff
```

Run the binary:

```bash
./ff --help
```

## Running Tests

```bash
# Unit tests (fast, CI runs these)
go test -timeout 60s ./...

# Integration tests (slower, requires Rust/npm)
go test -v -timeout 600s -tags integration ./...
```

## Linting

```bash
golangci-lint run --timeout=5m
```

The CI runs the same lint check on every PR.

## Project Structure

```
cmd/fly/
  main.go            Entry point
  commands/          All CLI commands (single package)
    root.go          Root command + command registration
    api.go           HTTP API client
    credentials.go   Keychain + file credential storage
    config.go        Global CLI config (~/.ff/)
    errors.go        Error types + exit codes
    ...
internal/
  cli/               Shared HTTP client + types
  credentials/       Credential persistence
  manifest/          functionfly.jsonc parser
  version/           Build version injection
  bundler/           TypeScript/JS bundler
  flypy/             FlyPy Python-to-Wasm compiler
```

## Adding a New Command

1. Create a new file in `cmd/fly/commands/` (e.g. `mycommand.go`).
2. Define a `NewMyCmd() *cobra.Command` function.
3. Register it in `root.go` inside `NewRootCmd()`.
4. Add tests in `mycommand_test.go`.
5. Run `go vet ./cmd/fly/commands/` and `go test`.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`).
- No comments in code unless asked (per project convention).
- Use `fmt.Errorf("...: %w", err)` for error wrapping.
- User-facing errors should include a hint (e.g. "Run 'ff login' to authenticate").
- Use `printJSON()` for `--json` output and `WantJSON()` for `--format json`.

## Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
feat: add fly schedule trigger command
fix: correct URL encoding in log query params
docs: update README install instructions
chore: bump dependencies
```

This drives the automated changelog in GoReleaser releases.

## Pull Requests

1. Fork the repo and create a feature branch from `main`.
2. Make your changes with clear, conventional commit messages.
3. Ensure `go vet`, `go test`, and `golangci-lint` pass locally.
4. Open a PR against `main` with a description of the change.

## Releasing

Releases are automated via GitHub Actions + GoReleaser when a `v*.*.*` tag is pushed:

```bash
git tag v1.0.0
git push origin v1.0.0
```

The release workflow builds binaries for linux/darwin/windows (amd64/arm64), creates Homebrew formula, deb/rpm/apk packages, and publishes a GitHub release.

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.
