package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPython(t *testing.T) {
	p, err := findPython()
	if err != nil {
		t.Skipf("no Python interpreter on PATH: %v", err)
	}
	if !strings.Contains(p, "python") {
		t.Errorf("findPython returned %q, expected a python binary", p)
	}
}

func TestPythonExecutor_NonPythonHandler(t *testing.T) {
	tmp := t.TempDir()
	mf := &Manifest{Name: "test", Version: "1.0.0", Runtime: "node20"}
	if err := os.WriteFile(filepath.Join(tmp, "index.js"), []byte("module.exports = {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	ex := newPythonExecutor(mf, filepath.Join(tmp, "index.js"))
	req := httptest.NewRequest("GET", "/", nil)
	_, err := ex.Execute(req, nil)
	if err == nil {
		t.Fatal("expected error for non-Python handler")
	}
	if !strings.Contains(err.Error(), "dev runtime only supports Python") {
		t.Errorf("error message: %v", err)
	}
}

func TestPythonExecutor_NoPythonOnPath(t *testing.T) {
	// Save and clear PATH to force "not found".
	origPath := os.Getenv("PATH")
	t.Setenv("PATH", "")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	mf := &Manifest{Name: "test", Version: "1.0.0", Runtime: "python3.11"}
	ex := newPythonExecutor(mf, "main.py")
	req := httptest.NewRequest("GET", "/", nil)
	_, err := ex.Execute(req, nil)
	if err == nil {
		t.Fatal("expected error when no Python is on PATH")
	}
	if !strings.Contains(err.Error(), "no Python 3 interpreter") {
		t.Errorf("error message: %v", err)
	}
}

func TestPythonExecutor_RunsHandler(t *testing.T) {
	// Skip if Python isn't available.
	if _, err := findPython(); err != nil {
		t.Skipf("no Python: %v", err)
	}

	tmp := t.TempDir()
	src := `async def fetch(request, env, ctx):
    name = request.query.get("name", "World")
    return {"status": 200, "body": {"hello": name}, "headers": {"X-From-Handler": "yes"}}
`
	if err := os.WriteFile(filepath.Join(tmp, "main.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir("/") })

	mf := &Manifest{Name: "test", Version: "1.0.0", Runtime: "python3.11", TimeoutMS: 5000}
	ex := newPythonExecutor(mf, "main.py")

	req := httptest.NewRequest("GET", "/?name=Alice", nil)
	r := req.WithContext(req.Context())
	// httptest.NewRequest already returns *http.Request.
	r.Method = "GET"
	r.URL.RawQuery = "name=Alice"

	result, err := ex.Execute(r, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Status != 200 {
		t.Errorf("status = %d, want 200", result.Status)
	}
	if got := result.Headers.Get("X-From-Handler"); got != "yes" {
		t.Errorf("X-From-Handler = %q, want yes", got)
	}
	if !strings.Contains(string(result.Body), "Alice") {
		t.Errorf("body should mention Alice, got %q", string(result.Body))
	}
}

func TestPythonExecutor_HandlerErrorReturns500(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skipf("no Python: %v", err)
	}

	tmp := t.TempDir()
	src := `async def fetch(request, env, ctx):
    raise ValueError("intentional failure")
`
	if err := os.WriteFile(filepath.Join(tmp, "main.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir("/") })

	mf := &Manifest{Name: "test", Version: "1.0.0", Runtime: "python3.11", TimeoutMS: 5000}
	ex := newPythonExecutor(mf, "main.py")
	req := httptest.NewRequest("GET", "/", nil)
	result, err := ex.Execute(req, nil)
	if err != nil {
		t.Fatalf("unexpected exec error (handler should be isolated): %v", err)
	}
	if result.Status != 500 {
		t.Errorf("status = %d, want 500 (handler raised)", result.Status)
	}
	if !strings.Contains(string(result.Body), "intentional failure") {
		t.Errorf("body should contain error message, got: %s", string(result.Body))
	}
}

func TestPythonExecutor_TimeoutEnforced(t *testing.T) {
	if _, err := findPython(); err != nil {
		t.Skipf("no Python: %v", err)
	}

	tmp := t.TempDir()
	src := `import time
async def fetch(request, env, ctx):
    time.sleep(5)
    return {"status": 200, "body": "too late"}
`
	if err := os.WriteFile(filepath.Join(tmp, "main.py"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir("/") })

	mf := &Manifest{Name: "test", Version: "1.0.0", Runtime: "python3.11", TimeoutMS: 200}
	ex := newPythonExecutor(mf, "main.py")
	req := httptest.NewRequest("GET", "/", nil)
	_, err := ex.Execute(req, nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	// The error message will mention "context deadline exceeded" or "signal: killed".
	if !strings.Contains(strings.ToLower(err.Error()), "killed") &&
		!strings.Contains(strings.ToLower(err.Error()), "deadline") &&
		!strings.Contains(strings.ToLower(err.Error()), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

// ensure http is referenced
var _ = http.MethodGet
