// Package integrationcenter contains the provider-neutral integration state
// contract and deterministic reducer. It deliberately knows nothing about
// PostgreSQL, Kafka, HTTP clients, secrets, or connector names.
package integrationcenter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	MaxIssues       = 32
	MaxActions      = 16
	MaxCapabilities = 128
	MaxReasonCode   = 96
	MaxSourceRef    = 192
)

type RuntimeStatus string

const (
	RuntimeReady           RuntimeStatus = "ready"
	RuntimeSeparateSurface RuntimeStatus = "separate_surface"
	RuntimeHealthOnly      RuntimeStatus = "health_only"
	RuntimeUnsupported     RuntimeStatus = "unsupported"
	RuntimeNotRegistered   RuntimeStatus = "not_registered"
	RuntimeDrifted         RuntimeStatus = "drifted"
)

type AccountStatus string

const (
	AccountNotCreated AccountStatus = "not_created"
	AccountDisabled   AccountStatus = "disabled"
	AccountActive     AccountStatus = "active"
	AccountSuspended  AccountStatus = "suspended"
	AccountError      AccountStatus = "error"
)

type CredentialStatus string

const (
	CredentialMissing                 CredentialStatus = "missing"
	CredentialPresent                 CredentialStatus = "present"
	CredentialExpired                 CredentialStatus = "expired"
	CredentialReauthorizationRequired CredentialStatus = "reauthorization_required"
	CredentialInvalid                 CredentialStatus = "invalid"
	CredentialUnknown                 CredentialStatus = "unknown"
)

type ConfigurationStatus string

const (
	ConfigurationMissing ConfigurationStatus = "missing"
	ConfigurationInvalid ConfigurationStatus = "invalid"
	ConfigurationValid   ConfigurationStatus = "valid"
	ConfigurationStale   ConfigurationStatus = "stale"
	ConfigurationUnknown ConfigurationStatus = "unknown"
)

type HealthStatus string

const (
	HealthUnknown     HealthStatus = "unknown"
	HealthHealthy     HealthStatus = "healthy"
	HealthDegraded    HealthStatus = "degraded"
	HealthUnavailable HealthStatus = "unavailable"
	HealthStale       HealthStatus = "stale"
)

type CapabilityStatus string

const (
	CapabilityNotDeclared           CapabilityStatus = "not_declared"
	CapabilityDeclared              CapabilityStatus = "declared"
	CapabilityGranted               CapabilityStatus = "granted"
	CapabilityEnabled               CapabilityStatus = "enabled"
	CapabilityBlocked               CapabilityStatus = "blocked"
	CapabilityQualificationRequired CapabilityStatus = "qualification_required"
	CapabilityStale                 CapabilityStatus = "stale"
)

type SyncStatus string

const (
	SyncNotConfigured SyncStatus = "not_configured"
	SyncIdle          SyncStatus = "idle"
	SyncRunning       SyncStatus = "running"
	SyncRetrying      SyncStatus = "retrying"
	SyncFailed        SyncStatus = "failed"
	SyncStale         SyncStatus = "stale"
	SyncPaused        SyncStatus = "paused"
)

type ReconciliationStatus string

const (
	ReconciliationNotConfigured ReconciliationStatus = "not_configured"
	ReconciliationHealthy       ReconciliationStatus = "healthy"
	ReconciliationDriftOpen     ReconciliationStatus = "drift_open"
	ReconciliationFailed        ReconciliationStatus = "failed"
	ReconciliationStale         ReconciliationStatus = "stale"
)

type WebhookStatus string

const (
	WebhookNotConfigured WebhookStatus = "not_configured"
	WebhookReceiving     WebhookStatus = "receiving"
	WebhookFailing       WebhookStatus = "failing"
	WebhookStale         WebhookStatus = "stale"
	WebhookUnsupported   WebhookStatus = "unsupported"
)

type RateLimitStatus string

const (
	RateLimitNotObserved  RateLimitStatus = "not_observed"
	RateLimitAvailable    RateLimitStatus = "available"
	RateLimitLimited      RateLimitStatus = "limited"
	RateLimitResetUnknown RateLimitStatus = "reset_unknown"
	RateLimitStale        RateLimitStatus = "stale"
)

