package testing

import (
	"context"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/functionfly/ff-cli/internal/manifest"
)

// TestConfig represents test configuration
type TestConfig struct {
	Input       string
	Concurrency int
	Iterations  int
	Timeout     time.Duration
}

// TestResult represents the result of a test execution
type TestResult struct {
	Input     string
	Status    int
	Output    string
	LatencyMs int
	Error     string
}

// BenchmarkConfig represents benchmark configuration
type BenchmarkConfig struct {
	Input         string
	Concurrency   int
	Iterations    int
	Duration      time.Duration
	MemoryProfile bool
	LoadPattern   string
}

// BenchmarkResult represents benchmark results
type BenchmarkResult struct {
	TotalRequests      int
	SuccessfulRequests int
	FailedRequests     int
	Duration           time.Duration
	AvgLatencyMs       float64
	MinLatencyMs       float64
	MaxLatencyMs       float64
	P50LatencyMs       float64
	P95LatencyMs       float64
	P99LatencyMs       float64
	RequestsPerSec     float64
	DataTransferredMB  float64
	MemoryProfile      *MemoryProfile
	Errors             map[string]int
}

// MemoryProfile represents memory usage statistics
type MemoryProfile struct {
	PeakMemoryMB float64
	AvgMemoryMB  float64
	HasLeaks     bool
}

// ValidationResult represents the result of a validation check
type ValidationResult struct {
	Check   string
	Passed  bool
	Message string
}

// LocalTestRunner handles local function testing
type LocalTestRunner struct {
	bundle   []byte
	manifest *manifest.Manifest
	runtime  Runtime
}

// NewLocalTestRunner creates a new local test runner
func NewLocalTestRunner(bundle []byte, m *manifest.Manifest) *LocalTestRunner {
	return &LocalTestRunner{
		bundle:   bundle,
		manifest: m,
		runtime:  GetRuntime(m.Runtime),
	}
}

// Validate runs input/output validation
func (r *LocalTestRunner) Validate(input string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Initialize runtime
	if err := r.runtime.Initialize(ctx, r.bundle, r.manifest); err != nil {
		return fmt.Errorf("runtime initialization failed: %w", err)
	}
	defer func() { _ = r.runtime.Cleanup() }()

	// Test with provided input
	result, err := r.runtime.Execute(ctx, input)
	if err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	// Basic validation checks
	if result.Status < 200 || result.Status >= 600 {
		return fmt.Errorf("invalid HTTP status code: %d", result.Status)
	}

	if result.Output == "" {
		return fmt.Errorf("empty output returned")
	}

	return nil
}

