# Changelog

## v1.1.1 — 2026-07-24

### New Commands

- **`ff implant`** — Function implantation system for embedding and managing function implants:
  - `build` — Build an implant from source
  - `diff` — Compare implant versions
  - `init` — Initialize a new implant
  - `list` — List available implants
  - `publish` — Publish an implant
  - `sign` — Sign an implant
  - `test` — Test an implant
  - `validate` — Validate an implant manifest

### Bug Fixes

- Fixed homebrew tap URL from `functionfly/tap` to `functionfly/homebrew-tap`
- Fixed GitHub URLs from `functionfly/fly` to `functionfly/ff-cli`

---

## v1.1.0 — 2026-07-01

### New Commands

- **`ff list`** — List all deployed functions with `--all` flag
- **`ff search`** — Search the public function registry by keyword, runtime, or tag
- **`ff run`** — Execute a deployed function directly from the CLI (`--input`, `--file`, `--method`, `--header`)
- **`ff delete`** — Delete a deployed function with confirmation prompt
- **`ff diff`** — Compare local source against the deployed version (`--env`)
- **`ff function info`** — Show detailed function metadata and configuration
- **`ff analytics`** — Rich analytics with period comparison and dimension breakdown (`--period`, `--compare`, `--dimension`)
- **`ff exec-history`** — View past function executions with status and latency filtering
- **`ff embed`** — Generate SDK code snippets for calling a function in multiple languages (`--lang`)
- **`ff dna`** — View function DNA, mutations, and variant lineage
- **`ff trust`** — View trust scores and verify function integrity
- **`ff time-machine`** — Replay and inspect past function states (`list`, `replay`, `diff`, `inspect`)
- **`ff state`** — Function state KV store with full CRUD (`list`, `get`, `set`, `delete`, `clear`, `export`, `import`)
- **`ff user`** — Manage user profile (`show`, `update`, `settings`)
- **`ff billing`** — Manage billing and plan (`show`, `upgrade`, `downgrade`, `usage`)
- **`ff apps`** — Manage applications (`list`, `create`, `get`, `update`, `delete`)
- **`ff api-keys`** — Manage API keys for CI/CD (`list`, `create`, `rotate`, `revoke`)
- **`ff notify`** — Webhook notification management (`list`, `create`, `update`, `delete`, `test`)

### Enhanced Commands

- **`ff deploy`** — First-class environment management:
  - `--env <name>` — Deploy to any named environment (staging, production, dev, qa, preview, etc.)
  - `--promote <from>→<to>` — Promote a version from one environment to another
  - `--env-file <path>` — Inject environment-specific variables during deployment
  - `--canary <N>` — Start a canary deployment at N% traffic
  - `--dry-run` — Validate and bundle without publishing
  - `--skip-type-check` — Skip TypeScript type checking
- **`ff env`** — New subcommands:
  - `apply` — Set environment variables from a `.env` file (`--dry-run`)
  - `import` — Import variables from JSON or shell format files
- **`ff logs`** — New filtering options:
  - `--level` — Filter by log level (info, warn, error)
  - `--request-id` — Filter by specific request ID
  - `--function` — Filter by function name
  - `--since` / `--until` — Time range filtering
- **`ff login`** — Simplified OAuth flow (removed invite-code requirement)

### Vault (Enterprise Secrets Management)

The encrypted secrets vault now includes 9 subcommand groups with 40+ operations:

- **`ff vault secrets`** — Full secret lifecycle (create, rotate, bulk-delete, export)
- **`ff vault tokens`** — Access token management for secrets
- **`ff vault versions`** — Secret version history with diff and rollback
- **`ff vault audit`** — Audit log querying and export (JSON, CSV, CEF)
- **`ff vault namespaces`** — Organize secrets into hierarchical namespaces
- **`ff vault dynamic`** — On-demand database credentials with lease management
- **`ff vault shares`** — Cross-tenant secret sharing with permission controls
- **`ff vault rbac`** — Role-based access control for vault operations
- **`ff vault config`** — Security configuration (MFA, SSO, break-glass, escrow, cache)

### Global Changes

- Added `--yes` / `-y` flag to skip confirmation prompts on all commands
- Changed short flag for `--format` from `-o` to `-m` across all commands
- All new commands support `--json` output for CI/CD integration
- Plan-tier gating for premium features with clear upgrade messages

### Bug Fixes

- Fixed `ff login` OAuth flow — switched from POST to GET for `/auth/oauth/url`
- Fixed install script handling goreleaser-wrapped directory structure
- Fixed backend codegen bugs causing 7 integration test failures
- Replaced deprecated `wasm32-wasi` target with `wasm32-wasip1` in CI
- Fixed Windows test compatibility for permissions, timeout, and credentials
- Fixed install script URL construction for darwin/windows archives
- Simplified CI pipeline — direct installation of golangci-lint and goreleaser
- Updated GitHub Actions for Node 24 support

### Infrastructure

- Added `install.sh` script for homebrew/homebrew-tap installation
- Restored brews configuration for homebrew-tap publishing
- Added native bundler for improved build performance
- Expanded dependency detection in the bundler system

---

## v1.0.0 — 2026-05-15

Initial release of the FunctionFly CLI.

- OAuth authentication (GitHub/Google)
- Function scaffolding, local dev, publish, deploy
- Environment variables and secrets management
- Canary deployments with traffic splitting
- Live log streaming and execution stats
- Encrypted secrets vault with enterprise features
- Scheduled function executions
- FlyPy deterministic Python compiler
- WASM bundling for JS/TS, Python, Rust
- Cross-platform binaries (Linux, macOS, Windows)
- Homebrew, deb, rpm, apk packages