type OverallStatus string

const (
	OverallHealthy                 OverallStatus = "healthy"
	OverallAttention               OverallStatus = "attention"
	OverallDegraded                OverallStatus = "degraded"
	OverallSyncing                 OverallStatus = "syncing"
	OverallBlocked                 OverallStatus = "blocked"
	OverallSetupRequired           OverallStatus = "setup_required"
	OverallReauthorizationRequired OverallStatus = "reauthorization_required"
	OverallStale                   OverallStatus = "stale"
	OverallDisabled                OverallStatus = "disabled"
	OverallUnsupported             OverallStatus = "unsupported"
	OverallUnknown                 OverallStatus = "unknown"
)

type Visibility string

const (
	VisibilityFull     Visibility = "full"
	VisibilityPartial  Visibility = "partial"
	VisibilityRedacted Visibility = "redacted"
)

// EvidenceRef identifies an authoritative observation without copying its
// payload. SourceRef is an opaque bounded reference, never a URL with secrets.
type EvidenceRef struct {
	ObservedAt        time.Time  `json:"observed_at"`
	CheckedAt         time.Time  `json:"checked_at"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	SourceKind        string     `json:"source_kind"`
	SourceRef         string     `json:"source_ref"`
	SourceVersion     string     `json:"source_version,omitempty"`
	ReasonCode        string     `json:"reason_code,omitempty"`
	CorrelationID     string     `json:"correlation_id,omitempty"`
	CausationID       string     `json:"causation_id,omitempty"`
	EvidenceDigest    string     `json:"evidence_digest,omitempty"`
	Visibility        Visibility `json:"visibility"`
	StaleAfterSeconds int64      `json:"stale_after_seconds"`
	AgeSeconds        int64      `json:"age_seconds"`
}

func (e EvidenceRef) Validate(now time.Time) error {
	if e.ObservedAt.IsZero() || e.CheckedAt.IsZero() || e.ObservedAt.Location() != time.UTC || e.CheckedAt.Location() != time.UTC || e.CheckedAt.Before(e.ObservedAt) {
		return errors.New("integration center: invalid evidence timestamps")
	}
	if e.ExpiresAt != nil && (e.ExpiresAt.IsZero() || e.ExpiresAt.Location() != time.UTC || e.ExpiresAt.Before(e.CheckedAt)) {
		return errors.New("integration center: invalid evidence expiry")
	}
	if strings.TrimSpace(e.SourceKind) == "" || !safeRef(e.SourceRef, MaxSourceRef) || len(e.ReasonCode) > MaxReasonCode || !safeCode(e.ReasonCode) || e.StaleAfterSeconds < 0 || e.AgeSeconds < 0 || e.Visibility == "" || !validVisibility(e.Visibility) {
		return errors.New("integration center: invalid evidence")
	}
	if e.ExpiresAt != nil && now.After(*e.ExpiresAt) {
		// Expiry is valid evidence; the reducer will make the dimension stale.
	}
	for _, value := range []string{e.SourceVersion, e.CorrelationID, e.CausationID, e.EvidenceDigest} {
		if len(value) > 192 || strings.ContainsAny(value, "\x00\r\n") {
			return errors.New("integration center: invalid evidence reference")
		}
	}
	return nil
}

type Dimension struct {
	Status   string      `json:"status"`
	Evidence EvidenceRef `json:"evidence"`
}

type Dimensions struct {
	Runtime        Dimension `json:"runtime"`
	Account        Dimension `json:"account"`
	Credential     Dimension `json:"credential"`
	Configuration  Dimension `json:"configuration"`
	Health         Dimension `json:"health"`
	Capability     Dimension `json:"capability"`
	Sync           Dimension `json:"sync"`
	Reconciliation Dimension `json:"reconciliation"`
	Webhook        Dimension `json:"webhook"`
	RateLimit      Dimension `json:"rate_limit"`
}

type Capability struct {
	Name             string           `json:"name"`
	Direction        string           `json:"direction"`
	Status           CapabilityStatus `json:"status"`
	ApprovalRequired bool             `json:"approval_required"`
	Risk             string           `json:"risk"`
	ReasonCode       string           `json:"reason_code,omitempty"`
}

type Issue struct {
	Code            string     `json:"code"`
	Dimension       string     `json:"dimension"`
	Severity        string     `json:"severity"`
	Title           string     `json:"title"`
	ReasonCode      string     `json:"reason_code"`
	OccurrenceCount int        `json:"occurrence_count"`
	FirstSeenAt     time.Time  `json:"first_seen_at"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
	Visibility      Visibility `json:"visibility"`
}

