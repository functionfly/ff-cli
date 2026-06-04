package bundler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// NPMRegistryURL is the base URL for the npm registry
const NPMRegistryURL = "https://registry.npmjs.org"

// NPMTimeout is the timeout for npm registry requests
const NPMTimeout = 30 * time.Second

// NPMMetaCacheDuration is how long to cache package metadata (24 hours)
const NPMMetaCacheDuration = 24 * time.Hour

// NPMTarballCacheDuration is how long to cache tarballs (7 days)
const NPMTarballCacheDuration = 7 * 24 * time.Hour

// Max sizes to prevent OOM and abuse (production-safe)
const (
	NPMMaxMetadataSize = 10 << 20  // 10 MiB
	NPMMaxTarballSize  = 100 << 20 // 100 MiB
)

// Default User-Agent for registry requests (identify client, avoid 403)
const NPMUserAgent = "FunctionFly-Bundler/1.0 (+https://functionfly.com)"

// NPMLogger is an optional logger for cache and resolution warnings. When nil, no logging.
type NPMLogger interface {
	Warn(args ...interface{})
	Warnf(format string, args ...interface{})
}

// NPMClient is a client for interacting with the npm registry
type NPMClient struct {
	httpClient  *http.Client
	cacheDir    string
	authToken   string
	redisClient *redis.Client
	logger      NPMLogger
}

// NewNPMClient creates a new npm registry client with production defaults:
// timeout, User-Agent, and a transport suitable for registry requests.
func NewNPMClient(cacheDir string) *NPMClient {
	return &NPMClient{
		httpClient: &http.Client{
			Timeout: NPMTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		cacheDir: cacheDir,
	}
}

// SetAuthToken sets the npm authentication token
func (c *NPMClient) SetAuthToken(token string) {
	c.authToken = token
}

// SetRedisClient sets the Redis client for caching
func (c *NPMClient) SetRedisClient(client *redis.Client) {
	c.redisClient = client
}

// SetLogger sets an optional logger for cache and resolution warnings
func (c *NPMClient) SetLogger(l NPMLogger) {
	c.logger = l
}

func (c *NPMClient) warn(args ...interface{}) {
	if c.logger != nil {
		c.logger.Warn(args...)
	}
}

func (c *NPMClient) warnf(format string, args ...interface{}) {
	if c.logger != nil {
		c.logger.Warnf(format, args...)
	}
}

// NewNPMClientWithRedis creates a new npm registry client with Redis caching
func NewNPMClientWithRedis(cacheDir string, redisClient *redis.Client) *NPMClient {
	client := NewNPMClient(cacheDir)
	client.redisClient = redisClient
	return client
}

// getFromRedis retrieves data from Redis cache. Returns (nil, error) on miss or when Redis is not configured.
func (c *NPMClient) getFromRedis(ctx context.Context, key string) ([]byte, error) {
	if c.redisClient == nil {
		return nil, fmt.Errorf("redis not configured")
	}
	val, err := c.redisClient.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	return val, nil
}

// saveToRedis saves data to Redis cache with TTL
func (c *NPMClient) saveToRedis(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	if c.redisClient == nil {
		return fmt.Errorf("Redis client not configured")
	}

	return c.redisClient.Set(ctx, key, data, ttl).Err()
}

// InvalidateCache invalidates the cache for a specific package (e.g. for security updates).
// Uses SCAN instead of KEYS to avoid blocking Redis in production.
func (c *NPMClient) InvalidateCache(ctx context.Context, packageName string) error {
	if c.redisClient == nil {
		return nil
	}
	metaKey := fmt.Sprintf("npm:meta:%s", packageName)
	pattern := fmt.Sprintf("npm:package:%s:*", packageName)

	if err := c.redisClient.Del(ctx, metaKey).Err(); err != nil {
		return fmt.Errorf("invalidate metadata cache: %w", err)
	}

	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = c.redisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return fmt.Errorf("scan package cache keys: %w", err)
		}
		if len(keys) > 0 {
			if err := c.redisClient.Del(ctx, keys...).Err(); err != nil {
				return fmt.Errorf("invalidate package cache: %w", err)
			}
		}
		if cursor == 0 {
			break
		}
	}
	return nil
}

