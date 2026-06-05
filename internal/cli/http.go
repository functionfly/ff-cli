package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const clientDefaultTimeout = 30 * time.Second

var defaultHTTPClient = &http.Client{
	Timeout: clientDefaultTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

// Client represents an HTTP client for the FunctionFly API.
type Client struct {
	baseURL        string
	token          string
	client         *http.Client
	allowInsecure  bool
	debugRequestFn func(string)
}

type clientOption func(*Client)

// NewClient creates a new API client.
func NewClient(baseURL, token string, opts ...clientOption) *Client {
	c := &Client{
		baseURL: baseURL,
		token:   token,
		client:  defaultHTTPClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// WithInsecureTLS returns an option that relaxes TLS verification for a
// specific client. It exists only for local dev/test against a localhost
// API with a self-signed certificate. Production callers should not set
// this.
func WithInsecureTLS() clientOption {
	return func(c *Client) {
		c.allowInsecure = true
		c.client = &http.Client{Timeout: clientDefaultTimeout}
		c.client.Transport = insecureTransport()
	}
}

// WithDebugRequestHook attaches a hook that receives a redacted request
// preview before the request is sent. The hook is invoked synchronously
// and may be called from any goroutine.
func WithDebugRequestHook(fn func(preview string)) clientOption {
	return func(c *Client) {
		c.debugRequestFn = fn
	}
}

func (c *Client) logDebugRequest(req *http.Request) {
	if c.debugRequestFn == nil {
		return
	}
	body := redactableBody(req)
	c.debugRequestFn(fmt.Sprintf("%s %s %s | Authorization: %s | Body: %s",
		req.Method, req.URL, req.Proto, redactBearer(c.token), truncatePreview(body)))
}

// doRequest performs an HTTP request with authentication.
func (c *Client) doRequest(method, path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	fullURL, err := joinURIRoot(c.baseURL, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, fullURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	c.logDebugRequest(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// joinURIRoot ensures path values are not treated as opaque strings when
// the base URL contains a path component. It also rejects control
// characters that could be used for request smuggling.
func joinURIRoot(base, path string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("base URL is empty")
	}
	if path == "" {
		return base, nil
	}
	if strings.ContainsAny(path, "\r\n") {
		return "", fmt.Errorf("invalid request path: control characters are not allowed")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path, nil
}

func redactBearer(token string) string {
	if token == "" {
		return "(none)"
	}
	if len(token) <= 8 {
		return "REDACTED"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

func redactableBody(req *http.Request) string {
	if req.Body == nil {
		return ""
	}
	data, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(data))
	return string(data)
}

func truncatePreview(s string) string {
	const limit = 200
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func insecureTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
	}
}

func init() {
	_ = strings.ContainsAny
}

// Login performs authentication and returns a JWT token
func (c *Client) Login(email, password string) (string, error) {
	resp, err := c.doRequest("POST", "/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("login failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode login response: %w", err)
	}

	return result.Token, nil
}

// CreateApp creates a new app
func (c *Client) CreateApp(name, slug string) (*AppResponse, error) {
	resp, err := c.doRequest("POST", "/v1/apps", map[string]string{
		"name": name,
		"slug": slug,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create app failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		App *AppResponse `json:"app"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode create app response: %w", err)
	}

	return result.App, nil
}

// GetApp retrieves an app by ID
func (c *Client) GetApp(appID string) (*AppResponse, error) {
	resp, err := c.doRequest("GET", "/v1/apps/"+appID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get app failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		App *AppResponse `json:"app"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode get app response: %w", err)
	}

	return result.App, nil
}

// CreateBackend creates a new backend for an app
func (c *Client) CreateBackend(appID, provider, region, url, sharedSecret string) (*BackendResponse, error) {
	body := map[string]string{
		"provider": provider,
		"region":   region,
		"url":      url,
	}
	if sharedSecret != "" {
		body["shared_secret"] = sharedSecret
	}

	resp, err := c.doRequest("POST", "/v1/apps/"+appID+"/backends", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create backend failed (%d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Backend *BackendResponse `json:"backend"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode create backend response: %w", err)
	}

	return result.Backend, nil
}

// GetStatus retrieves app status including backends and health info
func (c *Client) GetStatus(appID string) (*StatusResponse, error) {
	resp, err := c.doRequest("GET", "/v1/apps/"+appID+"/status", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get status failed (%d): %s", resp.StatusCode, string(body))
	}

	var result StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode status response: %w", err)
	}

	return &result, nil
}

// Health checks the orchestrator health endpoint
func (c *Client) Health() error {
	resp, err := c.doRequest("GET", "/health", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed (%d)", resp.StatusCode)
	}

	return nil
}

// Deploy creates a new deployment
func (c *Client) Deploy(appID string, req *DeployRequest) (*DeployResponse, error) {
	resp, err := c.doRequest("POST", "/v1/apps/"+appID+"/deploy", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("deploy failed (%d): %s", resp.StatusCode, string(body))
	}

	var result DeployResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode deploy response: %w", err)
	}

	return &result, nil
}

// ListDeployments retrieves deployments for an app
func (c *Client) ListDeployments(appID string) (*ListDeploymentsResponse, error) {
	resp, err := c.doRequest("GET", "/v1/apps/"+appID+"/deployments", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list deployments failed (%d): %s", resp.StatusCode, string(body))
	}

	var result ListDeploymentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode list deployments response: %w", err)
	}

	return &result, nil
}

// Rollback rolls back to a specific deployment
func (c *Client) Rollback(deploymentID string) (*RollbackResponse, error) {
	resp, err := c.doRequest("POST", "/deployments/"+deploymentID+"/rollback", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rollback failed (%d): %s", resp.StatusCode, string(body))
	}

	var result RollbackResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode rollback response: %w", err)
	}

	return &result, nil
}

// Registry methods for function publishing

// PublishFunction publishes a function to the registry
func (c *Client) PublishFunction(req *PublishRequest) (*PublishResponse, error) {
	resp, err := c.doRequest("POST", "/v1/registry/publish", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("publish failed (%d): %s", resp.StatusCode, string(body))
	}

	var result PublishResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode publish response: %w", err)
	}

	return &result, nil
}

// GetFunction gets function information
func (c *Client) GetFunction(author, name string) (*FunctionInfo, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/v1/registry/%s/%s", author, name), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get function failed (%d): %s", resp.StatusCode, string(body))
	}

	var result FunctionInfo
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode function info: %w", err)
	}

	return &result, nil
}

// GetFunctionStats gets function usage statistics
func (c *Client) GetFunctionStats(author, name, period string) (*FunctionStats, error) {
	url := fmt.Sprintf("/v1/registry/%s/%s/stats", author, name)
	if period != "" {
		url += "?period=" + period
	}

	resp, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get stats failed (%d): %s", resp.StatusCode, string(body))
	}

	var result FunctionStats
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode stats: %w", err)
	}

	return &result, nil
}

// TestFunction runs a remote test execution
func (c *Client) TestFunction(author, name string, input string) (*TestResult, error) {
	req := &TestRequest{Input: input}
	resp, err := c.doRequest("POST", fmt.Sprintf("/v1/registry/%s/%s/test", author, name), req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("test failed (%d): %s", resp.StatusCode, string(body))
	}

	var result TestResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode test result: %w", err)
	}

	return &result, nil
}

// GetFunctionLogs retrieves function execution logs
func (c *Client) GetFunctionLogs(author, name string, params map[string]string) ([]*FunctionLogEntry, error) {
	path := fmt.Sprintf("/v1/registry/%s/%s/logs", author, name)
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		path = path + "?" + q.Encode()
	}

	resp, err := c.doRequest("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get logs failed (%d): %s", resp.StatusCode, string(body))
	}

	var result []*FunctionLogEntry
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode logs: %w", err)
	}

	return result, nil
}

// GetDetailedMetrics retrieves detailed function metrics
func (c *Client) GetDetailedMetrics(author, name, period string) (*DetailedMetrics, error) {
	url := fmt.Sprintf("/v1/registry/%s/%s/metrics?period=%s", author, name, period)

	resp, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get metrics failed (%d): %s", resp.StatusCode, string(body))
	}

	var result DetailedMetrics
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode metrics: %w", err)
	}

	return &result, nil
}

// GetHealthStatus retrieves function health status
func (c *Client) GetHealthStatus(author, name string) (*HealthStatus, error) {
	url := fmt.Sprintf("/v1/registry/%s/%s/health", author, name)

	resp, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get health failed (%d): %s", resp.StatusCode, string(body))
	}

	var result HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode health status: %w", err)
	}

	return &result, nil
}

// GetDeploymentLogs retrieves deployment logs
func (c *Client) GetDeploymentLogs(deploymentID string, params map[string]string) ([]*LogEntry, error) {
	url := fmt.Sprintf("/v1/deployments/%s/logs", deploymentID)
	if len(params) > 0 {
		url += "?"
		for k, v := range params {
			url += k + "=" + v + "&"
		}
		url = url[:len(url)-1] // Remove trailing &
	}

	resp, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get deployment logs failed (%d): %s", resp.StatusCode, string(body))
	}

	var result []*LogEntry
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode deployment logs: %w", err)
	}

	return result, nil
}

// GetDeploymentStatus retrieves deployment status
func (c *Client) GetDeploymentStatus(deploymentID string) (*DeploymentStatus, error) {
	url := fmt.Sprintf("/v1/deployments/%s/status", deploymentID)

	resp, err := c.doRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get deployment status failed (%d): %s", resp.StatusCode, string(body))
	}

	var result DeploymentStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode deployment status: %w", err)
	}

	return &result, nil
}

// Response types
type AppResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

type BackendResponse struct {
	ID           string    `json:"id"`
	Provider     string    `json:"provider"`
	Region       string    `json:"region"`
	URL          string    `json:"url"`
	SharedSecret string    `json:"shared_secret"`
	CreatedAt    time.Time `json:"created_at"`
}

type StatusResponse struct {
	App      *AppResponse             `json:"app"`
	Backends []*BackendStatusResponse `json:"backends"`
}

type BackendStatusResponse struct {
	Backend           *BackendResponse      `json:"backend"`
	CircuitState      *CircuitStateResponse `json:"circuit_state"`
	LatestHealthCheck *HealthCheckResponse  `json:"latest_health_check"`
}

type CircuitStateResponse struct {
	State         string     `json:"state"`
	SinceTs       time.Time  `json:"since_ts"`
	FailCount     int        `json:"fail_count"`
	SuccessCount  int        `json:"success_count"`
	LastFailureTs *time.Time `json:"last_failure_ts"`
}

type HealthCheckResponse struct {
	Timestamp    time.Time `json:"timestamp"`
	OK           bool      `json:"ok"`
	StatusCode   int       `json:"status_code"`
	LatencyMs    int       `json:"latency_ms"`
	ErrorMessage string    `json:"error_message"`
}

// Registry types
type PublishRequest struct {
	Author   string          `json:"author"`
	Name     string          `json:"name"`
	Version  string          `json:"version"`
	Manifest json.RawMessage `json:"manifest"`
	Access   string          `json:"access,omitempty"` // "public" | "private"
	Force    bool            `json:"force,omitempty"`  // overwrite existing version
}

type PublishResponse struct {
	OK       bool   `json:"ok"`
	Function string `json:"function"`
	Version  string `json:"version"`
	Message  string `json:"message,omitempty"`
}

type FunctionInfo struct {
	Author      string    `json:"author"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Runtime     string    `json:"runtime"`
	Public      bool      `json:"public"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type FunctionStats struct {
	FunctionID   string  `json:"function_id"`
	TotalCalls   int64   `json:"total_calls"`
	SuccessRate  float64 `json:"success_rate"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	Revenue      float64 `json:"revenue"`
	Period       string  `json:"period"`
}

type TestRequest struct {
	Input string `json:"input"`
}

type TestResult struct {
	Status    int    `json:"status"`
	Body      string `json:"body"`
	LatencyMs int    `json:"latency_ms"`
	Cached    bool   `json:"cached"`
	Region    string `json:"region"`
}

// Monitoring and Logging Types

// FunctionLogEntry represents a function execution log entry
type FunctionLogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Level      string    `json:"level"`
	Message    string    `json:"message"`
	RequestID  string    `json:"request_id"`
	StatusCode int       `json:"status_code,omitempty"`
	LatencyMs  int       `json:"latency_ms,omitempty"`
	Region     string    `json:"region,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
	IP         string    `json:"ip,omitempty"`
}

// LogEntry represents a deployment or system log entry
type LogEntry struct {
	Timestamp    time.Time `json:"timestamp"`
	Level        string    `json:"level"`
	Message      string    `json:"message"`
	Source       string    `json:"source"`
	DeploymentID string    `json:"deployment_id,omitempty"`
}

// DetailedMetrics represents comprehensive performance metrics
type DetailedMetrics struct {
	FunctionID      string            `json:"function_id"`
	Name            string            `json:"name"`
	Author          string            `json:"author"`
	Period          string            `json:"period"`
	TotalRequests   int64             `json:"total_requests"`
	SuccessfulReqs  int64             `json:"successful_requests"`
	FailedReqs      int64             `json:"failed_requests"`
	ErrorRate       float64           `json:"error_rate"`
	AvgLatencyMs    float64           `json:"avg_latency_ms"`
	P50LatencyMs    float64           `json:"p50_latency_ms"`
	P95LatencyMs    float64           `json:"p95_latency_ms"`
	P99LatencyMs    float64           `json:"p99_latency_ms"`
	MinLatencyMs    float64           `json:"min_latency_ms"`
	MaxLatencyMs    float64           `json:"max_latency_ms"`
	RequestsPerSec  float64           `json:"requests_per_sec"`
	DataTransferred float64           `json:"data_transferred_mb"`
	TopErrors       []ErrorCount      `json:"top_errors"`
	StatusCodes     map[int]int64     `json:"status_codes"`
	RegionalStats   []RegionalStats   `json:"regional_stats"`
	TimeSeries      []TimeSeriesPoint `json:"time_series"`
}

// ErrorCount represents error frequency
type ErrorCount struct {
	Error   string  `json:"error"`
	Count   int64   `json:"count"`
	Percent float64 `json:"percent"`
}

// RegionalStats represents per-region statistics
type RegionalStats struct {
	Region       string  `json:"region"`
	Requests     int64   `json:"requests"`
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	ErrorRate    float64 `json:"error_rate"`
}

// TimeSeriesPoint represents a data point in time series
type TimeSeriesPoint struct {
	Timestamp  time.Time `json:"timestamp"`
	Requests   int64     `json:"requests"`
	AvgLatency float64   `json:"avg_latency_ms"`
	ErrorRate  float64   `json:"error_rate"`
}

// HealthStatus represents comprehensive health status
type HealthStatus struct {
	Overall        string          `json:"overall"`
	FunctionHealth *FunctionHealth `json:"function_health,omitempty"`
	SystemHealth   *SystemHealth   `json:"system_health,omitempty"`
	PlatformHealth *PlatformHealth `json:"platform_health,omitempty"`
	Timestamp      time.Time       `json:"timestamp"`
}

// FunctionHealth represents function-specific health
type FunctionHealth struct {
	FunctionName   string           `json:"function_name"`
	Status         string           `json:"status"`
	Availability   float64          `json:"availability"`
	AvgLatencyMs   float64          `json:"avg_latency_ms"`
	ErrorRate      float64          `json:"error_rate"`
	LastChecked    time.Time        `json:"last_checked"`
	Issues         []HealthIssue    `json:"issues,omitempty"`
	RegionalHealth []RegionalHealth `json:"regional_health,omitempty"`
}

// SystemHealth represents system-wide health
type SystemHealth struct {
	Status       string          `json:"status"`
	ResponseTime time.Duration   `json:"response_time"`
	Services     []ServiceStatus `json:"services"`
	Issues       []HealthIssue   `json:"issues,omitempty"`
}

// PlatformHealth represents platform-wide health
type PlatformHealth struct {
	Status        string                 `json:"status"`
	Regions       []RegionStatus         `json:"regions"`
	GlobalMetrics map[string]interface{} `json:"global_metrics,omitempty"`
	Issues        []HealthIssue          `json:"issues,omitempty"`
}

// HealthIssue represents a health issue
type HealthIssue struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Service  string `json:"service,omitempty"`
	Region   string `json:"region,omitempty"`
}

// RegionalHealth represents health in a specific region
type RegionalHealth struct {
	Region    string  `json:"region"`
	Status    string  `json:"status"`
	LatencyMs float64 `json:"latency_ms"`
	ErrorRate float64 `json:"error_rate"`
}

// ServiceStatus represents the status of a service
type ServiceStatus struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// RegionStatus represents the status of a region
type RegionStatus struct {
	Name   string        `json:"name"`
	Status string        `json:"status"`
	Issues []HealthIssue `json:"issues,omitempty"`
}

// DeploymentStatus represents detailed deployment status
type DeploymentStatus struct {
	DeploymentID string    `json:"deployment_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Message      string    `json:"message"`
	HealthStatus string    `json:"health_status"`
	Region       string    `json:"region"`
	URL          string    `json:"url"`
}