type Action struct {
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	Label               string `json:"label"`
	Permission          string `json:"permission"`
	Risk                string `json:"risk"`
	ApprovalRequired    bool   `json:"approval_required"`
	ExpectedVersion     int64  `json:"expected_version"`
	IdempotencyRequired bool   `json:"idempotency_required"`
	Href                string `json:"href,omitempty"`
}

// Input contains normalized source facts. Adapters must classify secrets and
// provider errors before constructing it.
type Input struct {
	AccountID        string
	ConnectorID      string
	Family           string
	DisplayName      string
	Surface          string
	Version          int64
	Dimensions       Dimensions
	Capabilities     []Capability
	Issues           []Issue
	SourceWatermarks []string
	Now              time.Time
}

type Snapshot struct {
	SnapshotID      string        `json:"snapshot_id"`
	SnapshotVersion int           `json:"snapshot_version"`
	SnapshotDigest  string        `json:"snapshot_digest"`
	GeneratedAt     time.Time     `json:"generated_at"`
	Partial         bool          `json:"partial"`
	Consistency     string        `json:"consistency"`
	AccountID       string        `json:"account_id"`
	ConnectorID     string        `json:"connector_id"`
	Family          string        `json:"family"`
	DisplayName     string        `json:"display_name"`
	Surface         string        `json:"surface"`
	Version         int64         `json:"version"`
	Dimensions      Dimensions    `json:"dimensions"`
	Capabilities    []Capability  `json:"capabilities"`
	Overall         OverallStatus `json:"overall"`
	DominantIssue   *Issue        `json:"dominant_issue,omitempty"`
	Issues          []Issue       `json:"issues"`
	Actions         []Action      `json:"available_actions"`
}

type Summary struct {
	Total         int `json:"total"`
	Healthy       int `json:"healthy"`
	Attention     int `json:"attention"`
	Blocked       int `json:"blocked"`
	Stale         int `json:"stale"`
	Syncing       int `json:"syncing"`
	Unsupported   int `json:"unsupported"`
	SetupRequired int `json:"setup_required"`
}

var validStatuses = map[string]map[string]struct{}{
	"runtime":        {string(RuntimeReady): {}, string(RuntimeSeparateSurface): {}, string(RuntimeHealthOnly): {}, string(RuntimeUnsupported): {}, string(RuntimeNotRegistered): {}, string(RuntimeDrifted): {}},
	"account":        {string(AccountNotCreated): {}, string(AccountDisabled): {}, string(AccountActive): {}, string(AccountSuspended): {}, string(AccountError): {}},
	"credential":     {string(CredentialMissing): {}, string(CredentialPresent): {}, string(CredentialExpired): {}, string(CredentialReauthorizationRequired): {}, string(CredentialInvalid): {}, string(CredentialUnknown): {}},
	"configuration":  {string(ConfigurationMissing): {}, string(ConfigurationInvalid): {}, string(ConfigurationValid): {}, string(ConfigurationStale): {}, string(ConfigurationUnknown): {}},
	"health":         {string(HealthUnknown): {}, string(HealthHealthy): {}, string(HealthDegraded): {}, string(HealthUnavailable): {}, string(HealthStale): {}},
	"capability":     {string(CapabilityNotDeclared): {}, string(CapabilityDeclared): {}, string(CapabilityGranted): {}, string(CapabilityEnabled): {}, string(CapabilityBlocked): {}, string(CapabilityQualificationRequired): {}, string(CapabilityStale): {}},
	"sync":           {string(SyncNotConfigured): {}, string(SyncIdle): {}, string(SyncRunning): {}, string(SyncRetrying): {}, string(SyncFailed): {}, string(SyncStale): {}, string(SyncPaused): {}},
	"reconciliation": {string(ReconciliationNotConfigured): {}, string(ReconciliationHealthy): {}, string(ReconciliationDriftOpen): {}, string(ReconciliationFailed): {}, string(ReconciliationStale): {}},
	"webhook":        {string(WebhookNotConfigured): {}, string(WebhookReceiving): {}, string(WebhookFailing): {}, string(WebhookStale): {}, string(WebhookUnsupported): {}},
	"rate_limit":     {string(RateLimitNotObserved): {}, string(RateLimitAvailable): {}, string(RateLimitLimited): {}, string(RateLimitResetUnknown): {}, string(RateLimitStale): {}},
}

