package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// pythonExecutor invokes a Python handler (`async def fetch(request, env, ctx)`)
// in a subprocess. It is the dev-server-side counterpart to the WASM runtime
// used in production; on WSL/Linux where Python 3 is installed, it lets
// `ff dev` actually run the user's function while iterating.
type pythonExecutor struct {
	manifest *Manifest
	funcFile string
	timeout  time.Duration
}

func newPythonExecutor(manifest *Manifest, funcFile string) *pythonExecutor {
	timeout := 5 * time.Second
	if manifest != nil && manifest.TimeoutMS > 0 {
		timeout = time.Duration(manifest.TimeoutMS) * time.Millisecond
	}
	return &pythonExecutor{manifest: manifest, funcFile: funcFile, timeout: timeout}
}

// requestEnvelope is the JSON object we hand to the Python handler.
type requestEnvelope struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    string              `json:"body,omitempty"`
	Query   map[string]string   `json:"query,omitempty"`
}

// responseEnvelope is the JSON object we expect back from the Python handler.
type responseEnvelope struct {
	Status  int               `json:"status"`
	Body    interface{}       `json:"body"`
	Headers map[string]string `json:"headers,omitempty"`
	Error   string            `json:"error,omitempty"`
	Stack   string            `json:"stack,omitempty"`
	Other   map[string]any    `json:"-"`
}

// executorResult is the wire-format return of execute().
type executorResult struct {
	Status  int
	Body    []byte
	Headers http.Header
}

// Execute runs the user's Python handler with the given request and returns
// the response. The Python handler is invoked as a subprocess so the user's
// code can't crash the dev server.
func (e *pythonExecutor) Execute(r *http.Request, body []byte) (*executorResult, error) {
	if !strings.HasSuffix(e.funcFile, ".py") {
		return nil, fmt.Errorf("dev runtime only supports Python handlers in this build (got %s)\n   → For other runtimes use `ff flypy local` after `ff flypy build`", filepath.Base(e.funcFile))
	}

	python, err := findPython()
	if err != nil {
		return nil, err
	}

	// Build the request envelope.
	q := r.URL.Query()
	query := make(map[string]string, len(q))
	for k, vs := range q {
		if len(vs) > 0 {
			query[k] = vs[0]
		}
	}
	env := requestEnvelope{
		Method:  r.Method,
		URL:     r.URL.String(),
		Headers: r.Header,
		Body:    string(body),
		Query:   query,
	}
	envJSON, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("could not marshal request envelope: %w", err)
	}

	// The driver script: imports the user's module, calls fetch(request, env, ctx),
	// serialises the response to stdout. The dev server only relies on the
	// JSON contract — the user's actual function code is untouched.
	driver := pythonDevDriver
	if e.manifest != nil && e.manifest.Runtime != "" {
		// Allow explicit override (e.g. python3.11 in the manifest); we still
		// resolve a real interpreter below.
		_ = e.manifest.Runtime
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, python, "-I", "-S", "-c", driver,
		e.funcFile, // 1st script arg: function file (relative to cwd)
	)
	cmd.Dir, _ = os.Getwd()
	cmd.Stderr = &bytes.Buffer{} // captured for diagnostics
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Env = append(os.Environ(),
		"PYTHONUNBUFFERED=1",
		"PYTHONDONTWRITEBYTECODE=1",
		"FF_DEV_REQUEST="+string(envJSON),
	)

	if err := cmd.Run(); err != nil {
		stderr := cmd.Stderr.(*bytes.Buffer).String()
		// Exit code 2 is a clean Python error (raised exception). Map to 500.
		if exitErr := (&exec.ExitError{}); errors.As(err, &exitErr) {
			return nil, fmt.Errorf("handler failed: %w\n%s", err, stderr)
		}
		return nil, fmt.Errorf("could not run Python: %w\n%s", err, stderr)
	}

	var resp responseEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("handler returned invalid JSON: %w\nstdout: %s", err, truncate(stdout.String(), 500))
	}

	// Build HTTP response.
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	headers := http.Header{}
	for k, v := range resp.Headers {
		headers.Set(k, v)
	}

	var bodyOut []byte
	switch v := resp.Body.(type) {
	case nil:
		bodyOut = nil
	case string:
		bodyOut = []byte(v)
	case []byte:
		bodyOut = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("handler response body is not JSON-serialisable: %w", err)
		}
		bodyOut = b
	}

	return &executorResult{Status: status, Body: bodyOut, Headers: headers}, nil
}

// findPython returns the best available Python 3 interpreter.
func findPython() (string, error) {
	for _, name := range []string{"python3.11", "python3.12", "python3.13", "python3.10", "python3"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Python 3 interpreter found on PATH\n   → Install Python 3.10+ and try again")
}

// pythonDevDriver is the bootstrap script executed by pythonExecutor. It
// reads the request envelope from the FF_DEV_REQUEST env var, imports the
// user's function file, calls its `fetch` coroutine, and writes a JSON
// response to stdout.
//
// Why a separate script: it isolates the user's code from the dev server
// process, so a segfault or infinite loop in the handler can't take down
// `ff dev`. Any exit status other than 0 is treated as a 500.
const pythonDevDriver = `
import asyncio
import importlib.util
import json
import os
import sys
import traceback

req = json.loads(os.environ["FF_DEV_REQUEST"])
file_name = sys.argv[1] if len(sys.argv) > 1 else "main.py"
spec = importlib.util.spec_from_file_location("user_handler", file_name)
if spec is None or spec.loader is None:
    sys.stderr.write("could not load %s\n" % file_name)
    sys.exit(1)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

if not hasattr(mod, "fetch"):
    sys.stderr.write("handler %s must define async def fetch(request, env, ctx)\n" % file_name)
    sys.exit(1)

class _Request:
    def __init__(self, d):
        self.method = d.get("method", "GET")
        self.url = d.get("url", "/")
        self.headers = d.get("headers") or {}
        self.body = d.get("body") or ""
        self.query = d.get("query") or {}
    async def json(self):
        return json.loads(self.body) if self.body else {}
    async def text(self):
        return self.body or ""

class _Env:
    def __init__(self):
        for k, v in os.environ.items():
            if k.startswith("FF_USER_"):
                setattr(self, k[len("FF_USER_"):], v)
    def get(self, k, default=None):
        return getattr(self, k, default)

class _Ctx:
    def __init__(self):
        self._wait = []
    def waitUntil(self, p):
        self._wait.append(p)

request = _Request(req)
env = _Env()
ctx = _Ctx()

try:
    result = mod.fetch(request, env, ctx)
    if asyncio.iscoroutine(result):
        result = asyncio.run(result)

    if isinstance(result, dict) and "status" in result:
        out = {"status": int(result.get("status", 200))}
        body = result.get("body")
        if body is None:
            out["body"] = None
        elif isinstance(body, (str, bytes)):
            out["body"] = body.decode() if isinstance(body, bytes) else body
        else:
            out["body"] = body
        out["headers"] = result.get("headers") or {}
    else:
        out = {"status": 200, "body": result}
except Exception as e:
    sys.stderr.write("handler raised: %s\n%s\n" % (e, traceback.format_exc()))
    out = {"status": 500, "body": {"error": str(e), "type": type(e).__name__}, "headers": {"Content-Type": "application/json"}}

sys.stdout.write(json.dumps(out))
sys.stdout.flush()
`

// truncate is a tiny helper used in error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ensure logrus is referenced (the dev server logs through it for failures)
var _ = logrus.WithError