// NPMMetadata represents the metadata for an npm package
type NPMMetadata struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Description          string            `json:"description"`
	Main                 string            `json:"main"`
	Repository           *NPMRepository    `json:"repository"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	Dist                 *NPMDist          `json:"dist"`
}

// NPMRepository represents the repository information
type NPMRepository struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// NPMDist represents the distribution information
type NPMDist struct {
	Shasum  string `json:"shasum"`
	Tarball string `json:"tarball"`
}

// NPMRegistryResponse represents the response from the npm registry for a package
type NPMRegistryResponse struct {
	Name     string                  `json:"name"`
	Versions map[string]*NPMMetadata `json:"versions"`
	Time     map[string]interface{}  `json:"time"`      // version -> ISO date string; "modified"/"created" may be string or number
	DistTags map[string]string       `json:"dist-tags"` // "latest", etc.
}

// validatePackageName rejects empty, overly long, or unsafe package names (production safety).
func validatePackageName(name string) error {
	if name == "" {
		return fmt.Errorf("package name is empty")
	}
	if len(name) > 214 {
		return fmt.Errorf("package name too long")
	}
	if strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("invalid package name")
	}
	return nil
}

// doHTTPWithRetry performs a GET with exponential backoff (up to 4 attempts, 1s/2s/4s). Respects ctx.
func (c *NPMClient) doHTTPWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", NPMUserAgent)
	if c.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.authToken)
	}
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		clone := req.Clone(ctx)
		resp, err := c.httpClient.Do(clone)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode == 404 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("not found (404)")
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 500 && resp.StatusCode != 429 {
			return resp, nil
		}
		_ = resp.Body.Close()
		if resp.StatusCode == 429 {
			lastErr = fmt.Errorf("rate limited (429)")
			continue
		}
		lastErr = fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil, lastErr
}

// ResolveVersion resolves a version range to an exact version
func (c *NPMClient) ResolveVersion(ctx context.Context, packageName, versionRange string) (string, error) {
	if err := validatePackageName(packageName); err != nil {
		return "", err
	}
	// Handle exact versions
	if !strings.HasPrefix(versionRange, "^") &&
		!strings.HasPrefix(versionRange, "~") &&
		!strings.HasPrefix(versionRange, ">") &&
		!strings.HasPrefix(versionRange, "<") &&
		!strings.HasPrefix(versionRange, ">=") &&
		!strings.HasPrefix(versionRange, "<=") &&
		versionRange != "latest" &&
		versionRange != "*" {
		// Already an exact version
		return versionRange, nil
	}

	metadata, err := c.GetPackageMetadata(ctx, packageName)
	if err != nil {
		return "", err
	}

	// Handle special cases
	if versionRange == "latest" || versionRange == "*" {
		if latest, ok := metadata.DistTags["latest"]; ok {
			return latest, nil
		}
		// If no latest tag, return the newest version
		versions := make([]string, 0, len(metadata.Versions))
		for v := range metadata.Versions {
			versions = append(versions, v)
		}
		if len(versions) == 0 {
			return "", fmt.Errorf("no versions available for %s", packageName)
		}
		sort.Sort(sort.Reverse(semverSort(versions)))
		return versions[0], nil
	}

	// Handle semver ranges
	return resolveSemverRange(versionRange, metadata.Versions)
}

// GetPackageMetadata fetches package metadata from the npm registry (with cache and retries).
func (c *NPMClient) GetPackageMetadata(ctx context.Context, packageName string) (*NPMRegistryResponse, error) {
	if err := validatePackageName(packageName); err != nil {
		return nil, err
	}
	cacheKey := fmt.Sprintf("npm:meta:%s", packageName)
	cached, err := c.getFromCache(ctx, cacheKey)
	if err == nil && len(cached) > 0 {
		var response NPMRegistryResponse
		if err := json.Unmarshal(cached, &response); err == nil {
			return &response, nil
		}
	}

	url := fmt.Sprintf("%s/%s", NPMRegistryURL, packageName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.doHTTPWithRetry(ctx, req)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return nil, fmt.Errorf("package %s not found", packageName)
		}
		return nil, fmt.Errorf("fetch package metadata: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, NPMMaxMetadataSize))
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}
	if len(body) >= NPMMaxMetadataSize {
		return nil, fmt.Errorf("package metadata exceeds max size (%d bytes)", NPMMaxMetadataSize)
	}

	if err := c.saveToCache(ctx, cacheKey, body, NPMMetaCacheDuration); err != nil {
		c.warnf("npm: failed to cache metadata for %s: %v", packageName, err)
	}

	var response NPMRegistryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse package metadata: %w", err)
	}
	return &response, nil
}

// GetTarballURL returns the tarball URL for a specific package version
func (c *NPMClient) GetTarballURL(ctx context.Context, packageName, version string) (string, error) {
	if err := validatePackageName(packageName); err != nil {
		return "", err
	}
	if version == "" {
		return "", fmt.Errorf("version is empty")
	}
	metadata, err := c.GetPackageMetadata(ctx, packageName)
	if err != nil {
		return "", err
	}
	versionMetadata, ok := metadata.Versions[version]
	if !ok {
		versionMetadata, ok = metadata.Versions["v"+version]
	}
	if !ok {
		return "", fmt.Errorf("version %s not found for package %s", version, packageName)
	}
	if versionMetadata.Dist == nil || versionMetadata.Dist.Tarball == "" {
		return "", fmt.Errorf("no tarball for %s@%s", packageName, version)
	}
	return versionMetadata.Dist.Tarball, nil
}

// DownloadTarball downloads a package tarball (with cache, retries, and size limit).
func (c *NPMClient) DownloadTarball(ctx context.Context, packageName, version string) ([]byte, error) {
	if err := validatePackageName(packageName); err != nil {
		return nil, err
	}
	if version == "" {
		return nil, fmt.Errorf("version is empty")
	}
	cacheKey := fmt.Sprintf("npm:package:%s:%s", packageName, version)
	cached, err := c.getFromCache(ctx, cacheKey)
	if err == nil && len(cached) > 0 {
		return cached, nil
	}

	tarballURL, err := c.GetTarballURL(ctx, packageName, version)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tarballURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create tarball request: %w", err)
	}

	resp, err := c.doHTTPWithRetry(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("download tarball: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, NPMMaxTarballSize))
	if err != nil {
		return nil, fmt.Errorf("read tarball: %w", err)
	}
	if len(body) >= NPMMaxTarballSize {
		return nil, fmt.Errorf("tarball exceeds max size (%d bytes)", NPMMaxTarballSize)
	}

	if err := c.saveToCache(ctx, cacheKey, body, NPMTarballCacheDuration); err != nil {
		c.warnf("npm: failed to cache tarball for %s@%s: %v", packageName, version, err)
	}
	return body, nil
}

// ResolveDependencies resolves all dependencies for a package
func (c *NPMClient) ResolveDependencies(ctx context.Context, deps map[string]string) (map[string]*NPMMetadata, error) {
	result := make(map[string]*NPMMetadata)

	for name, versionRange := range deps {
		// Resolve version range to exact version
		exactVersion, err := c.ResolveVersion(ctx, name, versionRange)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s@%s: %w", name, versionRange, err)
		}

		// Get package metadata
		metadata, err := c.GetPackageMetadata(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("failed to get metadata for %s: %w", name, err)
		}

		versionMetadata, ok := metadata.Versions[exactVersion]
		if !ok {
			versionMetadata, ok = metadata.Versions["v"+exactVersion]
		}
		if !ok {
			return nil, fmt.Errorf("version %s not found for %s", exactVersion, name)
		}

		result[name] = versionMetadata
	}

	return result, nil
}

// BuildDependencyTree builds the complete dependency tree for a set of dependencies
func (c *NPMClient) BuildDependencyTree(ctx context.Context, deps map[string]string) (map[string]*NPMMetadata, error) {
	// First level dependencies
	result, err := c.ResolveDependencies(ctx, deps)
	if err != nil {
		return nil, err
	}

	// Collect all transitive dependencies
	seen := make(map[string]bool)
	queue := make([]string, 0)

	for name := range result {
		seen[name] = true
		queue = append(queue, name)
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		metadata := result[current]
		if metadata == nil || metadata.Dependencies == nil {
			continue
		}

		for depName, depVersion := range metadata.Dependencies {
			if seen[depName] {
				continue
			}
			seen[depName] = true

			exactVersion, err := c.ResolveVersion(ctx, depName, depVersion)
			if err != nil {
				c.warnf("npm: skip transitive dep %s@%s: %v", depName, depVersion, err)
				continue
			}

			allMetadata, err := c.GetPackageMetadata(ctx, depName)
			if err != nil {
				c.warnf("npm: skip metadata for %s: %v", depName, err)
				continue
			}

			depMetadata, ok := allMetadata.Versions[exactVersion]
			if !ok {
				depMetadata, ok = allMetadata.Versions["v"+exactVersion]
			}
			if !ok {
				c.warn("npm: version not found", exactVersion, depName)
				continue
			}

			result[depName] = depMetadata
			queue = append(queue, depName)
		}
	}

	return result, nil
}

// getFromCache retrieves data from the cache (Redis first, then local filesystem).
func (c *NPMClient) getFromCache(ctx context.Context, key string) ([]byte, error) {
	if c.redisClient != nil {
		if data, err := c.getFromRedis(ctx, key); err == nil {
			return data, nil
		}
	}
	if c.cacheDir == "" {
		return nil, fmt.Errorf("cache dir not set")
	}
	path, err := c.cacheFilePath(key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var entry struct {
		Expiry time.Time `json:"expiry"`
		Data   []byte    `json:"data"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	if time.Now().After(entry.Expiry) {
		_ = os.Remove(path)
		return nil, fmt.Errorf("cache expired")
	}
	return entry.Data, nil
}

