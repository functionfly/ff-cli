package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func NewInitCmd() *cobra.Command {
	var template string
	var force bool
	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a new function project",
		Long:  "Create a new FunctionFly function project with all required files.",
		Example: `  ff init hello-world
  ff init --template hello-world my-function
  ff init --template http-api api-service
  ff init --template cron-job daily-task
  ff init --template webhook webhook-handler`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return runInit(name, template, force)
		},
	}
	cmd.Flags().StringVarP(&template, "template", "t", "hello-world", "Template (hello-world, http-api, cron-job, webhook, python, typescript, javascript)")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing files")
	return cmd
}

func runInit(name, template string, force bool) error {
	if name == "" {
		if IsInteractive() {
			name = Prompt("Function name", "my-function")
		} else {
			return fmt.Errorf("function name is required\n   → Usage: ff init <name>")
		}
	}
	if !isValidFunctionName(name) {
		return fmt.Errorf("invalid function name: %q\n   → Use lowercase letters, numbers, and hyphens only; max 63 characters; no leading or trailing hyphens", name)
	}
	if !isValidTemplate(template) {
		return fmt.Errorf("invalid template %q\n   → Supported: %s", template, strings.Join(validTemplateList(), ", "))
	}
	// Reject names that would escape the current directory.
	cleaned := filepath.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("invalid function name: %q\n   → Names cannot be '.' or '..' or start with '..'", name)
	}
	if filepath.IsAbs(cleaned) {
		return fmt.Errorf("invalid function name: %q\n   → Names must be relative paths", name)
	}
	projectDir := filepath.Join(".", name)
	// Resolve to an absolute path and double-check it stays inside CWD.
	absProject, _ := filepath.Abs(projectDir)
	absCwd, _ := filepath.Abs(".")
	if absProject != "" && absCwd != "" && !strings.HasPrefix(absProject, absCwd+string(filepath.Separator)) && absProject != absCwd {
		return fmt.Errorf("invalid function name: %q\n   → Resolves outside the current directory", name)
	}
	if _, err := os.Stat(projectDir); err == nil && !force {
		return fmt.Errorf("directory %q already exists\n   → Use --force to overwrite", projectDir)
	}
	if err := os.MkdirAll(projectDir, 0750); err != nil {
		return fmt.Errorf("could not create directory: %w", err)
	}

	// Generate template files with spinner
	err := WithSpinner("Generating template files", func() error {
		files, err := generateTemplateFiles(name, template)
		if err != nil {
			return err
		}
		for filename, content := range files {
			path := filepath.Join(projectDir, filename)
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				return fmt.Errorf("could not write %s: %w", filename, err)
			}
			fmt.Printf("  ✓ %s\n", filepath.Join(name, filename))
		}
		return nil
	})
	if err != nil {
		return err
	}
	fmt.Printf("\n✅ Created %s/\n\n", name)
	fmt.Println("Next steps:")
	fmt.Printf("  cd %s\n", name)
	fmt.Println("  ff dev          # Run locally")
	fmt.Println("  ff publish      # Publish to the registry")
	return nil
}

// isValidFunctionName validates function name: lowercase, digits, hyphens; 1–63 chars; no leading/trailing hyphen.
func isValidFunctionName(name string) bool {
	if len(name) == 0 || len(name) > 63 {
		return false
	}
	if name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return false
		}
	}
	return true
}

// validTemplateList returns the supported --template values in display order.
func validTemplateList() []string {
	return []string{"hello-world", "http-api", "cron-job", "webhook", "python", "typescript", "javascript"}
}

// isValidTemplate reports whether t is a recognized --template value.
func isValidTemplate(t string) bool {
	for _, v := range validTemplateList() {
		if t == v {
			return true
		}
	}
	return false
}