func validVisibility(v Visibility) bool {
	return v == VisibilityFull || v == VisibilityPartial || v == VisibilityRedacted
}
func safeCode(v string) bool {
	if v == "" {
		return true
	}
	for i, r := range v {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '_' && r != '.' && r != '-' {
			return false
		}
		if i == 0 && (r < 'a' || r > 'z') {
			return false
		}
	}
	return len(v) <= MaxReasonCode
}
func safeRef(v string, max int) bool {
	return v != "" && len(v) <= max && !strings.ContainsAny(v, "\x00\r\n")
}

func (d Dimensions) Validate(now time.Time) error {
	values := []struct {
		name  string
		value Dimension
	}{
		{"runtime", d.Runtime}, {"account", d.Account}, {"credential", d.Credential}, {"configuration", d.Configuration}, {"health", d.Health}, {"capability", d.Capability}, {"sync", d.Sync}, {"reconciliation", d.Reconciliation}, {"webhook", d.Webhook}, {"rate_limit", d.RateLimit},
	}
	for _, item := range values {
		if _, ok := validStatuses[item.name][item.value.Status]; !ok {
			return fmt.Errorf("integration center: unknown %s status %q", item.name, item.value.Status)
		}
		if err := item.value.Evidence.Validate(now); err != nil {
			return fmt.Errorf("%s: %w", item.name, err)
		}
	}
	return nil
}

func (i Input) Validate() error {
	if !safeRef(i.AccountID, 128) || !safeRef(i.ConnectorID, 96) || !safeRef(i.Family, 64) || !safeRef(i.Surface, 64) || i.Version < 1 || i.Now.IsZero() || i.Now.Location() != time.UTC || len(i.Capabilities) > MaxCapabilities || len(i.Issues) > MaxIssues {
		return errors.New("integration center: invalid input")
	}
	if i.DisplayName != "" && (len(i.DisplayName) > 160 || strings.ContainsAny(i.DisplayName, "\x00\r\n")) {
		return errors.New("integration center: invalid display name")
	}
	if err := i.Dimensions.Validate(i.Now); err != nil {
		return err
	}
	for _, c := range i.Capabilities {
		if c.Name == "" || len(c.Name) > 128 || !safeRef(c.Name, 128) || c.Direction == "" || (c.Direction != "read" && c.Direction != "write") || c.Risk == "" || !safeCode(c.ReasonCode) {
			return errors.New("integration center: invalid capability")
		}
	}
	for _, issue := range i.Issues {
		if err := validateIssue(issue); err != nil {
			return err
		}
	}
	for _, w := range i.SourceWatermarks {
		if !safeRef(w, MaxSourceRef) {
			return errors.New("integration center: invalid watermark")
		}
	}
	return nil
}