// Test runs functional tests
func (r *LocalTestRunner) Test(config TestConfig) (*TestResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	defer cancel()

	start := time.Now()

	// Initialize runtime
	if err := r.runtime.Initialize(ctx, r.bundle, r.manifest); err != nil {
		return nil, fmt.Errorf("runtime initialization failed: %w", err)
	}
	defer func() { _ = r.runtime.Cleanup() }()

	// Execute test
	result, err := r.runtime.Execute(ctx, config.Input)
	if err != nil {
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	latency := time.Since(start)

	return &TestResult{
		Input:     config.Input,
		Status:    result.Status,
		Output:    result.Output,
		LatencyMs: int(latency.Milliseconds()),
		Error:     result.Error,
	}, nil
}

// Benchmark runs performance benchmarking
func (r *LocalTestRunner) Benchmark(config BenchmarkConfig) (*BenchmarkResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), config.Duration)
	defer cancel()

	start := time.Now()

	// Initialize runtime once for all benchmark runs
	if err := r.runtime.Initialize(ctx, r.bundle, r.manifest); err != nil {
		return nil, fmt.Errorf("runtime initialization failed: %w", err)
	}
	defer func() { _ = r.runtime.Cleanup() }()

	// Run benchmark
	results := make(chan *TestResult, config.Concurrency*10)
	errors := make(chan error, config.Concurrency)

	var wg sync.WaitGroup
	var mu sync.Mutex
	latencies := []float64{}
	totalRequests := 0
	successfulRequests := 0
	failedRequests := 0
	errorCounts := make(map[string]int)

	// Start workers
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.benchmarkWorker(ctx, config, results, errors)
		}()
	}

	// Collect results
	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	// Process results
	for result := range results {
		mu.Lock()
		totalRequests++
		latencies = append(latencies, float64(result.LatencyMs))

		if result.Status >= 200 && result.Status < 300 {
			successfulRequests++
		} else {
			failedRequests++
			if result.Error != "" {
				errorCounts[result.Error]++
			}
		}
		mu.Unlock()
	}

	// Check for errors
	select {
	case err := <-errors:
		return nil, err
	default:
	}

	duration := time.Since(start)

	// Calculate statistics
	sort.Float64s(latencies)
	var avgLatency, minLatency, maxLatency, p50, p95, p99 float64

	if len(latencies) > 0 {
		minLatency = latencies[0]
		maxLatency = latencies[len(latencies)-1]

		sum := 0.0
		for _, lat := range latencies {
			sum += lat
		}
		avgLatency = sum / float64(len(latencies))

		p50 = percentile(latencies, 50)
		p95 = percentile(latencies, 95)
		p99 = percentile(latencies, 99)
	}

	requestsPerSec := float64(totalRequests) / duration.Seconds()
	dataTransferredMB := float64(len(config.Input)*totalRequests) / (1024 * 1024)

	// Memory profiling
	var memProfile *MemoryProfile
	if config.MemoryProfile {
		memProfile = r.collectMemoryProfile()
	}

	return &BenchmarkResult{
		TotalRequests:      totalRequests,
		SuccessfulRequests: successfulRequests,
		FailedRequests:     failedRequests,
		Duration:           duration,
		AvgLatencyMs:       avgLatency,
		MinLatencyMs:       minLatency,
		MaxLatencyMs:       maxLatency,
		P50LatencyMs:       p50,
		P95LatencyMs:       p95,
		P99LatencyMs:       p99,
		RequestsPerSec:     requestsPerSec,
		DataTransferredMB:  dataTransferredMB,
		MemoryProfile:      memProfile,
		Errors:             errorCounts,
	}, nil
}

// benchmarkWorker runs benchmark iterations for a single worker
func (r *LocalTestRunner) benchmarkWorker(ctx context.Context, config BenchmarkConfig, results chan<- *TestResult, errors chan<- error) {
	for i := 0; config.Iterations == 0 || i < config.Iterations; i++ {
		select {
		case <-ctx.Done():
			return
		default:
			result, err := r.runtime.Execute(ctx, config.Input)
			if err != nil {
				errors <- err
				return
			}

			results <- &TestResult{
				Input:     config.Input,
				Status:    result.Status,
				Output:    result.Output,
				LatencyMs: result.LatencyMs,
				Error:     result.Error,
			}
		}
	}
}

// collectMemoryProfile collects memory usage statistics
func (r *LocalTestRunner) collectMemoryProfile() *MemoryProfile {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &MemoryProfile{
		PeakMemoryMB: float64(m.Sys) / (1024 * 1024),
		AvgMemoryMB:  float64(m.Alloc) / (1024 * 1024),
		HasLeaks:     m.NumGC > 10, // Simple heuristic
	}
}

// percentile calculates the given percentile from a sorted slice
func percentile(sortedData []float64, p float64) float64 {
	if len(sortedData) == 0 {
		return 0
	}

	index := (p / 100) * float64(len(sortedData)-1)
	lower := int(index)
	upper := lower + 1

	if upper >= len(sortedData) {
		return sortedData[lower]
	}

	weight := index - float64(lower)
	return sortedData[lower]*(1-weight) + sortedData[upper]*weight
}