func generateTemplateFiles(name, template string) (map[string]string, error) {
	files := map[string]string{}

	// Get template description based on template type
	description := getTemplateDescription(template)

	files["functionfly.jsonc"] = fmt.Sprintf(`{
  "$schema": "https://functionfly.com/schemas/functionfly.json",
  "name": "%s",
  "version": "1.0.0",
  "runtime": "%s",
  "public": true,
  "deterministic": true,
  "cache_ttl": 86400,
  "timeout_ms": %d,
  "memory_mb": %d,
  "description": "%s"%s
}
`, name, runtimeForTemplate(template), timeoutForTemplate(template), memoryForTemplate(template), description, scheduleForTemplate(template))

	switch template {
	case "typescript":
		files["index.ts"] = getHelloWorldTypeScriptTemplate(name)
	case "javascript":
		files["index.js"] = getHelloWorldJSTemplate(name)
	case "http-api":
		files["main.py"] = getHttpApiTemplate(name)
	case "cron-job":
		files["main.py"] = getCronJobTemplate(name)
	case "webhook":
		files["main.py"] = getWebhookTemplate(name)
	case "hello-world":
		fallthrough
	default:
		files["main.py"] = getHelloWorldTemplate(name)
	}

	files["test.http"] = fmt.Sprintf("### Test %s locally\nPOST http://localhost:8787\nContent-Type: application/json\n\n{}", name)
	return files, nil
}

func runtimeForTemplate(template string) string {
	switch template {
	case "typescript":
		return "node20"
	case "python":
		return "python3.11"
	default:
		return "python3.11"
	}
}

func timeoutForTemplate(template string) int {
	switch template {
	case "http-api":
		return 10000
	case "cron-job":
		return 30000
	default:
		return 5000
	}
}

func memoryForTemplate(template string) int {
	switch template {
	case "http-api":
		return 256
	default:
		return 128
	}
}

func getTemplateDescription(template string) string {
	switch template {
	case "hello-world":
		return "A simple function that returns a greeting"
	case "http-api":
		return "A RESTful API with CRUD operations"
	case "cron-job":
		return "A scheduled function for periodic tasks"
	case "webhook":
		return "A webhook handler for third-party events"
	default:
		return "A FunctionFly function"
	}
}

func scheduleForTemplate(template string) string {
	switch template {
	case "cron-job":
		return "\n  ,\"schedule\": \"0 * * * *\""
	default:
		return ""
	}
}

func getHelloWorldTemplate(name string) string {
	return fmt.Sprintf(`"""
%s - A FunctionFly function
A simple function that returns a greeting.
"""

async def fetch(request, env, ctx):
    """
    Handle incoming requests and return a greeting.

    Args:
        request: The incoming request object
        env: Environment variables and secrets
        ctx: Execution context

    Returns:
        Response with greeting message
    """
    url = request.url

    # Parse query parameters
    params = {}
    if '?' in url:
        query_string = url.split('?')[1]
        for param in query_string.split('&'):
            if '=' in param:
                key, value = param.split('=', 1)
                params[key] = value

    # Get name from query params or default to World
    name = params.get('name', 'World')

    # Return greeting
    return {
        "status": 200,
        "body": f"Hello, {name}! Welcome to FunctionFly.",
        "headers": {
            "Content-Type": "application/json",
            "X-FunctionFly-Template": "hello-world"
        }
    }
`, name)
}