func validateIssue(issue Issue) error {
	if !safeCode(issue.Code) || !safeCode(issue.Dimension) || (issue.Severity != "info" && issue.Severity != "warning" && issue.Severity != "critical") || issue.Title == "" || len(issue.Title) > 160 || !safeCode(issue.ReasonCode) || issue.OccurrenceCount < 1 || issue.OccurrenceCount > 1000000 || issue.FirstSeenAt.IsZero() || issue.LastSeenAt.IsZero() || issue.FirstSeenAt.Location() != time.UTC || issue.LastSeenAt.Location() != time.UTC || issue.LastSeenAt.Before(issue.FirstSeenAt) || !validVisibility(issue.Visibility) {
		return errors.New("integration center: invalid issue")
	}
	if strings.ContainsAny(issue.Title, "\x00\r\n") || containsSensitiveText(issue.Title) {
		return errors.New("integration center: unsafe issue title")
	}
	return nil
}

func containsSensitiveText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"token", "secret", "password", "authorization", "bearer", "api_key", "apikey", "private_key"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func staleDimension(d Dimension, now time.Time) bool {
	return d.Evidence.ExpiresAt != nil && now.After(*d.Evidence.ExpiresAt) || d.Evidence.StaleAfterSeconds > 0 && now.Sub(d.Evidence.CheckedAt) > time.Duration(d.Evidence.StaleAfterSeconds)*time.Second
}

func makeIssue(code, dimension, severity, title, reason string, now time.Time) Issue {
	return Issue{Code: code, Dimension: dimension, Severity: severity, Title: title, ReasonCode: reason, OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now, Visibility: VisibilityFull}
}

