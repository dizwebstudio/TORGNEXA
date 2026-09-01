package connectors

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// ReadinessStatus is the provider-neutral maturity of a connector runtime.
// A manifest or a successful health probe alone can never produce ready or
// qualified.
type ReadinessStatus string

const (
	ReadinessManifestOnly          ReadinessStatus = "manifest_only"
	ReadinessHealthOnly            ReadinessStatus = "health_only"
	ReadinessReadOnly              ReadinessStatus = "read_only"
	ReadinessPartiallySupported    ReadinessStatus = "partially_supported"
	ReadinessReady                 ReadinessStatus = "ready"
	ReadinessQualified             ReadinessStatus = "qualified"
	ReadinessDegraded              ReadinessStatus = "degraded"
	ReadinessReauthorizationNeeded ReadinessStatus = "reauthorization_required"
	ReadinessNotAvailable          ReadinessStatus = "not_available"
)

var validReadinessStatuses = map[ReadinessStatus]struct{}{
	ReadinessManifestOnly: {}, ReadinessHealthOnly: {}, ReadinessReadOnly: {},
	ReadinessPartiallySupported: {}, ReadinessReady: {}, ReadinessQualified: {},
	ReadinessDegraded: {}, ReadinessReauthorizationNeeded: {}, ReadinessNotAvailable: {},
}

// ReadinessCapability is a non-secret capability evidence projection.
type ReadinessCapability struct {
	Name               string   `json:"name"`
	Status             string   `json:"status"`
	Direction          string   `json:"direction"`
	RequiredScopes     []string `json:"required_scopes,omitempty"`
	RiskClass          string   `json:"risk_class"`
	Idempotency        string   `json:"idempotency"`
	ReadAfterWrite     string   `json:"read_after_write"`
	WebhookOrReconcile bool     `json:"webhook_or_reconciliation"`
	RuntimeEvidence    string   `json:"runtime_evidence"`
}

// ReadinessRateLimit contains safe operational limits from the reviewed
// manifest. It deliberately excludes credentials and remote payloads.
type ReadinessRateLimit struct {
	MaxConcurrency   int `json:"max_concurrency"`
	MinIntervalMS    int `json:"min_interval_ms"`
	RequestTimeoutMS int `json:"request_timeout_ms"`
	RetryMaxAttempts int `json:"retry_max_attempts"`
}

// ReadinessProfile describes one connector across the catalog and runtime.
type ReadinessProfile struct {
	ID                      string                `json:"connector_id"`
	DisplayName             string                `json:"display_name"`
	Family                  string                `json:"family"`
	Surface                 string                `json:"surface"`
	Status                  ReadinessStatus       `json:"status"`
	Owner                   string                `json:"owner"`
	Priority                string                `json:"priority"`
	Decision                string                `json:"decision"`
	NextAction              string                `json:"next_action"`
	OfficialDocsRef         string                `json:"official_docs_ref"`
	OfficialDocsStatus      string                `json:"official_docs_status"`
	SandboxStatus           string                `json:"sandbox_status"`
	LiveQualificationStatus string                `json:"live_qualification_status"`
	LastVerifiedAt          string                `json:"last_verified_at"`
	ConformanceRef          string                `json:"conformance_ref"`
	RuntimeRef              string                `json:"runtime_ref"`
	HealthOnly              bool                  `json:"health_only"`
	Capabilities            []ReadinessCapability `json:"capabilities"`
	Blockers                []string              `json:"blockers"`
	RateLimit               ReadinessRateLimit    `json:"rate_limit"`
}

// ReadinessSummary is calculated from the immutable catalog snapshot.
type ReadinessSummary struct {
	Total                   int `json:"total"`
	ManifestOnly            int `json:"manifest_only"`
	HealthOnly              int `json:"health_only"`
	ReadOnly                int `json:"read_only"`
	PartiallySupported      int `json:"partially_supported"`
	Ready                   int `json:"ready"`
	Qualified               int `json:"qualified"`
	Degraded                int `json:"degraded"`
	ReauthorizationRequired int `json:"reauthorization_required"`
	NotAvailable            int `json:"not_available"`
}

// ReadinessMatrix is the API and SDK view of connector depth. Profiles are
// sorted by connector ID and contain no secret material.
type ReadinessMatrix struct {
	SchemaVersion int                `json:"schema_version"`
	GeneratedAt   time.Time          `json:"generated_at"`
	Consistency   string             `json:"consistency"`
	Summary       ReadinessSummary   `json:"summary"`
	Profiles      []ReadinessProfile `json:"profiles"`
}

// ReadinessCatalog returns the checked-in, redacted readiness evidence.
func ReadinessCatalog() ([]ReadinessProfile, error) {
	var payload struct {
		Profiles []ReadinessProfile `json:"profiles"`
	}
	if err := json.Unmarshal([]byte(generatedReadinessMatrixJSON), &payload); err != nil {
		return nil, err
	}
	profiles := payload.Profiles
	if err := validateReadinessProfiles(profiles); err != nil {
		return nil, err
	}
	return cloneReadinessProfiles(profiles), nil
}

