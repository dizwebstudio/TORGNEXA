// Package connectorsandbox provides the host-owned Connector SDK dry-run and
// reference sandbox boundary. Provider code never receives direct host network,
// filesystem, environment or production-secret authority.
package connectorsandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

const ResultVersion = 1

var (
	ErrInvalidOperation     = errors.New("connector sandbox: invalid operation")
	ErrInvalidPlan          = errors.New("connector sandbox: invalid admission plan")
	ErrCapabilityDenied     = errors.New("connector sandbox: capability denied")
	ErrSecretDenied         = errors.New("connector sandbox: secret denied")
	ErrProductionCredential = errors.New("connector sandbox: production credential forbidden")
	ErrEgressDenied         = errors.New("connector sandbox: egress denied")
	ErrResourceLimit        = errors.New("connector sandbox: resource limit exceeded")
	ErrSandboxUnavailable   = errors.New("connector sandbox: linux isolation unavailable")
	ErrPolicyDenied         = errors.New("connector sandbox: host policy denied")
)

var safeToken = regexp.MustCompile(`^[a-z][a-z0-9._:/-]{0,127}$`)

type Mode string

const (
	ModeDryRun Mode = "dry_run"
	ModeTest   Mode = "test"
)

func (m Mode) Valid() bool { return m == ModeDryRun || m == ModeTest }

type CredentialTier string

const (
	CredentialSandbox    CredentialTier = "sandbox"
	CredentialProduction CredentialTier = "production"
)

func (t CredentialTier) Valid() bool { return t == CredentialSandbox || t == CredentialProduction }

type CredentialBinding struct {
	Reference sdk.SecretReference `json:"secret_reference,omitempty"`
	Tier      CredentialTier      `json:"tier"`
}

func (b CredentialBinding) Validate() error {
	if !b.Tier.Valid() || !b.Reference.Valid() {
		return ErrInvalidOperation
	}
	return nil
}

type Operation struct {
	RequestID        string         `json:"request_id"`
	ExtensionID      string         `json:"connector_id"`
	ExtensionVersion string         `json:"connector_version"`
	Capability       sdk.Capability `json:"capability"`
	ResourceType     string         `json:"resource_type"`
	ResourceID       string         `json:"resource_id"`
}

func (o Operation) Validate() error {
	if !safeToken.MatchString(o.RequestID) || !safeToken.MatchString(o.ExtensionID) || o.ExtensionVersion == "" ||
		!safeToken.MatchString(o.ResourceType) || !safeToken.MatchString(o.ResourceID) {
		return ErrInvalidOperation
	}
	if _, ok := sdk.CapabilityDefinitionFor(o.Capability); !ok {
		return ErrInvalidOperation
	}
	return nil
}

type ChangeKind string

const (
	ChangeCreate ChangeKind = "create"
	ChangeUpdate ChangeKind = "update"
	ChangeDelete ChangeKind = "delete"
)

func (k ChangeKind) Valid() bool { return k == ChangeCreate || k == ChangeUpdate || k == ChangeDelete }

type Change struct {
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	Kind         ChangeKind `json:"kind"`
	BeforeSHA256 string     `json:"before_sha256,omitempty"`
	AfterSHA256  string     `json:"after_sha256,omitempty"`
}

func DigestCanonical(value any) string {
	data, _ := json.Marshal(value)
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

type ExternalAction struct {
	Method        string                            `json:"method"`
	Destination   pluginsecurity.NetworkDestination `json:"destination"`
	RouteTemplate string                            `json:"route_template"`
}

type Usage struct {
	WallTimeMS   int64 `json:"wall_time_ms"`
	CPUTimeMS    int64 `json:"cpu_time_ms"`
	PeakRSSBytes int64 `json:"peak_rss_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
}

type IsolationEvidence struct {
	ProductionCredentialsBlocked bool `json:"production_credentials_blocked"`
	EnvironmentIsolated          bool `json:"environment_isolated"`
	FilesystemIsolated           bool `json:"filesystem_isolated"`
	DirectNetworkBlocked         bool `json:"direct_network_blocked"`
	EgressMediated               bool `json:"egress_mediated"`
	ResourceLimitsEnforced       bool `json:"resource_limits_enforced"`
}

type ResultStatus string

const (
	StatusPlanned       ResultStatus = "planned"
	StatusSucceeded     ResultStatus = "succeeded"
	StatusRejected      ResultStatus = "rejected"
	StatusLimitExceeded ResultStatus = "limit_exceeded"
)

type OperationResult struct {
	Version          int               `json:"version"`
	Mode             Mode              `json:"mode"`
	Status           ResultStatus      `json:"status"`
	RequestID        string            `json:"request_id"`
	ExtensionID      string            `json:"connector_id"`
	ExtensionVersion string            `json:"connector_version"`
	Capability       sdk.Capability    `json:"capability"`
	Changes          []Change          `json:"changes,omitempty"`
	ExternalActions  []ExternalAction  `json:"external_actions,omitempty"`
	ReasonCode       string            `json:"reason_code,omitempty"`
	Usage            Usage             `json:"usage"`
	Isolation        IsolationEvidence `json:"isolation"`
	CompletedAt      time.Time         `json:"completed_at"`
}

func validatePlan(plan pluginsecurity.AdmissionPlan) error {
	if plan.BoundaryVersion != pluginsecurity.BoundaryVersion || plan.ExtensionID == "" || plan.ExtensionVersion == "" ||
		len(plan.ArtifactSHA256) != 64 || !plan.ExecutionMode.Valid() || !plan.Trust.Valid() || plan.Limits.Validate() != nil || plan.Granted.Validate() != nil {
		return ErrInvalidPlan
	}
	if plan.Granted.ExtensionID != plan.ExtensionID || plan.Granted.ExtensionVersion != plan.ExtensionVersion || plan.Granted.ArtifactSHA256 != plan.ArtifactSHA256 {
		return ErrInvalidPlan
	}
	return nil
}

func capabilityGranted(plan pluginsecurity.AdmissionPlan, capability sdk.Capability) bool {
	for _, value := range plan.Granted.Capabilities {
		if value == capability {
			return true
		}
	}
	return false
}
func secretGranted(plan pluginsecurity.AdmissionPlan, class string) bool {
	for _, value := range plan.Granted.SecretClasses {
		if value == class {
			return true
		}
	}
	return false
}
func networkGranted(plan pluginsecurity.AdmissionPlan, destination pluginsecurity.NetworkDestination) bool {
	for _, value := range plan.Granted.Network {
		if value.Host == destination.Host && value.Port == destination.Port {
			return true
		}
	}
	return false
}
func safeRoute(route string) bool {
	return strings.HasPrefix(route, "/") && len(route) <= 256 && !strings.ContainsAny(route, "?#\\\r\n") && !strings.Contains(route, "..")
}