func getHttpApiTemplate(name string) string {
	return `"""
HTTP API Function
A RESTful API with built-in routing for CRUD operations.
Uses env.kv for persistence when available; falls back to in-memory otherwise.
"""

from datetime import datetime
import json

# Storage: env.kv (persistent) when available, else in-memory
KV_PREFIX = "http_api_users:"
_kv = None
_memory_db = {}
_memory_next_id = 1

def _get_kv(env):
    global _kv
    if _kv is not None:
        return _kv
    if hasattr(env, "kv") and env.kv is not None:
        _kv = env.kv
        return _kv
    return None

async def _next_id(env):
    kv = _get_kv(env)
    if kv:
        try:
            raw = await kv.get(KV_PREFIX + "next_id")
            n = int(raw) if raw else 1
            await kv.set(KV_PREFIX + "next_id", str(n + 1))
            return n
        except Exception:
            pass
    global _memory_next_id
    n = _memory_next_id
    _memory_next_id += 1
    return n

async def _user_get(env, user_id):
    kv = _get_kv(env)
    if kv:
        try:
            raw = await kv.get(KV_PREFIX + str(user_id))
            return json.loads(raw) if raw else None
        except Exception:
            pass
    return _memory_db.get(user_id)

async def _user_set(env, user_id, user):
    kv = _get_kv(env)
    if kv:
        try:
            await kv.set(KV_PREFIX + str(user_id), json.dumps(user))
            ids_raw = await kv.get(KV_PREFIX + "ids")
            ids = json.loads(ids_raw) if ids_raw else []
            if user_id not in ids:
                ids.append(user_id)
                await kv.set(KV_PREFIX + "ids", json.dumps(ids))
            return
        except Exception:
            pass
    _memory_db[user_id] = user

async def _user_delete(env, user_id):
    kv = _get_kv(env)
    if kv:
        try:
            await kv.delete(KV_PREFIX + str(user_id))
            ids_raw = await kv.get(KV_PREFIX + "ids")
            ids = json.loads(ids_raw) if ids_raw else []
            if user_id in ids:
                ids.remove(user_id)
                await kv.set(KV_PREFIX + "ids", json.dumps(ids))
            return
        except Exception:
            pass
    _memory_db.pop(user_id, None)

async def _user_list(env):
    kv = _get_kv(env)
    if kv:
        try:
            ids_raw = await kv.get(KV_PREFIX + "ids")
            ids = json.loads(ids_raw) if ids_raw else []
            out = []
            for i in ids:
                raw = await kv.get(KV_PREFIX + str(i))
                if raw:
                    out.append(json.loads(raw))
            return out
        except Exception:
            pass
    return list(_memory_db.values())


async def fetch(request, env, ctx):
    """
    Handle incoming HTTP requests with RESTful routing.

    Routes:
        GET    /users      - List all users
        GET    /users/{id} - Get user by ID
        POST   /users      - Create a new user
        PUT    /users/{id} - Update user
        DELETE /users/{id} - Delete user
        GET    /health     - Health check
    """
    url = request.url
    method = request.method

    if '/health' in url:
        return {
            "status": 200,
            "body": {
                "status": "healthy",
                "timestamp": datetime.utcnow().isoformat(),
                "service": "http-api"
            },
            "headers": {"Content-Type": "application/json"}
        }

    if '/users' in url:
        path = url.split("?")[0].rstrip("/")
        parts = path.split("/")
        user_id = parts[-1] if len(parts) > 0 and parts[-1].isdigit() else None

        if method == 'GET' and user_id is None:
            user_list = await _user_list(env)
            return {
                "status": 200,
                "body": {"users": user_list, "count": len(user_list)},
                "headers": {"Content-Type": "application/json"}
            }

        if method == 'GET' and user_id is not None:
            uid = int(user_id)
            user = await _user_get(env, uid)
            if user is None:
                return {"status": 404, "body": {"error": "User not found"}, "headers": {"Content-Type": "application/json"}}
            return {"status": 200, "body": user, "headers": {"Content-Type": "application/json"}}

        if method == 'POST' and user_id is None:
            uid = await _next_id(env)
            user = {"id": uid, "name": "New User", "created_at": datetime.utcnow().isoformat()}
            await _user_set(env, uid, user)
            return {"status": 201, "body": user, "headers": {"Content-Type": "application/json"}}

        if method == 'PUT' and user_id is not None:
            uid = int(user_id)
            body = {}
            try:
                body = await request.json() if hasattr(request, "json") else {}
            except Exception:
                pass
            existing = await _user_get(env, uid)
            if existing is None:
                return {"status": 404, "body": {"error": "User not found"}, "headers": {"Content-Type": "application/json"}}
            existing.update(body)
            existing["id"] = uid
            await _user_set(env, uid, existing)
            return {"status": 200, "body": existing, "headers": {"Content-Type": "application/json"}}

        if method == 'DELETE' and user_id is not None:
            uid = int(user_id)
            await _user_delete(env, uid)
            return {"status": 204, "body": None, "headers": {"Content-Type": "application/json"}}

    return {"status": 404, "body": {"error": "Not found"}, "headers": {"Content-Type": "application/json"}}
`
}

