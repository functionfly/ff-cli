package implant

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type ImplantManifest struct {
	ID             string                 `yaml:"id" json:"id"`
	Name           string                 `yaml:"name" json:"name"`
	Version        string                 `yaml:"version" json:"version"`
	Description    string                 `yaml:"description" json:"description"`
	Category       string                 `yaml:"category" json:"category"`
	Implements     []string               `yaml:"implements,omitempty" json:"implements,omitempty"`
	Permissions    []string               `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	Events         []EventSpec            `yaml:"events,omitempty" json:"events,omitempty"`
	Workflows      []WorkflowSpec         `yaml:"workflows,omitempty" json:"workflows,omitempty"`
	Intents        []string               `yaml:"intents,omitempty" json:"intents,omitempty"`
	Phases         []string               `yaml:"phases,omitempty" json:"phases,omitempty"`
	Deprecated     bool                   `yaml:"deprecated,omitempty" json:"deprecated,omitempty"`
	SunsetDate     string                 `yaml:"sunset_date,omitempty" json:"sunset_date,omitempty"`
	Certifies      []string               `yaml:"certifies,omitempty" json:"certifies,omitempty"`
	Changelog      string                 `yaml:"changelog,omitempty" json:"changelog,omitempty"`
	Build          *BuildSpec             `yaml:"build,omitempty" json:"build,omitempty"`
	Entrypoint     string                 `yaml:"entrypoint,omitempty" json:"entrypoint,omitempty"`
	Tags           []string               `yaml:"tags,omitempty" json:"tags,omitempty"`
	OAuthProvider  string                 `yaml:"oauth_provider,omitempty" json:"oauth_provider,omitempty"`
	OAuthConfig    map[string]interface{} `yaml:"oauth_config,omitempty" json:"oauth_config,omitempty"`
	RepositoryURL  string                 `yaml:"repository_url,omitempty" json:"repository_url,omitempty"`
	HomepageURL    string                 `yaml:"homepage_url,omitempty" json:"homepage_url,omitempty"`
	License        string                 `yaml:"license,omitempty" json:"license,omitempty"`
	Actions        []ActionSpec           `yaml:"actions,omitempty" json:"actions,omitempty"`
	Authentication *AuthenticationSpec    `yaml:"authentication,omitempty" json:"authentication,omitempty"`
}

type AuthenticationSpec struct {
	Type        string   `yaml:"type" json:"type"`
	Scopes      []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	SecretVault string   `yaml:"secret_vault,omitempty" json:"secret_vault,omitempty"`
}

type EventSpec struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Payload     string `yaml:"payload,omitempty" json:"payload,omitempty"`
}

type WorkflowSpec struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Steps       []string `yaml:"steps,omitempty" json:"steps,omitempty"`
}

type BuildSpec struct {
	Runtime  string `yaml:"runtime,omitempty" json:"runtime,omitempty"`
	Output   string `yaml:"output,omitempty" json:"output,omitempty"`
	Memory   string `yaml:"memory,omitempty" json:"memory,omitempty"`
	Storage  string `yaml:"storage,omitempty" json:"storage,omitempty"`
}

type ActionSpec struct {
	Name        string `yaml:"name" json:"name"`
	DisplayName string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

func ParseManifest(data []byte) (*ImplantManifest, error) {
	var m ImplantManifest
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse manifest JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse manifest YAML: %w", err)
		}
	}
	return &m, nil
}

func (m *ImplantManifest) Validate() error {
	if m == nil {
		return errors.New("manifest is nil")
	}
	if strings.TrimSpace(m.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("version is required")
	}
	if err := ValidateSemver(m.Version); err != nil {
		return fmt.Errorf("version %q is not valid semver: %w", m.Version, err)
	}
	if strings.TrimSpace(m.Category) == "" {
		return errors.New("category is required")
	}

	actionNames := map[string]bool{}
	for _, a := range m.Actions {
		if strings.TrimSpace(a.Name) == "" {
			return errors.New("action entries must declare a name")
		}
		actionNames[a.Name] = true
	}

	if len(m.Certifies) > 0 && len(m.Actions) == 0 {
		return errors.New("manifest declares `certifies` but contains no actions")
	}
	for _, c := range m.Certifies {
		if !actionNames[c] {
			return fmt.Errorf("certifies references unknown action %q", c)
		}
	}

	if len(m.Implements) > 0 {
		for _, i := range m.Implements {
			if strings.TrimSpace(i) == "" {
				return errors.New("implements contains an empty entry")
			}
		}
	}

	return nil
}

func ValidateSemver(v string) error {
	semverRe := regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z\-\.]+)?(?:\+[0-9A-Za-z\-\.]+)?$`)
	if !semverRe.MatchString(v) {
		return errors.New("expected MAJOR.MINOR.PATCH with optional pre-release and build metadata")
	}
	parts := strings.SplitN(v, ".", 3)
	for _, p := range parts[:3] {
		clean := p
		if idx := strings.Index(p, "-"); idx >= 0 {
			clean = p[:idx]
		}
		if _, err := strconv.Atoi(clean); err != nil {
			return fmt.Errorf("non-numeric version component %q", clean)
		}
	}
	return nil
}

func (m *ImplantManifest) ToYAML() ([]byte, error) {
	return yaml.Marshal(m)
}

func (m *ImplantManifest) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}