// cacheFilePath returns a path under cacheDir (no path traversal).
func (c *NPMClient) cacheFilePath(key string) (string, error) {
	hash := sha256.Sum256([]byte(key))
	name := hex.EncodeToString(hash[:]) + ".cache"
	path := filepath.Join(c.cacheDir, name)
	clean := filepath.Clean(path)
	base := filepath.Clean(c.cacheDir)
	rel, err := filepath.Rel(base, clean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid cache path")
	}
	return path, nil
}

// saveToCache writes to Redis (when configured) and then to local filesystem with TTL.
func (c *NPMClient) saveToCache(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	if c.redisClient != nil {
		if err := c.saveToRedis(ctx, key, data, ttl); err != nil {
			c.warnf("redis save failed, falling back to disk cache: %v", err)
		}
	}
	if c.cacheDir == "" {
		return nil
	}
	if err := os.MkdirAll(c.cacheDir, 0750); err != nil {
		return err
	}
	path, err := c.cacheFilePath(key)
	if err != nil {
		return err
	}
	entry := struct {
		Expiry time.Time `json:"expiry"`
		Data   []byte    `json:"data"`
	}{
		Expiry: time.Now().Add(ttl),
		Data:   data,
	}
	entryData, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(path, entryData, 0600)
}

// semverSort implements sort.Interface for semantic version strings
type semverSort []string