func Reduce(input Input) (Snapshot, error) {
	if err := input.Validate(); err != nil {
		return Snapshot{}, err
	}
	d := input.Dimensions
	issues := append([]Issue(nil), input.Issues...)
	add := func(issue Issue) { issues = append(issues, issue) }
	if staleDimension(d.Runtime, input.Now) {
		d.Runtime.Status = string(RuntimeDrifted)
		add(makeIssue("runtime_stale", "runtime", "critical", "Среда интеграции устарела", "runtime_evidence_stale", input.Now))
	}
	if staleDimension(d.Health, input.Now) && d.Health.Status == string(HealthHealthy) {
		d.Health.Status = string(HealthStale)
		add(makeIssue("health_stale", "health", "warning", "Проверка подключения устарела", "health_evidence_stale", input.Now))
	}
	if staleDimension(d.Sync, input.Now) && d.Sync.Status == string(SyncIdle) {
		d.Sync.Status = string(SyncStale)
		add(makeIssue("sync_stale", "sync", "warning", "Синхронизация устарела", "sync_evidence_stale", input.Now))
	}
	if staleDimension(d.Reconciliation, input.Now) && d.Reconciliation.Status == string(ReconciliationHealthy) {
		d.Reconciliation.Status = string(ReconciliationStale)
		add(makeIssue("reconciliation_stale", "reconciliation", "warning", "Данные сверки устарели", "reconciliation_evidence_stale", input.Now))
	}
	if d.Runtime.Status == string(RuntimeUnsupported) || d.Runtime.Status == string(RuntimeNotRegistered) || d.Runtime.Status == string(RuntimeDrifted) {
		add(makeIssue("runtime_not_admitted", "runtime", "critical", "Операции не подключены к production runtime", "runtime_not_admitted", input.Now))
	}
	if d.Capability.Status == string(CapabilityBlocked) || d.Capability.Status == string(CapabilityQualificationRequired) {
		add(makeIssue("capability_blocked", "capability", "critical", "Операция заблокирована политикой или квалификацией", "capability_not_ready", input.Now))
	}
	if d.Account.Status == string(AccountNotCreated) || d.Credential.Status == string(CredentialMissing) || d.Configuration.Status == string(ConfigurationMissing) {
		add(makeIssue("setup_required", "account", "warning", "Требуется настройка интеграции", "setup_required", input.Now))
	}
	if d.Credential.Status == string(CredentialExpired) || d.Credential.Status == string(CredentialInvalid) || d.Credential.Status == string(CredentialReauthorizationRequired) {
		add(makeIssue("reauthorization_required", "credential", "critical", "Требуется повторная авторизация", "reauthorization_required", input.Now))
	}
	if d.Health.Status == string(HealthDegraded) || d.Health.Status == string(HealthUnavailable) {
		add(makeIssue("health_degraded", "health", "critical", "Подключение недоступно или работает нестабильно", "health_unavailable", input.Now))
	}
	if d.Sync.Status == string(SyncFailed) || d.Sync.Status == string(SyncRetrying) {
		add(makeIssue("sync_attention", "sync", "warning", "Синхронизация требует внимания", "sync_retry_or_failed", input.Now))
	}
	if d.Reconciliation.Status == string(ReconciliationDriftOpen) || d.Reconciliation.Status == string(ReconciliationFailed) {
		add(makeIssue("reconciliation_attention", "reconciliation", "warning", "Найдены расхождения данных", "reconciliation_drift", input.Now))
	}
	if d.Webhook.Status == string(WebhookFailing) {
		add(makeIssue("webhook_attention", "webhook", "warning", "Webhook-доставка завершается ошибками", "webhook_failing", input.Now))
	}
	if d.RateLimit.Status == string(RateLimitLimited) || d.RateLimit.Status == string(RateLimitResetUnknown) {
		add(makeIssue("rate_limit", "rate_limit", "warning", "Достигнут лимит запросов", "rate_limited", input.Now))
	}
	if d.Health.Evidence.Visibility == VisibilityRedacted || d.Sync.Evidence.Visibility == VisibilityRedacted || d.Reconciliation.Evidence.Visibility == VisibilityRedacted {
		add(makeIssue("redacted_evidence", "visibility", "warning", "Часть состояния скрыта из-за прав доступа", "permission_required", input.Now))
	}
	for _, dim := range []struct {
		name  string
		value *Dimension
	}{{"runtime", &d.Runtime}, {"account", &d.Account}, {"credential", &d.Credential}, {"configuration", &d.Configuration}, {"health", &d.Health}, {"capability", &d.Capability}, {"sync", &d.Sync}, {"reconciliation", &d.Reconciliation}, {"webhook", &d.Webhook}, {"rate_limit", &d.RateLimit}} {
		if dim.value.Evidence.Visibility == VisibilityRedacted && dim.value.Status != "unknown" {
			dim.value.Status = "unknown"
		}
	}
	sort.SliceStable(issues, func(a, b int) bool {
		if issues[a].Code != issues[b].Code {
			return issues[a].Code < issues[b].Code
		}
		return issues[a].Dimension < issues[b].Dimension
	})
	if len(issues) > MaxIssues {
		issues = issues[:MaxIssues]
	}
	overall := deriveOverall(d, issues)
	actions := deriveActions(input, overall, issues)
	if len(actions) > MaxActions {
		actions = actions[:MaxActions]
	}
	snapshot := Snapshot{SnapshotVersion: 1, GeneratedAt: input.Now, Partial: false, Consistency: "best_effort", AccountID: input.AccountID, ConnectorID: input.ConnectorID, Family: input.Family, DisplayName: input.DisplayName, Surface: input.Surface, Version: input.Version, Dimensions: d, Capabilities: append([]Capability(nil), input.Capabilities...), Overall: overall, Issues: issues, Actions: actions}
	if len(input.SourceWatermarks) == 0 {
		snapshot.Partial = true
	}
	sort.Strings(input.SourceWatermarks)
	digestInput := struct {
		AccountID, ConnectorID, Family, Surface string
		Version                                 int64
		Dimensions                              Dimensions
		Capabilities                            []Capability
		Issues                                  []Issue
		Watermarks                              []string
	}{input.AccountID, input.ConnectorID, input.Family, input.Surface, input.Version, d, snapshot.Capabilities, issues, input.SourceWatermarks}
	encoded, _ := json.Marshal(digestInput)
	sum := sha256.Sum256(encoded)
	snapshot.SnapshotDigest = hex.EncodeToString(sum[:])
	snapshot.SnapshotID = "ic:" + snapshot.SnapshotDigest[:26]
	if len(issues) > 0 {
		dominant := issues[0]
		snapshot.DominantIssue = &dominant
	}
	return snapshot, nil
}

