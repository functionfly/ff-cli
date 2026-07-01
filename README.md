# ff-cli — FunctionFly CLI

> Go from idea to global API in under 60 seconds.

`ff` is the official command-line tool for [FunctionFly](https://functionfly.com). Write a function, publish it, and call it from anywhere — no infra required.

## Install

**macOS / Linux (one-liner)**
```bash
curl -fsSL https://raw.githubusercontent.com/functionfly/fly/main/scripts/install.sh | bash
```

**Homebrew**
```bash
brew tap functionfly/tap
brew install ff
```

**Windows (PowerShell)**
```powershell
iwr -useb https://raw.githubusercontent.com/functionfly/fly/main/scripts/install.ps1 | iex
```

**Download directly** — see [Releases](https://github.com/functionfly/fly/releases)

---

## Quick start

```bash
# Log in
ff login

# Scaffold a new function
ff init slugify

# Run it locally
cd slugify && ff dev

# Test it
ff test

# Publish to the registry
ff publish

# Deploy to production
ff deploy --env production
```

Your function is now live at `https://api.functionfly.com/fx/<you>/slugify`.

---

## Commands

### Core

| Command | Description |
|---------|-------------|
| `ff login` | Authenticate with FunctionFly (OAuth) |
| `ff logout` | Clear stored credentials |
| `ff whoami` | Show the current authenticated user |
| `ff init <name>` | Scaffold a new function project |
| `ff dev` | Run the function locally with hot reload |
| `ff test` | Run tests against the local runtime |
| `ff publish` | Publish the function to the registry |
| `ff publish-batch` | Batch publish multiple functions |
| `ff update` | Bump the function version |
| `ff rollback` | Roll back to a previous version |

### Deployment

| Command | Description |
|---------|-------------|
| `ff deploy` | Publish and promote to any environment (`--env`, `--canary`, `--promote`, `--env-file`) |
| `ff canary` | Manage canary deployments (`start`, `status`, `promote`, `rollback`, `cancel`, `history`) |
| `ff health` | Check deployed function health (supports `--watch`) |
| `ff logs` | Stream live execution logs (`--follow`, `--level`, `--request-id`, `--since`) |
| `ff stats` | View invocation stats |
| `ff analytics` | Rich analytics with period comparison and dimension breakdown |
| `ff exec-history` | View past function executions with status filtering |

### Function Management

| Command | Description |
|---------|-------------|
| `ff list` | List deployed functions (`--all`) |
| `ff search [query]` | Search the public function registry (`--runtime`, `--limit`) |
| `ff run <author/name>` | Execute a deployed function (`--input`, `--file`, `--method`, `--header`) |
| `ff delete <author/name>` | Delete a deployed function |
| `ff diff` | Compare local vs deployed state (`--env`) |
| `ff function info` | Detailed function information |
| `ff embed` | Generate SDK code snippets (`--lang`, `--method`) |
| `ff dna` | View function DNA, mutations, and variants |
| `ff trust` | View trust scores and verify integrity |
| `ff time-machine` | Replay and inspect past function states (`list`, `replay`, `diff`, `inspect`) |

### Configuration & Secrets

| Command | Description |
|---------|-------------|
| `ff config` | Show/edit CLI configuration |
| `ff env` | Manage environment variables (`list`, `set`, `get`, `unset`, `apply`, `import`) |
| `ff secrets` | Manage per-function secrets (`list`, `set`, `unset`) |
| `ff vault` | Manage the encrypted secrets vault (see below) |
| `ff state` | Manage function state KV store (`list`, `get`, `set`, `delete`, `clear`, `export`, `import`) |
| `ff schedule` | Manage scheduled function executions (`set`, `list`, `get`, `remove`, `presets`, `trigger`) |

### Account & Billing

| Command | Description |
|---------|-------------|
| `ff user` | Manage user profile (`show`, `update`, `settings`) |
| `ff billing` | Manage billing and plan (`show`, `upgrade`, `downgrade`, `usage`) |
| `ff apps` | Manage applications (`list`, `create`, `get`, `update`, `delete`) |
| `ff api-keys` | Manage API keys for CI/CD (`list`, `create`, `rotate`, `revoke`) |
| `ff notify` | Manage webhook notifications (`list`, `create`, `update`, `delete`, `test`) |

### Advanced

| Command | Description |
|---------|-------------|
| `ff compile` | Compile functions to various formats (`python`, `rust`) |
| `ff flypy` | FlyPy — Deterministic Python Compiler (`build`, `deploy`, `local`) |
| `ff dre` | DRE (Deterministic Reliable Execution) and FXCERT operations |
| `ff backend` | Manage execution backends (`add`, `list`, `remove`) |
| `ff manifest ensure-descriptions` | Add descriptions to functionfly.jsonc files |

### Utilities

| Command | Description |
|---------|-------------|
| `ff completion` | Generate shell completions (`bash`, `zsh`, `fish`, `powershell`) |
| `ff doctor` | Run environment diagnostics |
| `ff self-update` | Update the CLI itself |
| `ff changelog` | Show the CLI changelog |

### Vault

The `ff vault` command provides access to the FunctionFly encrypted secrets vault — a zero-knowledge, client-side encrypted secrets management system with enterprise features.

| Command | Description |
|---------|-------------|
| `ff vault secrets` | Manage vault secrets (`list`, `create`, `get`, `update`, `delete`, `rotate`, `bulk-delete`, `export`) |
| `ff vault tokens` | Manage access tokens (`create`, `list`, `revoke`) |
| `ff vault versions` | Manage secret versions (`list`, `get`, `diff`, `rollback`) |
| `ff vault audit` | Query audit logs (`list`, `export`) |
| `ff vault namespaces` | Organize secrets into namespaces (`list`, `create`, `delete`) |
| `ff vault dynamic` | Dynamic secrets — on-demand database credentials (`targets`, `credentials`, `leases`) |
| `ff vault shares` | Cross-tenant secret sharing (`create`, `list`, `revoke`) |
| `ff vault rbac` | Role-based access control (`roles`, `assignments`, `assign`, `unassign`) |
| `ff vault config` | Security configuration (`mfa`, `sso`, `break-glass`, `escrow`, `cache`) |

### Global flags

All commands support:
- `--debug` — Enable full debug output
- `--verbose` / `-v` — Enable verbose API calls
- `--trace` — Enable HTTP trace with request/response bodies
- `--format` / `-m` — Output format: `table`, `json` (default: `table`)
- `--yes` / `-y` — Skip confirmation prompts
- `--version` — Show CLI version

---

## Plan tiers

Certain features require a paid plan. The CLI checks your plan locally and shows a clear upgrade message if you try to access a feature above your tier. Run `ff whoami` to see your current plan.

| Feature | Free | Starter ($24/mo) | Professional ($79/mo) | Enterprise ($299/mo) |
|---------|------|-------------------|-----------------------|----------------------|
| Publish, deploy, test, logs (fetch), env, secrets | ✅ | ✅ | ✅ | ✅ |
| Scheduled executions (`ff schedule set`) | — | ✅ | ✅ | ✅ |
| Live log streaming (`ff logs --follow`) | — | ✅ | ✅ | ✅ |
| Canary deployments (`ff canary`) | — | — | ✅ | ✅ |
| Vault namespaces, MFA, break-glass, audit export | — | — | ✅ | ✅ |
| Vault RBAC, shares, escrow, SIEM webhooks | — | — | — | ✅ |
| Vault SSO | — | — | — | — (Agent Enterprise) |

Numeric limits per plan:

| Resource | Free | Starter | Professional | Enterprise |
|----------|------|---------|--------------|------------|
| Apps | 1 | 3 | 10 | Unlimited |
| Functions | 3 | 5 | 25 | Unlimited |
| Requests/month | 25K | 250K | 2.5M | 25M |
| Vault secrets | 25 | 500 | 5,000 | 1,000,000 |
| Tokens per secret | 5 | 25 | 100 | 1,000 |

Upgrade at [functionfly.com/billing](https://functionfly.com/billing).

---

## Configuration

The CLI reads config from (in order of precedence):

1. Environment variables (prefix `FF_`)
2. `functionfly.jsonc` in the current directory
3. `~/.ff/config.yaml`

### Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `FF_API_URL` | `https://api.functionfly.com` | API endpoint |
| `FF_TOKEN` | — | Auth token (skips `ff login`) |
| `FF_CONFIG` | `~/.ff/config.yaml` | Config file path |

### functionfly.jsonc

The function manifest uses JSONC format (JSON with comments):

```jsonc
{
  "name": "my-function",           // required, lowercase + hyphens
  "version": "1.0.0",              // required, semver
  "runtime": "node20",             // node18, node20, python3.11, deno, bun, rust, browser-wasm
  "entry": "index.ts",             // optional, auto-detected
  "public": true,                  // default: true
  "deterministic": false,          // default: false
  "cache_ttl": 3600,               // default: 3600, max: 86400
  "timeout_ms": 5000,              // default: 5000, max: 30000
  "memory_mb": 128,                // 128, 256, 512, or 1024
  "description": "My function",    // max 500 chars
  "dependencies": {},              // npm/python deps
  "env": {},                       // runtime env vars
  "typeCheck": true,               // TypeScript type checking
  "tsConfig": "tsconfig.json",     // custom tsconfig path
  "strictMode": false,             // strict TypeScript
  "skipTypeCheck": false           // skip type checking
}
```

---

## Development

**Requirements:** Go ≥ 1.25

```bash
# Clone
git clone https://github.com/functionfly/ff-cli.git
cd ff-cli

# Build
make build

# Test
make test

# Lint
make lint

# Security
make security

# Run locally (no install)
./bin/ff --help
```

### Project structure

```
cmd/fly/           # CLI entry point and commands
internal/
├── bundler/       # TypeScript/JS/Python/WASM bundling
├── cli/           # HTTP client and config
├── credentials/   # Credential persistence (OS keychain)
├── flypy/         # FlyPy Python-to-Wasm compiler
├── manifest/      # functionfly.jsonc parser
├── telemetry/     # Telemetry and event tracking
├── testing/       # Test runner and validator
├── version/       # Build version injection
└── watcher/       # File watcher for hot reload
scripts/           # Install scripts (bash, PowerShell)
```

### Release process

Tags trigger the GoReleaser workflow automatically:

```bash
git tag v1.1.0
git push origin v1.1.0
```

GoReleaser produces:
- Linux/macOS/Windows binaries (amd64 + arm64)
- `.deb`, `.rpm`, `.apk` packages
- Homebrew formula update
- GitHub Release with changelog

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

---

## License

[Apache 2.0](LICENSE)