func (s semverSort) Len() int      { return len(s) }
func (s semverSort) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s semverSort) Less(i, j int) bool {
	return compareSemver(s[i], s[j]) < 0
}

// compareSemver compares two semantic version strings
// Returns -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareSemver(v1, v2 string) int {
	// Simple semver comparison - strip leading v if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	v1Parts := strings.Split(v1, ".")
	v2Parts := strings.Split(v2, ".")

	for i := 0; i < len(v1Parts) && i < len(v2Parts); i++ {
		v1Num := 0
		v2Num := 0
		fmt.Sscanf(v1Parts[i], "%d", &v1Num) //nolint:errcheck
		fmt.Sscanf(v2Parts[i], "%d", &v2Num) //nolint:errcheck

		if v1Num < v2Num {
			return -1
		}
		if v1Num > v2Num {
			return 1
		}
	}

	if len(v1Parts) < len(v2Parts) {
		return -1
	}
	if len(v1Parts) > len(v2Parts) {
		return 1
	}

	return 0
}

// resolveSemverRange resolves a semver range to an exact version
func resolveSemverRange(rangeStr string, versions map[string]*NPMMetadata) (string, error) {
	// Simple implementation - find the latest version that satisfies the range
	var validVersions []string

	for v := range versions {
		if satisfiesSemver(v, rangeStr) {
			validVersions = append(validVersions, v)
		}
	}

	if len(validVersions) == 0 {
		return "", fmt.Errorf("no version satisfies range %s", rangeStr)
	}

	// Sort by semver and return the latest
	sort.Sort(sort.Reverse(semverSort(validVersions)))
	return validVersions[0], nil
}

// satisfiesSemver checks if a version satisfies a semver range
func satisfiesSemver(version, rangeStr string) bool {
	// Strip leading v
	version = strings.TrimPrefix(version, "v")

	// Handle caret ranges (^1.2.3 -> >=1.2.3 <2.0.0)
	if strings.HasPrefix(rangeStr, "^") {
		base := strings.TrimPrefix(rangeStr, "^")
		return satisfiesCaretRange(version, base)
	}

	// Handle tilde ranges (~1.2.3 -> >=1.2.3 <1.3.0)
	if strings.HasPrefix(rangeStr, "~") {
		base := strings.TrimPrefix(rangeStr, "~")
		return satisfiesTildeRange(version, base)
	}

	// Handle exact version
	return version == rangeStr
}

// satisfiesCaretRange: ^x.y.z means >= x.y.z and < (x+1).0.0
func satisfiesCaretRange(version, base string) bool {
	if compareSemver(version, base) < 0 {
		return false
	}
	baseParts := strings.Split(strings.TrimPrefix(base, "v"), ".")
	major := 0
	fmt.Sscanf(baseParts[0], "%d", &major) //nolint:errcheck
	upper := fmt.Sprintf("%d.0.0", major+1)
	return compareSemver(version, upper) < 0
}

// satisfiesTildeRange: ~x.y.z means >= x.y.z and < x.(y+1).0
func satisfiesTildeRange(version, base string) bool {
	if compareSemver(version, base) < 0 {
		return false
	}
	baseParts := strings.Split(strings.TrimPrefix(base, "v"), ".")
	if len(baseParts) < 2 {
		return true
	}
	major, minor := 0, 0
	fmt.Sscanf(baseParts[0], "%d", &major) //nolint:errcheck
	fmt.Sscanf(baseParts[1], "%d", &minor) //nolint:errcheck
	upper := fmt.Sprintf("%d.%d.0", major, minor+1)
	return compareSemver(version, upper) < 0
}