func deriveOverall(d Dimensions, issues []Issue) OverallStatus {
	if d.Runtime.Status == string(RuntimeUnsupported) || d.Runtime.Status == string(RuntimeNotRegistered) || d.Runtime.Status == string(RuntimeDrifted) {
		return OverallUnsupported
	}
	if d.Capability.Status == string(CapabilityBlocked) || d.Capability.Status == string(CapabilityQualificationRequired) {
		return OverallBlocked
	}
	if d.Account.Status == string(AccountNotCreated) || d.Credential.Status == string(CredentialMissing) || d.Configuration.Status == string(ConfigurationMissing) {
		return OverallSetupRequired
	}
	if d.Credential.Status == string(CredentialExpired) || d.Credential.Status == string(CredentialInvalid) || d.Credential.Status == string(CredentialReauthorizationRequired) {
		return OverallReauthorizationRequired
	}
	if d.Account.Status == string(AccountDisabled) {
		return OverallDisabled
	}
	if d.Health.Status == string(HealthDegraded) || d.Health.Status == string(HealthUnavailable) {
		return OverallDegraded
	}
	if d.Health.Status == string(HealthStale) || d.Sync.Status == string(SyncStale) || d.Reconciliation.Status == string(ReconciliationStale) {
		return OverallStale
	}
	for _, issue := range issues {
		if issue.Severity == "critical" || issue.Code == "rate_limit" || issue.Code == "sync_attention" || issue.Code == "reconciliation_attention" || issue.Code == "webhook_attention" {
			return OverallAttention
		}
	}
	if d.Sync.Status == string(SyncRunning) {
		return OverallSyncing
	}
	if d.Runtime.Status == string(RuntimeReady) && d.Account.Status == string(AccountActive) && d.Credential.Status == string(CredentialPresent) && d.Configuration.Status == string(ConfigurationValid) && d.Health.Status == string(HealthHealthy) {
		return OverallHealthy
	}
	return OverallUnknown
}

func deriveActions(input Input, overall OverallStatus, issues []Issue) []Action {
	actions := make([]Action, 0, 4)
	add := func(id, kind, label, permission, risk, href string, approval bool) {
		actions = append(actions, Action{ID: id, Kind: kind, Label: label, Permission: permission, Risk: risk, ApprovalRequired: approval, ExpectedVersion: input.Version, IdempotencyRequired: kind != "view_history", Href: href})
	}
	for _, issue := range issues {
		switch issue.Code {
		case "setup_required":
			add("configure", "configure", "Настроить", "connectors.accounts.write", "write", "/integrations", false)
		case "reauthorization_required":
			add("reauthorize", "reauthorize", "Повторно авторизовать", "connectors.accounts.write", "write_sensitive", "/integrations", false)
		case "health_degraded", "health_stale":
			add("check", "check", "Проверить подключение", "connectors.accounts.read", "read", "/integrations", false)
		case "sync_attention", "sync_stale":
			add("open_sync", "open_sync", "Открыть синхронизацию", "sync.read", "read", "/sync", false)
		case "reconciliation_attention":
			add("open_drift", "open_drift", "Открыть сверку", "sync.read", "read", "/sync", false)
		case "rate_limit":
			add("view_history", "view_history", "Посмотреть историю", "connectors.accounts.read", "read", "/integrations", false)
		}
	}
	if len(actions) == 0 && overall == OverallHealthy {
		add("view_history", "view_history", "История состояния", "connectors.accounts.read", "read", "/integrations", false)
	}
	return actions
}

// BuildSummary computes counters from the same snapshot set used by the API.
func BuildSummary(rows []Snapshot) Summary {
	var s Summary
	s.Total = len(rows)
	for _, row := range rows {
		switch row.Overall {
		case OverallHealthy:
			s.Healthy++
		case OverallAttention:
			s.Attention++
		case OverallBlocked:
			s.Blocked++
		case OverallStale:
			s.Stale++
		case OverallSyncing:
			s.Syncing++
		case OverallUnsupported:
			s.Unsupported++
		case OverallSetupRequired, OverallReauthorizationRequired, OverallDisabled, OverallDegraded, OverallUnknown:
			s.SetupRequired++
		}
	}
	return s
}
