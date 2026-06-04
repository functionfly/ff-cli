# `ff` — FunctionFly Developer CLI

The `ff` CLI is the primary developer interface for FunctionFly.

## Install and upgrade

- **Install script (Linux/macOS):**  
  `curl -fsSL https://raw.githubusercontent.com/functionfly/functionfly/main/scripts/install.sh | bash`
- **Homebrew:** `brew tap functionfly/tap && brew install ff` (when tap is configured)
- **From source:** `go build -o bin/ff ./cmd/ff` (binary at `bin/ff`)

Upgrade: run the install script again with `VERSION=latest`, or `brew upgrade ff` / `scoop update ff` / `choco upgrade ff`. Or run `ff self-update` to print instructions.

See [packaging/README.md](../../packaging/README.md) for Windows and release artifacts.

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
| `ff stats` | View usage statistics |
| `ff logs` | View recent logs |
| `ff logs --follow` | Stream live logs |
| `ff rollback` | Roll back to previous version |
| `ff env list/set/get/unset` | Manage environment variables |
| `ff secrets list/set/unset` | Manage secrets |
| `ff completion bash/zsh/fish/powershell` | Shell completion |

## JSON Output

All commands support `--json` for CI/CD:

```bash
ff publish --json
ff stats --json
ff whoami --json
```
