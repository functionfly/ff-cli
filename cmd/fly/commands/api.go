package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/functionfly/ff-cli/internal/version"
)

// APIClient is a simple HTTP client for the FunctionFly API.
type APIClient struct {
	BaseURL string
	Token   string
	client  *http.Client
}

// NewAPIClient creates a new API client using FF_TOKEN env or stored credentials.
func NewAPIClient() (*APIClient, error) {
	token := os.Getenv("FF_TOKEN")
	if token == "" {
		creds, err := LoadCredentials()
		if err != nil {
			return nil, err
		}
		token = creds.Token
	}
	baseURL := "https://api.functionfly.com"
	if u := os.Getenv("FF_API_URL"); u != "" {
		baseURL = u
	} else if cfg, _ := LoadConfig(); cfg != nil && cfg.API.URL != "" {
		baseURL = cfg.API.URL
	}
	c := &APIClient{
		BaseURL: baseURL,
		Token:   token,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:    90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
	if err := c.sanitizeBaseURL(); err != nil {
		return nil, err
	}
	return c, nil
}

// NewAPIClientWithToken creates a new API client with an explicit token.
func NewAPIClientWithToken(token string) *APIClient {
	baseURL := "https://api.functionfly.com"
	if u := os.Getenv("FF_API_URL"); u != "" {
		baseURL = u
	} else if cfg, _ := LoadConfig(); cfg != nil && cfg.API.URL != "" {
		baseURL = cfg.API.URL
	}
	c := &APIClient{
		BaseURL: baseURL,
		Token:   token,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:    90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}
	_ = c.sanitizeBaseURL()
	return c
}

func (c *APIClient) sanitizeBaseURL() error {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("invalid API base URL %q: %w", c.BaseURL, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("API base URL must use http or https: %s", c.BaseURL)
	}
	if u.Host == "" {
		return fmt.Errorf("API base URL missing host: %s", c.BaseURL)
	}
	c.BaseURL = strings.TrimRight(u.String(), "/")
	return nil
}

func (c *APIClient) joinPath(path string) (string, error) {
	if c.BaseURL == "" {
		return "", fmt.Errorf("API base URL is empty")
	}
	if path == "" {
		return "", fmt.Errorf("request path is empty")
	}
	if strings.ContainsAny(path, "\r\n") {
		return "", fmt.Errorf("invalid request path: control characters are not allowed")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BaseURL + path, nil
}

func (c *APIClient) Get(path string, out interface{}) error {
	fullURL, err := c.joinPath(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

func (c *APIClient) Post(path string, body interface{}, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	fullURL, err := c.joinPath(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", fullURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *APIClient) Put(path string, body interface{}, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	fullURL, err := c.joinPath(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PUT", fullURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *APIClient) Patch(path string, body interface{}, out interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	fullURL, err := c.joinPath(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("PATCH", fullURL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

func (c *APIClient) Delete(path string, out interface{}) error {
	fullURL, err := c.joinPath(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("DELETE", fullURL, nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
}

const maxResponseSize = 10 << 20 // 10 MB
const maxRetries = 3

func (c *APIClient) do(req *http.Request, out interface{}) error {
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("User-Agent", "ff-cli/"+version.Short())

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt) * 500 * time.Millisecond
			time.Sleep(delay)
		}

		resp, err := c.client.Do(cloneRequest(req))
		if err != nil {
			lastErr = fmt.Errorf("network error: %w\n   → Check your internet connection", err)
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		_ = resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("could not read response: %w", err)
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = c.buildError(resp.StatusCode, body)
			continue
		}

		if resp.StatusCode >= 400 {
			return c.buildError(resp.StatusCode, body)
		}

		if out != nil && len(body) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return fmt.Errorf("could not parse response: %w", err)
			}
		}
		return nil
	}
	return lastErr
}

func (c *APIClient) buildError(statusCode int, body []byte) error {
	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &errResp)
	msg := errResp.Error
	if msg == "" {
		msg = errResp.Message
	}
	if msg == "" {
		msg = fmt.Sprintf("HTTP %d", statusCode)
		if len(body) > 0 && body[0] != '{' {
			msg = fmt.Sprintf("%s: %s", msg, strings.TrimSpace(string(body)))
		}
	}
	hint := ""
	switch statusCode {
	case 401:
		hint = "\n   → Your session may have expired — run: ff login"
	case 403:
		hint = "\n   → You don't have permission to perform this action"
	case 404:
		hint = "\n   → The resource was not found"
	case 409:
		hint = "\n   → This version already exists — run: ff update patch"
	case 429:
		hint = "\n   → Rate limited — retrying with backoff"
	case 502, 503, 504:
		hint = "\n   → Server temporarily unavailable — retrying"
	}
	return fmt.Errorf("%s%s", msg, hint)
}

func cloneRequest(req *http.Request) *http.Request {
	r := req.Clone(req.Context())
	if req.Body != nil && req.GetBody != nil {
		body, _ := req.GetBody()
		r.Body = body
	}
	return r
}

// App is a minimal app for backend commands.
type App struct {
	ID string `json:"id"`
}

// Backend is a created backend response.
type Backend struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
	URL      string `json:"url"`
}

// CircuitState is circuit breaker state.
type CircuitState struct {
	State string `json:"state"`
}

// HealthCheck is a health check result.
type HealthCheck struct {
	OK bool `json:"ok"`
}

// BackendStatus is one backend's status in app status.
type BackendStatus struct {
	Backend           *Backend      `json:"backend"`
	CircuitState      *CircuitState `json:"circuit_state"`
	LatestHealthCheck *HealthCheck  `json:"latest_health_check"`
}

// AppStatus is app status with backends.
type AppStatus struct {
	Backends []*BackendStatus `json:"backends"`
}

// GetApp looks up an app by name (slug or name) via GET /v1/apps and returns the matching app.
func (c *APIClient) GetApp(name string) (*App, error) {
	var out struct {
		Apps []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"apps"`
	}
	if err := c.Get("/v1/apps", &out); err != nil {
		return nil, err
	}
	for _, a := range out.Apps {
		if a.Slug == name || a.Name == name {
			return &App{ID: a.ID}, nil
		}
	}
	return nil, fmt.Errorf("app not found: %s", name)
}

// CreateBackend creates a backend for an app via POST /v1/apps/{appId}/backends.
func (c *APIClient) CreateBackend(appID, provider, region, url, sharedSecret string) (*Backend, error) {
	body := map[string]interface{}{
		"provider":      provider,
		"region":        region,
		"url":           url,
		"shared_secret": sharedSecret,
	}
	var out Backend
	path := "/v1/apps/" + appID + "/backends"
	if err := c.Post(path, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetStatus returns app status including backends via GET /v1/apps/{appId}/status.
func (c *APIClient) GetStatus(appID string) (*AppStatus, error) {
	var out AppStatus
	if err := c.Get("/v1/apps/"+appID+"/status", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteBackend removes a backend via DELETE /v1/apps/{appId}/backends/{backendId}.
func (c *APIClient) DeleteBackend(appID, backendID string) error {
	path := "/v1/apps/" + appID + "/backends/" + backendID
	return c.Delete(path, nil)
}

// StreamLines opens a streaming GET connection and calls fn for each line.
func (c *APIClient) StreamLines(path string, fn func(line string) bool) error {
	fullURL, err := c.joinPath(path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "ff-cli/"+version.Short())
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	buf := make([]byte, 4096)
	var line []byte
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			for _, b := range buf[:n] {
				if b == '\n' {
					if len(line) > 0 {
						if !fn(string(line)) {
							return nil
						}
						line = line[:0]
					}
				} else {
					line = append(line, b)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	return nil
}
