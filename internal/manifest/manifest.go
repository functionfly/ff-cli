package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Manifest represents the functionfly.json configuration
type Manifest struct {
	Schema        string            `json:"$schema,omitempty"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Runtime       string            `json:"runtime"`
	Entry         string            `json:"entry,omitempty"`
	Public        *bool             `json:"public,omitempty"`
	Deterministic *bool             `json:"deterministic,omitempty"`
	CacheTTL      *int              `json:"cache_ttl,omitempty"`
	TimeoutMS     *int              `json:"timeout_ms,omitempty"`
	MemoryMB      *int              `json:"memory_mb,omitempty"`
	Description   string            `json:"description,omitempty"`
	Dependencies  map[string]string `json:"dependencies,omitempty"`
	Env           map[string]string `json:"env,omitempty"`
	// MicroPython-specific fields (FlyPy has been disabled)
	InputSchema  map[string]interface{} `json:"input_schema,omitempty"`
	OutputSchema map[string]interface{} `json:"output_schema,omitempty"`
	Idempotent   *bool                  `json:"idempotent,omitempty"`
	SideEffects  string                 `json:"side_effects,omitempty"`
	Capabilities []string               `json:"capabilities,omitempty"`
	MainFile     string                 `json:"main_file,omitempty"`
	// TypeScript type checking options
	TypeCheck     *bool  `json:"typeCheck,omitempty"`     // Enable/disable type checking
	TSConfig      string `json:"tsConfig,omitempty"`      // Custom tsconfig path
	StrictMode    *bool  `json:"strictMode,omitempty"`    // Enforce strict TypeScript
	SkipTypeCheck *bool  `json:"skipTypeCheck,omitempty"` // Skip type checking (for legacy code)
	// npm package options
	IncludePackages *bool  `json:"includePackages,omitempty"` // Include npm packages in bundle
	PackageCache    string `json:"packageCache,omitempty"`    // Custom package cache path
}

// Default values
const (
	DefaultPublic        = true
	DefaultDeterministic = false
	DefaultCacheTTL      = 3600
	DefaultTimeoutMS     = 5000
	DefaultMemoryMB      = 128
)

// Default manifest filename (JSONC format)
const DefaultManifestFile = "functionfly.jsonc"
const LegacyManifestFile = "functionfly.json"

// Load reads and parses the functionfly.jsonc file (with fallback to .json)
func Load(path string) (*Manifest, error) {
	var data []byte
	var err error

	if path == "" {
		// Try .jsonc first, then fall back to .json
		data, err = os.ReadFile(DefaultManifestFile)
		if err != nil {
			// Fall back to legacy .json
			data, err = os.ReadFile(LegacyManifestFile)
		}
	} else {
		// Use specified path - try .jsonc first, then .json
		data, err = os.ReadFile(forceJSONCExtension(path))
		if err != nil {
			// Try with .json extension
			data, err = os.ReadFile(forceJSONExtension(path))
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read manifest file: %w", err)
	}

	// Strip comments if present (for JSONC compatibility)
	jsonContent := StripComments(string(data))

	var manifest Manifest
	if err := json.Unmarshal([]byte(jsonContent), &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest JSON: %w", err)
	}

	// Apply defaults
	manifest.applyDefaults()

	return &manifest, nil
}

// forceJSONCExtension ensures the path has .jsonc extension
func forceJSONCExtension(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + ".jsonc"
	}
	return strings.TrimSuffix(path, ext) + ".jsonc"
}

// forceJSONExtension ensures the path has .json extension
func forceJSONExtension(path string) string {
	ext := filepath.Ext(path)
	if ext == "" {
		return path + ".json"
	}
	return strings.TrimSuffix(path, ext) + ".json"
}

// Save writes the manifest to functionfly.jsonc with helpful comments
func Save(manifest *Manifest, path string) error {
	if path == "" {
		path = DefaultManifestFile
	}

	// Ensure .jsonc extension
	if !strings.HasSuffix(path, ".jsonc") {
		path = strings.TrimSuffix(path, ".json") + ".jsonc"
	}

	// Generate JSON with comments
	content := generateJSONCWithComments(manifest)

	return os.WriteFile(path, []byte(content), 0600)
}

// generateJSONCWithComments generates a JSONC-formatted manifest with helpful comments
func generateJSONCWithComments(m *Manifest) string {
	var sb strings.Builder

	sb.WriteString("{\n")

	// name - required
	sb.WriteString(`  // Function name: lowercase letters, numbers, and hyphens only
`)
	sb.WriteString(fmt.Sprintf(`  "name": "%s",
`, m.Name))

	// version - required
	sb.WriteString(`  // Semantic version (x.y.z)
`)
	sb.WriteString(fmt.Sprintf(`  "version": "%s",
`, m.Version))

	// runtime - required
	sb.WriteString(`  // Runtime: node18, node20, python3.11, python3.12, deno
`)
	sb.WriteString(fmt.Sprintf(`  "runtime": "%s",
`, m.Runtime))

	// entry - optional
	if m.Entry != "" {
		sb.WriteString(`  // Entry file path (optional, auto-detected if not specified)
`)
		sb.WriteString(fmt.Sprintf(`  "entry": "%s",
`, escapeString(m.Entry)))
	}

	// public
	sb.WriteString(`  // Make function publicly accessible (default: true)
`)
	if m.Public != nil {
		sb.WriteString(fmt.Sprintf(`  "public": %t,
`, *m.Public))
	} else {
		sb.WriteString(`  "public": true,
`)
	}

	// deterministic
	sb.WriteString(`  // Enable deterministic caching (default: false)
`)
	if m.Deterministic != nil {
		sb.WriteString(fmt.Sprintf(`  "deterministic": %t,
`, *m.Deterministic))
	}

	// cache_ttl
	sb.WriteString(`  // Cache TTL in seconds (default: 3600, max: 86400)
`)
	if m.CacheTTL != nil {
		sb.WriteString(fmt.Sprintf(`  "cache_ttl": %d,
`, *m.CacheTTL))
	}

	// timeout_ms
	sb.WriteString(`  // Execution timeout in milliseconds (default: 5000, max: 30000)
`)
	if m.TimeoutMS != nil {
		sb.WriteString(fmt.Sprintf(`  "timeout_ms": %d,
`, *m.TimeoutMS))
	}

	// memory_mb
	sb.WriteString(`  // Memory allocation in MB: 128, 256, 512, 1024 (default: 128)
`)
	if m.MemoryMB != nil {
		sb.WriteString(fmt.Sprintf(`  "memory_mb": %d,
`, *m.MemoryMB))
	}

	// description
	if m.Description != "" {
		sb.WriteString(`  // Human-readable description of the function
`)
		sb.WriteString(fmt.Sprintf(`  "description": "%s",
`, escapeString(m.Description)))
	}

	// dependencies
	if len(m.Dependencies) > 0 {
		sb.WriteString(`  // NPM/Python dependencies
`)
		sb.WriteString(`  "dependencies": `)
		depsJSON, _ := json.Marshal(m.Dependencies)
		sb.WriteString(string(depsJSON))
		sb.WriteString(",\n")
	}

	// env
	if len(m.Env) > 0 {
		sb.WriteString(`  // Environment variables (available at runtime)
`)
		sb.WriteString(`  "env": `)
		envJSON, _ := json.Marshal(m.Env)
		sb.WriteString(string(envJSON))
		sb.WriteString("\n")
	}

	// typeCheck - TypeScript type checking
	if m.TypeCheck != nil {
		sb.WriteString(`  // Enable/disable TypeScript type checking (default: true for TypeScript files)
`)
		sb.WriteString(fmt.Sprintf(`  "typeCheck": %t,\n`, *m.TypeCheck))
	}

	// tsConfig - custom tsconfig path
	if m.TSConfig != "" {
		sb.WriteString(`  // Custom tsconfig.json path for type checking
`)
		sb.WriteString(fmt.Sprintf(`  "tsConfig": "%s",\n`, escapeString(m.TSConfig)))
	}

	// strictMode - enforce strict TypeScript
	if m.StrictMode != nil {
		sb.WriteString(`  // Enforce strict TypeScript mode (default: false)
`)
		sb.WriteString(fmt.Sprintf(`  "strictMode": %t,\n`, *m.StrictMode))
	}

	// skipTypeCheck - skip type checking entirely
	if m.SkipTypeCheck != nil {
		sb.WriteString(`  // Skip TypeScript type checking (for legacy code)
`)
		sb.WriteString(fmt.Sprintf(`  "skipTypeCheck": %t,\n`, *m.SkipTypeCheck))
	}

	sb.WriteString("}\n")

	return sb.String()
}

// escapeString escapes special characters in a JSON string
func escapeString(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

// Validate checks the manifest for correctness
func (m *Manifest) Validate() error {
	// Required fields
	if m.Name == "" {
		return fmt.Errorf("name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("version is required")
	}
	if m.Runtime == "" {
		return fmt.Errorf("runtime is required")
	}

	// Name validation
	nameRegex := regexp.MustCompile(`^[a-z0-9-]+$`)
	if !nameRegex.MatchString(m.Name) {
		return fmt.Errorf("name must contain only lowercase letters, numbers, and hyphens")
	}
	if len(m.Name) > 64 {
		return fmt.Errorf("name must be 64 characters or less")
	}
	if strings.HasPrefix(m.Name, "-") || strings.HasSuffix(m.Name, "-") {
		return fmt.Errorf("name cannot start or end with a hyphen")
	}

	// Version validation (semver)
	versionRegex := regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	if !versionRegex.MatchString(m.Version) {
		return fmt.Errorf("version must be in semver format (x.y.z)")
	}

	// Runtime validation
	validRuntimes := map[string]bool{
		"node18":       true,
		"node20":       true,
		"python3.11":   true,
		"deno":         true,
		"bun":          true,
		"rust":         true,
		"browser-wasm": true, // Browser Native WebAssembly (0ms cold start)
	}
	if !validRuntimes[m.Runtime] {
		return fmt.Errorf("runtime must be one of: node18, node20, python3.11, deno, bun, rust, browser-wasm")
	}

	// Entry file validation (if provided)
	if m.Entry != "" {
		// Check for invalid characters in path
		if strings.Contains(m.Entry, "..") {
			return fmt.Errorf("entry file path cannot contain '..' for security reasons")
		}
		if strings.Contains(m.Entry, "/") || strings.Contains(m.Entry, "\\") {
			return fmt.Errorf("entry file path cannot contain directory separators")
		}
		// Validate extension matches runtime
		validExtensions := map[string][]string{
			"node18":       {".js", ".ts"},
			"node20":       {".js", ".ts"},
			"python3.11":   {".py"},
			"deno":         {".js", ".ts"},
			"bun":          {".js", ".ts"},
			"rust":         {".rs"},
			"browser-wasm": {".wasm", ".wat"},
		}
		ext := filepath.Ext(m.Entry)
		if validExts, ok := validExtensions[m.Runtime]; ok {
			valid := false
			for _, validExt := range validExts {
				if ext == validExt {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("entry file extension '%s' is not valid for runtime '%s' (valid: %v)", ext, m.Runtime, validExts)
			}
		}
	}

	// Memory validation
	if m.MemoryMB != nil {
		validMemory := map[int]bool{128: true, 256: true, 512: true, 1024: true}
		if !validMemory[*m.MemoryMB] {
			return fmt.Errorf("memory_mb must be one of: 128, 256, 512, 1024")
		}
	}

	// Cache TTL validation
	if m.CacheTTL != nil && (*m.CacheTTL < 0 || *m.CacheTTL > 86400) {
		return fmt.Errorf("cache_ttl must be between 0 and 86400 seconds")
	}

	// Timeout validation
	if m.TimeoutMS != nil && (*m.TimeoutMS < 1000 || *m.TimeoutMS > 30000) {
		return fmt.Errorf("timeout_ms must be between 1000 and 30000 milliseconds")
	}

	// Description length
	if len(m.Description) > 500 {
		return fmt.Errorf("description must be 500 characters or less")
	}

	return nil
}

// applyDefaults sets default values for optional fields
func (m *Manifest) applyDefaults() {
	if m.Public == nil {
		defaultPublic := DefaultPublic
		m.Public = &defaultPublic
	}
	if m.Deterministic == nil {
		defaultDeterministic := DefaultDeterministic
		m.Deterministic = &defaultDeterministic
	}
	if m.CacheTTL == nil {
		defaultCacheTTL := DefaultCacheTTL
		m.CacheTTL = &defaultCacheTTL
	}
	if m.TimeoutMS == nil {
		defaultTimeoutMS := DefaultTimeoutMS
		m.TimeoutMS = &defaultTimeoutMS
	}
	if m.MemoryMB == nil {
		defaultMemoryMB := DefaultMemoryMB
		m.MemoryMB = &defaultMemoryMB
	}
}

// Getters with defaults applied
func (m *Manifest) GetPublic() bool {
	if m.Public != nil {
		return *m.Public
	}
	return DefaultPublic
}

func (m *Manifest) GetDeterministic() bool {
	if m.Deterministic != nil {
		return *m.Deterministic
	}
	return DefaultDeterministic
}

func (m *Manifest) GetCacheTTL() int {
	if m.CacheTTL != nil {
		return *m.CacheTTL
	}
	return DefaultCacheTTL
}

func (m *Manifest) GetTimeoutMS() int {
	if m.TimeoutMS != nil {
		return *m.TimeoutMS
	}
	return DefaultTimeoutMS
}

func (m *Manifest) GetMemoryMB() int {
	if m.MemoryMB != nil {
		return *m.MemoryMB
	}
	return DefaultMemoryMB
}

// String returns a human-readable representation
func (m *Manifest) String() string {
	public := "private"
	if m.GetPublic() {
		public = "public"
	}

	deterministic := "no"
	if m.GetDeterministic() {
		deterministic = "yes"
	}

	return fmt.Sprintf("%s@%s (%s, %s, %dms timeout, %dMB memory, cache: %s)",
		m.Name, m.Version, m.Runtime, public, m.GetTimeoutMS(), m.GetMemoryMB(), deterministic)
}