// ReadinessProfileFor resolves one connector profile from the reviewed
// catalog. The returned profile owns its slices and may be safely modified by
// the caller without changing the generated snapshot.
func ReadinessProfileFor(profileID string) (ReadinessProfile, error) {
	profiles, err := ReadinessCatalog()
	if err != nil {
		return ReadinessProfile{}, err
	}
	for _, profile := range profiles {
		if profile.ID == profileID {
			return profile, nil
		}
	}
	return ReadinessProfile{}, ErrConnectorNotFound
}

// ReadinessSnapshot returns an immutable repository snapshot suitable for
// tenant-authorized read APIs. It does not call a remote connector.
func ReadinessSnapshot() (ReadinessMatrix, error) {
	profiles, err := ReadinessCatalog()
	if err != nil {
		return ReadinessMatrix{}, err
	}
	summary := ReadinessSummary{Total: len(profiles)}
	for _, profile := range profiles {
		switch profile.Status {
		case ReadinessManifestOnly:
			summary.ManifestOnly++
		case ReadinessHealthOnly:
			summary.HealthOnly++
		case ReadinessReadOnly:
			summary.ReadOnly++
		case ReadinessPartiallySupported:
			summary.PartiallySupported++
		case ReadinessReady:
			summary.Ready++
		case ReadinessQualified:
			summary.Qualified++
		case ReadinessDegraded:
			summary.Degraded++
		case ReadinessReauthorizationNeeded:
			summary.ReauthorizationRequired++
		case ReadinessNotAvailable:
			summary.NotAvailable++
		}
	}
	return ReadinessMatrix{SchemaVersion: 1, GeneratedAt: time.Now().UTC(), Consistency: "repository_snapshot", Summary: summary, Profiles: profiles}, nil
}

// AllowsRemoteOperation is the common fail-closed admission check for a
// capability before a worker or application service starts a remote call.
// Write-like operations require qualified evidence; read operations may run
// on a ready connector but remain subject to the account health and policy
// gates owned by the caller.
func AllowsRemoteOperation(profile ReadinessProfile, capability string, write bool) bool {
	if profile.Status == ReadinessDegraded || profile.Status == ReadinessReauthorizationNeeded || profile.Status == ReadinessNotAvailable || profile.Status == ReadinessManifestOnly || profile.Status == ReadinessHealthOnly {
		return false
	}
	if write && profile.Status != ReadinessQualified {
		return false
	}
	for _, item := range profile.Capabilities {
		if item.Name == capability && item.Status != string(ReadinessHealthOnly) && item.Status != string(ReadinessManifestOnly) {
			if write {
				return item.Status == "qualified"
			}
			return item.Status == "qualified" || item.Status == "ready" || item.Status == "read_only"
		}
	}
	return false
}

func validateReadinessProfiles(profiles []ReadinessProfile) error {
	if len(profiles) != 61 {
		return errors.New("connectors: readiness matrix must contain 61 profiles")
	}
	previous := ""
	seen := make(map[string]struct{}, len(profiles))
	for _, profile := range profiles {
		if profile.ID == "" || profile.ID <= previous {
			return errors.New("connectors: readiness profiles are not sorted or contain an empty ID")
		}
		previous = profile.ID
		if _, exists := seen[profile.ID]; exists {
			return errors.New("connectors: duplicate readiness profile")
		}
		seen[profile.ID] = struct{}{}
		if _, ok := validReadinessStatuses[profile.Status]; !ok || profile.Owner == "" || profile.Priority == "" || profile.Decision == "" || profile.NextAction == "" || profile.ConformanceRef == "" || profile.RuntimeRef == "" {
			return errors.New("connectors: incomplete readiness profile")
		}
		if profile.HealthOnly && profile.Status != ReadinessHealthOnly {
			return errors.New("connectors: health-only connector has an executable readiness status")
		}
		if profile.Status == ReadinessQualified && (profile.LiveQualificationStatus != "passed" && profile.LiveQualificationStatus != "qualified") {
			return errors.New("connectors: qualified connector has no live evidence")
		}
		for _, capability := range profile.Capabilities {
			if strings.TrimSpace(capability.Name) == "" || strings.ContainsAny(capability.Name, "\r\n") || strings.ContainsAny(capability.RuntimeEvidence, "\r\n") {
				return errors.New("connectors: invalid readiness capability")
			}
		}
	}
	return nil
}

func cloneReadinessProfiles(profiles []ReadinessProfile) []ReadinessProfile {
	cloned := append([]ReadinessProfile(nil), profiles...)
	for i := range cloned {
		cloned[i].Capabilities = append([]ReadinessCapability(nil), cloned[i].Capabilities...)
		for j := range cloned[i].Capabilities {
			cloned[i].Capabilities[j].RequiredScopes = append([]string(nil), cloned[i].Capabilities[j].RequiredScopes...)
		}
		cloned[i].Blockers = append([]string(nil), cloned[i].Blockers...)
	}
	sort.Slice(cloned, func(i, j int) bool { return cloned[i].ID < cloned[j].ID })
	return cloned
}
