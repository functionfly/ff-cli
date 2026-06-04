package cli

import (
	"time"

	"github.com/google/uuid"
)

// DeployRequest represents a deployment request to the API
type DeployRequest struct {
	Provider       string                 `json:"provider"`
	Region         string                 `json:"region"`
	Artifact       string                 `json:"artifact"` // Base64 encoded
	Routes         []string               `json:"routes,omitempty"`
	EnvVars        map[string]string      `json:"env_vars,omitempty"`
	Secrets        map[string]string      `json:"secrets,omitempty"`
	ProviderConfig map[string]interface{} `json:"provider_config,omitempty"`
}

// DeployResponse represents the response from a deployment API call
type DeployResponse struct {
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}

// Deployment represents a deployment record
type Deployment struct {
	ID           uuid.UUID              `json:"id"`
	AppID        uuid.UUID              `json:"app_id"`
	Provider     string                 `json:"provider"`
	Region       string                 `json:"region"`
	DeploymentID string                 `json:"deployment_id"`
	ArtifactKey  string                 `json:"artifact_key"`
	Routes       []string               `json:"routes"`
	Status       string                 `json:"status"`
	Message      string                 `json:"message"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// ListDeploymentsResponse represents the response from listing deployments
type ListDeploymentsResponse struct {
	Deployments []*Deployment `json:"deployments"`
}

// RollbackRequest represents a rollback request
type RollbackRequest struct {
	ToDeploymentID string `json:"to_deployment_id"`
}

// RollbackResponse represents the response from a rollback API call
type RollbackResponse struct {
	DeploymentID string `json:"deployment_id"`
	Status       string `json:"status"`
	Message      string `json:"message"`
}
