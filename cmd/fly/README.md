# `ff` — FunctionFly Developer CLI

The `ff` CLI is the primary developer interface for FunctionFly.

## Install and upgrade

- **Install script (Linux/macOS):**  
  `curl -fsSL https://raw.githubusercontent.com/functionfly/ff-cli/main/scripts/install.sh | bash`
- **Homebrew:** `brew tap functionfly/homebrew-tap && brew install ff` (when tap is configured)
- **From source:** `make build` (binary at `bin/ff`)

Upgrade: run the install script again with `VERSION=latest`, or `brew upgrade ff` / `scoop update ff` / `choco upgrade ff`. Or run `ff self-update` to print instructions.

## Quick Start

```bash
ff login                   # Authenticate
ff init my-function        # Scaffold a new function
cd my-function
ff dev                     # Run locally at http://localhost:8787
ff publish                 # Publish to the global registry
ff test                    # Test the deployed function
```

## Configuration

Precedence (highest first):

1. **Environment variables** (`FF_*`)
2. **Global config file** `~/.ff/config.yaml`
3. **Defaults**

| Variable | Description |
|----------|-------------|
| `FF_API_URL` | API base URL (e.g. `https://api.functionfly.com` or `http://localhost:8080`) |
| `FF_API_TIMEOUT` | Request timeout (e.g. `30s`) |
| `FF_DEV_EMAIL` / `FF_DEV_PASSWORD` | Dev login (with `ff login --dev`) |
| `FF_DEV_LOGIN=1` | Force dev email/password login |
| `FF_TOKEN` | Bearer token (overrides stored credentials) |
| `FF_TELEMETRY` | Set to `0`, `false`, or `no` to disable telemetry |
| `FF_CONFIG` | Path to config file (overrides `~/.ff/config.yaml`) |

- View current config: `ff config` or `ff config view`
- Reset to defaults: `ff config reset`

Credentials (after login) are stored in `~/.ff/credentials.json`.

## Commands

### Core

| Command | Description |
|---------|-------------|
| `ff login` | OAuth login (GitHub or Google) |
| `ff whoami` | Show current user |
| `ff logout` | Clear credentials |
| `ff config` | View or reset global config |
| `ff self-update` | Print upgrade instructions |
| `ff init <name>` | Scaffold a new function project |
| `ff dev` | Run locally with hot reload |
| `ff publish` | Publish to registry |
| `ff publish --build` | Build then publish |
| `ff test` | Test deployed function |
| `ff update patch` | Bump function version (patch/minor/major) |
| `ff rollback` | Roll back to previous version |

### Deployment

| Command | Description |
|---------|-------------|
| `ff deploy --env production` | Publish and set as production |
| `ff deploy --env staging` | Publish and set as staging |
| `ff deploy --promote staging→prod` | Promote staging version to production |
| `ff deploy --canary 10` | Publish and start canary at 10% traffic |
| `ff canary` | Manage canary deployments |
| `ff health` | Check deployed function health |
| `ff stats` | View usage statistics |
| `ff analytics` | Rich analytics with period comparison |
| `ff exec-history` | View past function executions |
| `ff logs` | View recent logs |
| `ff logs --follow` | Stream live logs |
| `ff logs --level error` | Filter by log level |
| `ff logs --request-id abc` | Filter by request ID |

### Function Management

| Command | Description |
|---------|-------------|
| `ff list` | List deployed functions |
| `ff search [query]` | Search the public function registry |
| `ff run <author/name>` | Execute a deployed function |
| `ff delete <author/name>` | Delete a deployed function |
| `ff diff` | Compare local vs deployed state |
| `ff function info` | Detailed function information |
| `ff embed` | Generate SDK code snippets |
| `ff dna` | View function DNA, mutations, variants |
| `ff trust` | View trust scores and verify integrity |
| `ff time-machine` | Replay and inspect past states |

### Configuration & Secrets

| Command | Description |
|---------|-------------|
| `ff env list/set/get/unset` | Manage environment variables |
| `ff env apply` | Set env vars from a .env file |
| `ff env import` | Import env vars from JSON/shell format |
| `ff secrets list/set/unset` | Manage secrets |
| `ff state list/get/set/delete` | Manage function state KV store |
| `ff schedule` | Manage scheduled executions |
| `ff vault` | Manage the encrypted secrets vault |

### Account & Billing

| Command | Description |
|---------|-------------|
| `ff user show/update/settings` | Manage user profile |
| `ff billing show/upgrade/usage` | Manage billing and plan |
| `ff apps list/create/get/delete` | Manage applications |
| `ff api-keys list/create/rotate/revoke` | Manage API keys |
| `ff notify list/create/delete/test` | Manage webhook notifications |

### Utilities

| Command | Description |
|---------|-------------|
| `ff completion bash/zsh/fish/powershell` | Shell completion |
| `ff doctor` | Run environment diagnostics |
| `ff changelog` | Show the CLI changelog |

## JSON Output

All commands support `--json` for CI/CD:

```bash
ff publish --json
ff stats --json
ff whoami --json
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--debug` | Enable full debug output |
| `--verbose` / `-v` | Enable verbose API calls |
| `--trace` | Enable HTTP trace with request/response bodies |
| `--format` / `-m` | Output format: `table`, `json` (default: `table`) |
| `--yes` / `-y` | Skip confirmation prompts |
| `--version` | Show CLI version |
