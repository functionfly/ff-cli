package commands

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBuildError_StatusCodes(t *testing.T) {
	client := &APIClient{Token: "test"}
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantHint   string
		wantMsg    string
	}{
		{
			name:       "401 unauthorized",
			statusCode: 401,
			body:       `{"error":"unauthorized"}`,
			wantHint:   "ff login",
			wantMsg:    "unauthorized",
		},
		{
			name:       "403 forbidden",
			statusCode: 403,
			body:       `{"error":"forbidden"}`,
			wantHint:   "permission",
			wantMsg:    "forbidden",
		},
		{
			name:       "404 not found",
			statusCode: 404,
			body:       `{"error":"not found"}`,
			wantHint:   "not found",
			wantMsg:    "not found",
		},
		{
			name:       "409 conflict",
			statusCode: 409,
			body:       `{"error":"version exists"}`,
			wantHint:   "update patch",
			wantMsg:    "version exists",
		},
		{
			name:       "429 rate limited",
			statusCode: 429,
			body:       `{"error":"too many requests"}`,
			wantHint:   "Rate limited",
			wantMsg:    "too many requests",
		},
		{
			name:       "502 bad gateway",
			statusCode: 502,
			body:       `{"error":"bad gateway"}`,
			wantHint:   "Server temporarily unavailable",
			wantMsg:    "bad gateway",
		},
		{
			name:       "503 service unavailable",
			statusCode: 503,
			body:       `{"error":"service down"}`,
			wantHint:   "Server temporarily unavailable",
			wantMsg:    "service down",
		},
		{
			name:       "504 gateway timeout",
			statusCode: 504,
			body:       `{"error":"timeout"}`,
			wantHint:   "Server temporarily unavailable",
			wantMsg:    "timeout",
		},
		{
			name:       "message field fallback",
			statusCode: 500,
			body:       `{"message":"internal error"}`,
			wantMsg:    "internal error",
		},
		{
			name:       "plain text body",
			statusCode: 500,
			body:       "Internal Server Error",
			wantMsg:    "HTTP 500",
		},
		{
			name:       "empty body",
			statusCode: 500,
			body:       "",
			wantMsg:    "HTTP 500",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.buildError(tt.statusCode, []byte(tt.body))
			if err == nil {
				t.Fatal("buildError returned nil")
			}
			errStr := err.Error()
			if !strings.Contains(errStr, tt.wantMsg) {
				t.Errorf("error %q should contain %q", errStr, tt.wantMsg)
			}
			if tt.wantHint != "" && !strings.Contains(errStr, tt.wantHint) {
				t.Errorf("error %q should contain hint %q", errStr, tt.wantHint)
			}
		})
	}
}

func TestBuildError_PrefersErrorOverMessage(t *testing.T) {
	client := &APIClient{Token: "test"}
	err := client.buildError(400, []byte(`{"error":"primary","message":"fallback"}`))
	if !strings.Contains(err.Error(), "primary") {
		t.Errorf("should prefer 'error' field, got: %s", err.Error())
	}
	if strings.Contains(err.Error(), "fallback") {
		t.Errorf("should not use 'message' when 'error' is present, got: %s", err.Error())
	}
}

func TestCloneRequest_PreservesHeaders(t *testing.T) {
	body := `{"key":"value"}`
	req, _ := http.NewRequest("POST", "http://example.com", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token123")

	cloned := cloneRequest(req)

	if cloned.Method != req.Method {
		t.Errorf("Method = %q, want %q", cloned.Method, req.Method)
	}
	if cloned.URL.String() != req.URL.String() {
		t.Errorf("URL = %q, want %q", cloned.URL.String(), req.URL.String())
	}
	if cloned.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", cloned.Header.Get("Content-Type"))
	}
	if cloned.Header.Get("Authorization") != "Bearer token123" {
		t.Errorf("Authorization = %q, want Bearer token123", cloned.Header.Get("Authorization"))
	}
}

