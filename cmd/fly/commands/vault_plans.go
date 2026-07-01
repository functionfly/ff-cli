package commands

import (
	"fmt"
)

// Plan constants mirror the server-side plans package.
const (
	PlanFree            = "free"
	PlanStarter         = "starter"
	PlanPro             = "professional"
	PlanEnterprise      = "enterprise"
	PlanEnterpriseSLA   = "enterprise_sla"
	PlanAgentEnterprise = "agent_enterprise"
)

// VaultFeature identifies a gated vault capability.
type VaultFeature string

const (
	// Vault features
	VaultFeatureNamespaces   VaultFeature = "namespaces"
	VaultFeatureMFA          VaultFeature = "mfa"
	VaultFeatureBreakGlass   VaultFeature = "break_glass"
	VaultFeatureAuditExport  VaultFeature = "audit_export"
	VaultFeatureIPAllowlist  VaultFeature = "ip_allowlist"
	VaultFeatureExpiration   VaultFeature = "expiration"
	VaultFeatureRBAC         VaultFeature = "rbac"
	VaultFeatureShares       VaultFeature = "shares"
	VaultFeatureEscrow       VaultFeature = "escrow"
	VaultFeatureSIEM         VaultFeature = "siem_webhooks"
	VaultFeatureSSO          VaultFeature = "sso"
	VaultFeatureHAStatus     VaultFeature = "ha_status"
	VaultFeatureCacheStats   VaultFeature = "cache_stats"

	// Platform features
	FeatureCanary        VaultFeature = "canary_deployments"
	FeatureSchedule      VaultFeature = "scheduled_executions"
	FeatureLiveLogs      VaultFeature = "live_log_streaming"
)

// featureRequirements maps each feature to the minimum plan required.
var featureRequirements = map[VaultFeature]string{
	// Vault features
	VaultFeatureNamespaces:  PlanPro,
	VaultFeatureMFA:         PlanPro,
	VaultFeatureBreakGlass:  PlanPro,
	VaultFeatureAuditExport: PlanPro,
	VaultFeatureIPAllowlist: PlanPro,
	VaultFeatureExpiration:  PlanPro,
	VaultFeatureRBAC:        PlanEnterprise,
	VaultFeatureShares:      PlanEnterprise,
	VaultFeatureEscrow:      PlanEnterprise,
	VaultFeatureSIEM:        PlanEnterprise,
	VaultFeatureSSO:         PlanAgentEnterprise,
	VaultFeatureHAStatus:    PlanAgentEnterprise,
	VaultFeatureCacheStats:  PlanEnterprise,
	// Platform features
	FeatureCanary:   PlanPro,
	FeatureSchedule: PlanStarter,
	FeatureLiveLogs: PlanStarter,
}

// planRank returns a numeric rank for comparison. Higher = more premium.
func planRank(plan string) int {
	switch plan {
	case PlanFree, "":
		return 0
	case PlanStarter:
		return 1
	case PlanPro:
		return 2
	case PlanEnterprise, PlanEnterpriseSLA:
		return 3
	case PlanAgentEnterprise:
		return 4
	default:
		return 0
	}
}

// supportsVaultFeature returns true if the given plan supports the feature.
func supportsVaultFeature(plan string, feature VaultFeature) bool {
	required, ok := featureRequirements[feature]
	if !ok {
		return true
	}
	return planRank(plan) >= planRank(required)
}

// requireVaultPlan checks if the current user's plan supports a vault feature.
// Returns a descriptive CLIError if access is denied.
func requireVaultPlan(feature VaultFeature) error {
	creds, err := LoadCredentials()
	if err != nil {
		return nil
	}
	plan := creds.User.Plan
	if supportsVaultFeature(plan, feature) {
		return nil
	}
	required := featureRequirements[feature]
	return NewCLIError(
		fmt.Errorf("%s requires %s plan or higher (current: %s)", feature, planDisplayName(required), planDisplayName(plan)),
		ExitCodeAuthError,
		fmt.Sprintf("%s requires %s plan or higher\n   → Current plan: %s\n   → Upgrade at: https://functionfly.com/billing", feature, planDisplayName(required), planDisplayName(plan)),
	)
}

// planDisplayName returns a human-readable plan name.
func planDisplayName(plan string) string {
	switch plan {
	case PlanFree, "":
		return "Free"
	case PlanStarter:
		return "Starter"
	case PlanPro:
		return "Professional"
	case PlanEnterprise, PlanEnterpriseSLA:
		return "Enterprise"
	case PlanAgentEnterprise:
		return "Agent Enterprise"
	default:
		return plan
	}
}