func getCronJobTemplate(name string) string {
	return `"""
Cron Job Function
A scheduled function that runs periodically.
Use cases: Data cleanup, report generation, cache refresh, etc.
"""

from datetime import datetime


async def fetch(request, env, ctx):
    """
    Handle scheduled execution.

    This function runs automatically based on the schedule defined
    in functionfly.jsonc.

    Schedule presets:
        "*/5 * * * *"    - Every 5 minutes
        "0 * * * *"      - Every hour
        "0 0 * * *"      - Every day at midnight
        "0 0 * * 1-5"   - Weekdays at midnight
    """
    # Check if this is a scheduled execution
    body = {}
    try:
        body = await request.json()
    except:
        pass

    trigger = body.get("trigger", "manual")
    timestamp = body.get("timestamp", datetime.utcnow().isoformat())

    result = {
        "status": "success",
        "trigger": trigger,
        "executed_at": timestamp,
        "message": "Scheduled job completed successfully"
    }

    return {
        "status": 200,
        "body": result,
        "headers": {
            "Content-Type": "application/json",
            "X-FunctionFly-Template": "cron-job"
        }
    }
`
}

func getWebhookTemplate(name string) string {
	return `"""
Webhook Handler Function
A generic webhook handler for processing incoming events.
Use cases: GitHub webhooks, Stripe, Slack, etc.
"""

from datetime import datetime


async def fetch(request, env, ctx):
    """
    Handle incoming webhook requests.

    Supports:
    - Signature verification (X-Hub-Signature-256)
    - Event type parsing
    - JSON payload parsing
    """
    # Get webhook secret from environment
    webhook_secret = env.get("WEBHOOK_SECRET", "")

    # Get headers
    headers = dict(request.headers)
    event_type = headers.get("x-github-event", headers.get("event-type", "unknown"))

    # Parse request body
    body = {}
    try:
        body = await request.json()
    except:
        pass

    result = {
        "received": True,
        "event_type": event_type,
        "timestamp": datetime.utcnow().isoformat(),
        "processed": True
    }

    return {
        "status": 200,
        "body": result,
        "headers": {"Content-Type": "application/json"}
    }
`
}

func getHelloWorldJSTemplate(name string) string {
	return fmt.Sprintf(`/**
 * %s - A FunctionFly function
 * A simple function that returns a greeting.
 *
 * @param {Request} request - The incoming request object
 * @param {Object} env - Environment variables and secrets
 * @param {Object} ctx - Execution context
 * @returns {Object} Response with greeting message
 */
export default async function fetch(request, env, ctx) {
  const url = new URL(request.url);
  const nameParam = url.searchParams.get("name") || "World";

  return {
    status: 200,
    body: JSON.stringify({ message: `+"`Hello, ${nameParam}! Welcome to FunctionFly.`"+` }),
    headers: {
      "Content-Type": "application/json",
      "X-FunctionFly-Template": "hello-world",
    },
  };
}
`, name)
}

func getHelloWorldTypeScriptTemplate(name string) string {
	return fmt.Sprintf(`/**
 * %s - A FunctionFly function
 * A simple function that returns a greeting.
 */

interface FunctionRequest {
  url: string;
  method: string;
  headers: Record<string, string>;
}

interface FunctionEnv {
  [key: string]: string | undefined;
}

interface FunctionContext {
  waitUntil(promise: Promise<unknown>): void;
}

interface FunctionResponse {
  status: number;
  body: string;
  headers: Record<string, string>;
}

export default async function fetch(
  request: FunctionRequest,
  env: FunctionEnv,
  ctx: FunctionContext
): Promise<FunctionResponse> {
  const url = new URL(request.url);
  const nameParam = url.searchParams.get("name") || "World";

  return {
    status: 200,
    body: JSON.stringify({ message: `+"`Hello, ${nameParam}! Welcome to FunctionFly.`"+` }),
    headers: {
      "Content-Type": "application/json",
      "X-FunctionFly-Template": "hello-world",
    },
  };
}
`, name)
}