func TestCloneRequest_BodyIsReproducible(t *testing.T) {
	body := `{"test":"data"}`
	req, _ := http.NewRequest("POST", "http://example.com", strings.NewReader(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}

	cloned := cloneRequest(req)
	data, err := io.ReadAll(cloned.Body)
	if err != nil {
		t.Fatalf("failed to read cloned body: %v", err)
	}
	if string(data) != body {
		t.Errorf("body = %q, want %q", string(data), body)
	}

	cloned2 := cloneRequest(req)
	data2, _ := io.ReadAll(cloned2.Body)
	if string(data2) != body {
		t.Errorf("second clone body = %q, want %q", string(data2), body)
	}
}

func newTestClient(ts *httptest.Server) *APIClient {
	return &APIClient{
		BaseURL: ts.URL,
		Token:   "test",
		client:  &http.Client{Timeout: 5 * time.Second},
	}
}

func TestAPIClient_DO_RetryOn5xx(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"service unavailable"}`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)

	var result struct {
		OK bool `json:"ok"`
	}
	err := client.Get("/test", &result)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if !result.OK {
		t.Error("expected ok=true")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestAPIClient_DO_RetryOn429(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"data":"ok"}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)

	var result struct {
		Data string `json:"data"`
	}
	err := client.Get("/test", &result)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if result.Data != "ok" {
		t.Errorf("data = %q, want %q", result.Data, "ok")
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestAPIClient_DO_NoRetryOn4xx(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)

	err := client.Get("/test", nil)
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("error should mention 'bad request', got: %s", err.Error())
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt (no retry for 400), got %d", attempts)
	}
}

func TestAPIClient_DO_MaxRetriesExhausted(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"always fail"}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)

	err := client.Get("/test", nil)
	if err == nil {
		t.Fatal("expected error after max retries")
	}
	if !strings.Contains(err.Error(), "always fail") {
		t.Errorf("error should contain last error message, got: %s", err.Error())
	}
	if attempts != maxRetries+1 {
		t.Errorf("expected %d attempts (maxRetries+1), got %d", maxRetries+1, attempts)
	}
}

func TestAPIClient_DO_SuccessNoRetry(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)

	var result struct {
		Status string `json:"status"`
	}
	err := client.Get("/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status = %q, want %q", result.Status, "ok")
	}
	if attempts != 1 {
		t.Errorf("expected 1 attempt for success, got %d", attempts)
	}
}

func TestAPIClient_DO_SetsUserAgent(t *testing.T) {
	var gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)
	client.Get("/test", nil)

	if !strings.HasPrefix(gotUA, "ff-cli/") {
		t.Errorf("User-Agent = %q, want prefix ff-cli/", gotUA)
	}
}

func TestAPIClient_DO_SetsAuthorization(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)
	client.Get("/test", nil)

	if gotAuth != "Bearer test" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test")
	}
}

func TestAPIClient_DO_NetworkErrorRetries(t *testing.T) {
	client := &APIClient{
		BaseURL: "http://127.0.0.1:1",
		Token:   "test",
		client:  &http.Client{Timeout: 1 * time.Second},
	}

	start := time.Now()
	err := client.Get("/test", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected network error")
	}
	if !strings.Contains(err.Error(), "network error") {
		t.Errorf("error should mention network error, got: %s", err.Error())
	}
	if elapsed < 500*time.Millisecond {
		t.Errorf("expected backoff delay, but only took %v", elapsed)
	}
}

func TestAPIClient_DO_PostSetsContentType(t *testing.T) {
	var gotCT string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)
	client.Post("/test", map[string]string{"key": "val"}, nil)

	if gotCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotCT)
	}
}

func TestAPIClient_DO_ResponseParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"name":"test","version":"1.0.0"}`))
	}))
	defer ts.Close()

	client := newTestClient(ts)

	var result struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	err := client.Get("/test", &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "test" {
		t.Errorf("name = %q, want %q", result.Name, "test")
	}
	if result.Version != "1.0.0" {
		t.Errorf("version = %q, want %q", result.Version, "1.0.0")
	}
}

func TestAPIClient_DO_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`not json`))
	}))
	defer ts.Close()

	client := newTestClient(ts)

	var result struct{}
	err := client.Get("/test", &result)
	if err == nil {
		t.Fatal("expected parse error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("error should mention parsing, got: %s", err.Error())
	}
}

func TestAPIClient_DO_EmptyBodySuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))
	defer ts.Close()

	client := newTestClient(ts)
	err := client.Delete("/test", nil)
	if err != nil {
		t.Fatalf("unexpected error for 204: %v", err)
	}
}
