// Package publicationquality implements the provider-neutral publication
// preflight contract. It evaluates an immutable local snapshot against a
// versioned connector profile and never mutates catalog, compliance or remote
// publication state.
package publicationquality

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalid         = errors.New("publication quality: invalid value")
	ErrNotFound        = errors.New("publication quality: not found")
	ErrConflict        = errors.New("publication quality: optimistic conflict")
	ErrGateDenied      = errors.New("publication quality: publication gate denied")
	ErrReceiptStale    = errors.New("publication quality: receipt is stale")
	ErrProfileUnsafe   = errors.New("publication quality: profile is unsafe")
	ErrUnsupported     = errors.New("publication quality: target is unsupported")
	ErrQualityUnknown  = errors.New("publication quality: evaluation is unknown")
	ErrApprovalPending = errors.New("publication quality: approval is required")
)

var (
	qualityRefPattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
	qualityCodePattern     = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	qualityLocalePattern   = regexp.MustCompile(`^[a-z]{2}(?:-[A-Z]{2})?$`)
	qualityCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
	qualityCountryPattern  = regexp.MustCompile(`^[A-Z]{2}$`)
)

// Decision is the terminal result of a quality run.
type Decision string

const (
	DecisionReady             Decision = "ready"
	DecisionReadyWithWarnings Decision = "ready_with_warnings"
	DecisionBlocked           Decision = "blocked"
	DecisionApprovalRequired  Decision = "approval_required"
	DecisionStale             Decision = "stale"
	DecisionUnsupported       Decision = "unsupported"
	DecisionNotConfigured     Decision = "not_configured"
	DecisionUnknown           Decision = "unknown"
)

func (d Decision) Valid() bool {
	switch d {
	case DecisionReady, DecisionReadyWithWarnings, DecisionBlocked, DecisionApprovalRequired, DecisionStale, DecisionUnsupported, DecisionNotConfigured, DecisionUnknown:
		return true
	default:
		return false
	}
}

func (d Decision) AllowsPublication() bool {
	return d == DecisionReady || d == DecisionReadyWithWarnings
}

// Severity controls whether an issue blocks publication or only informs the
// operator. A block always dominates the aggregate score.
type Severity string

const (
	SeverityBlock    Severity = "block"
	SeverityApproval Severity = "approval_required"
	SeverityWarn     Severity = "warn"
	SeverityInfo     Severity = "info"
)

func (s Severity) Valid() bool {
	return s == SeverityBlock || s == SeverityApproval || s == SeverityWarn || s == SeverityInfo
}

func severityRank(s Severity) int {
	switch s {
	case SeverityBlock:
		return 4
	case SeverityApproval:
		return 3
	case SeverityWarn:
		return 2
	default:
		return 1
	}
}

// Category is the stable score bucket and issue ownership hint.
type Category string

const (
	CategoryIdentity     Category = "identity_content"
	CategoryAttributes   Category = "category_attributes"
	CategoryMedia        Category = "media"
	CategoryPriceStock   Category = "price_stock"
	CategoryCompliance   Category = "compliance"
	CategoryCapability   Category = "mapping_capability"
	CategoryLocalization Category = "localization"
	CategoryContract     Category = "channel_contract"
)

var categoryOrder = []Category{CategoryIdentity, CategoryAttributes, CategoryMedia, CategoryPriceStock, CategoryCompliance, CategoryCapability, CategoryLocalization, CategoryContract}

func (c Category) Valid() bool {
	for _, known := range categoryOrder {
		if c == known {
			return true
		}
	}
	return false
}

// Target identifies a concrete connector account and local publication scope.
type Target struct {
	OrganizationID     string `json:"organization_id"`
	WorkspaceID        string `json:"workspace_id"`
	ProductID          string `json:"product_id"`
	OfferID            string `json:"offer_id,omitempty"`
	ConnectorAccountID string `json:"connector_account_id"`
	ConnectorID        string `json:"connector_id"`
	ChannelFamily      string `json:"channel_family"`
	Locale             string `json:"locale"`
	Jurisdiction       string `json:"jurisdiction"`
}

func (t Target) Validate() error {
	refs := []string{t.OrganizationID, t.WorkspaceID, t.ProductID, t.OfferID, t.ConnectorAccountID, t.ConnectorID, t.ChannelFamily}
	for index, value := range refs {
		if index == 3 && value == "" {
			continue
		}
		if !qualityRefPattern.MatchString(value) {
			return ErrInvalid
		}
	}
	if !qualityLocalePattern.MatchString(t.Locale) || !qualityCountryPattern.MatchString(t.Jurisdiction) {
		return ErrInvalid
	}
	return nil
}

func (t Target) key() string {
	return strings.Join([]string{t.OrganizationID, t.WorkspaceID, t.ProductID, t.OfferID, t.ConnectorAccountID, t.ConnectorID, t.ChannelFamily, t.Locale, t.Jurisdiction}, "\x00")
}

// ComplianceEvidence is the normalized result of Task-082. The quality engine
// stores only its outcome and fingerprint, never provider responses.
type ComplianceEvidence struct {
	Outcome     string    `json:"outcome"`
	Fingerprint string    `json:"fingerprint"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

func (e ComplianceEvidence) Validate() error {
	if e.Outcome != "allow" && e.Outcome != "warn" && e.Outcome != "approval_required" && e.Outcome != "block" {
		return ErrInvalid
	}
	if len(e.Fingerprint) != 64 || !isLowerHex(e.Fingerprint) || !isUTC(e.EvaluatedAt) {
		return ErrInvalid
	}
	return nil
}

// MediaAsset is release metadata from the upload-security pipeline. Binary
// content and URLs never enter a quality snapshot.
type MediaAsset struct {
	ID       string `json:"id"`
	Format   string `json:"format"`
	Bytes    int64  `json:"bytes"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Released bool   `json:"released"`
	Safe     bool   `json:"safe"`
}

func (m MediaAsset) Validate() error {
	if !qualityRefPattern.MatchString(m.ID) || !qualityCodePattern.MatchString(strings.ToLower(m.Format)) || m.Bytes < 0 || m.Width < 0 || m.Height < 0 {
		return ErrInvalid
	}
	return nil
}

// Snapshot is a bounded immutable read model assembled from canonical domains.
// Version and digest fields make a receipt invalid as soon as any source
// changes.
type Snapshot struct {
	Target               Target
	ProductVersion       int64
	OfferVersion         int64
	PriceVersion         int64
	InventoryVersion     int64
	MediaVersion         int64
	MappingVersion       int64
	CapabilityVersion    int64
	SKU                  string
	GTIN                 string
	Title                string
	Description          string
	CategoryCode         string
	ProductStatus        string
	Attributes           map[string]string
	Media                []MediaAsset
	Price                *domain.Money
	Available            *domain.Quantity
	Compliance           ComplianceEvidence
	CompliancePresent    bool
	MappingConfigured    bool
	CapabilityAdmitted   bool
	ProductsWriteEnabled bool
	SourceFreshAt        time.Time
	AssembledAt          time.Time

	// Optional source digests make lineages explicit without copying payloads.
	CatalogDigest string
	PIMDigest     string
	MediaDigest   string
}

func (s Snapshot) Validate() error {
	if err := s.Target.Validate(); err != nil || s.ProductVersion < 1 || s.OfferVersion < 0 || s.PriceVersion < 0 || s.InventoryVersion < 0 || s.MediaVersion < 0 || s.MappingVersion < 0 || s.CapabilityVersion < 0 || !isUTC(s.SourceFreshAt) || !isUTC(s.AssembledAt) {
		return ErrInvalid
	}
	if len(s.SKU) > 200 || len(s.GTIN) > 32 || len(s.Title) > 500 || len(s.Description) > 10_000 || len(s.CategoryCode) > 192 || len(s.ProductStatus) > 32 || strings.TrimSpace(s.Title) != s.Title || strings.TrimSpace(s.SKU) != s.SKU {
		return ErrInvalid
	}
	if s.ProductStatus != "" && s.ProductStatus != "draft" && s.ProductStatus != "active" && s.ProductStatus != "archived" {
		return ErrInvalid
	}
	if s.OfferVersion == 0 && s.Target.OfferID != "" {
		return ErrInvalid
	}
	if len(s.Attributes) > 256 || len(s.Media) > 64 {
		return ErrInvalid
	}
	for k, v := range s.Attributes {
		if !qualityCodePattern.MatchString(k) || len(v) > 2000 || strings.ContainsAny(v, "\x00\r\n") {
			return ErrInvalid
		}
	}
	for _, media := range s.Media {
		if media.Validate() != nil {
			return ErrInvalid
		}
	}
	if s.Price != nil && s.Price.Validate() != nil {
		return ErrInvalid
	}
	if s.Available != nil && s.Available.Validate() != nil {
		return ErrInvalid
	}
	if s.CompliancePresent && s.Compliance.Validate() != nil {
		return ErrInvalid
	}
	for _, digest := range []string{s.CatalogDigest, s.PIMDigest, s.MediaDigest} {
		if digest != "" && (len(digest) != 64 || !isLowerHex(digest)) {
			return ErrInvalid
		}
	}
	if s.Available != nil && s.Available.Value.Coefficient() < 0 {
		return ErrInvalid
	}
	return nil
}

// RuleKind is intentionally a small typed vocabulary; profiles cannot execute
// arbitrary expressions, scripts, SQL or network calls.
type RuleKind string

const (
	RuleRequired  RuleKind = "required"
	RuleMaxLength RuleKind = "max_length"
	RuleEnum      RuleKind = "enum"
	RuleMinValue  RuleKind = "min_value"
	RuleMaxValue  RuleKind = "max_value"
)

func (k RuleKind) Valid() bool {
	return k == RuleRequired || k == RuleMaxLength || k == RuleEnum || k == RuleMinValue || k == RuleMaxValue
}

// Rule is a declarative publication check.
type Rule struct {
	ID          string          `json:"id"`
	Category    Category        `json:"category"`
	Field       string          `json:"field"`
	Kind        RuleKind        `json:"kind"`
	Severity    Severity        `json:"severity"`
	MaxLength   int             `json:"max_length,omitempty"`
	Allowed     []string        `json:"allowed,omitempty"`
	Min         *domain.Decimal `json:"min,omitempty"`
	Max         *domain.Decimal `json:"max,omitempty"`
	Message     string          `json:"message"`
	Remediation string          `json:"remediation"`
}

func (r Rule) Validate() error {
	if !qualityCodePattern.MatchString(r.ID) || !r.Category.Valid() || !qualityCodePattern.MatchString(r.Field) || !r.Kind.Valid() || !r.Severity.Valid() || len(r.Message) < 1 || len(r.Message) > 240 || len(r.Remediation) < 1 || len(r.Remediation) > 240 {
		return ErrProfileUnsafe
	}
	if r.Kind == RuleMaxLength && (r.MaxLength < 1 || r.MaxLength > 20_000) {
		return ErrProfileUnsafe
	}
	if r.Kind == RuleEnum && (len(r.Allowed) == 0 || len(r.Allowed) > 256) {
		return ErrProfileUnsafe
	}
	if r.Kind == RuleMinValue && r.Min == nil {
		return ErrProfileUnsafe
	}
	if r.Kind == RuleMaxValue && r.Max == nil {
		return ErrProfileUnsafe
	}
	for _, value := range r.Allowed {
		if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return ErrProfileUnsafe
		}
	}
	if r.Min != nil && r.Min.Validate() != nil || r.Max != nil && r.Max.Validate() != nil {
		return ErrProfileUnsafe
	}
	return nil
}

// Profile is a versioned connector publication contract. It is local metadata
// and does not grant a remote capability by itself.
type Profile struct {
	ID                  string             `json:"id"`
	Version             int64              `json:"version"`
	ConnectorID         string             `json:"connector_id"`
	ChannelFamily       string             `json:"channel_family"`
	Locale              string             `json:"locale"`
	Jurisdiction        string             `json:"jurisdiction"`
	FreshnessTTL        time.Duration      `json:"freshness_ttl"`
	Rules               []Rule             `json:"rules"`
	Weights             map[Category]int64 `json:"weights"`
	RequiredMedia       int                `json:"required_media"`
	AllowedMediaFormats []string           `json:"allowed_media_formats"`
	MaxMediaBytes       int64              `json:"max_media_bytes"`
	RequirePrice        bool               `json:"require_price"`
	RequireStock        bool               `json:"require_stock"`
	Currency            string             `json:"currency"`
	Active              bool               `json:"active"`
}

func (p Profile) Validate() error {
	refs := []string{p.ID, p.ConnectorID, p.ChannelFamily}
	for _, value := range refs {
		if !qualityRefPattern.MatchString(value) {
			return ErrProfileUnsafe
		}
	}
	if p.Version < 1 || !qualityLocalePattern.MatchString(p.Locale) || !qualityCountryPattern.MatchString(p.Jurisdiction) || p.FreshnessTTL < time.Minute || p.FreshnessTTL > 30*24*time.Hour || p.RequiredMedia < 0 || p.RequiredMedia > 64 || p.MaxMediaBytes < 0 || p.MaxMediaBytes > 1<<30 || !qualityCurrencyPattern.MatchString(p.Currency) || len(p.Rules) > 512 || len(p.Weights) > len(categoryOrder) {
		return ErrProfileUnsafe
	}
	seen := map[string]struct{}{}
	for _, rule := range p.Rules {
		if err := rule.Validate(); err != nil {
			return err
		}
		if _, ok := seen[rule.ID]; ok {
			return ErrProfileUnsafe
		}
		seen[rule.ID] = struct{}{}
	}
	for category, weight := range p.Weights {
		if !category.Valid() || weight < 0 || weight > 10_000 {
			return ErrProfileUnsafe
		}
	}
	for _, format := range p.AllowedMediaFormats {
		if !qualityCodePattern.MatchString(format) {
			return ErrProfileUnsafe
		}
	}
	return nil
}

// Digest returns the stable profile/ruleset fingerprint.
func (p Profile) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d|%s|%s|%s|%s|%d|%d|%t|%t|%s|", p.ID, p.Version, p.ConnectorID, p.ChannelFamily, p.Locale, p.Jurisdiction, p.RequiredMedia, p.MaxMediaBytes, p.RequirePrice, p.RequireStock, p.Currency)
	formats := append([]string(nil), p.AllowedMediaFormats...)
	sort.Strings(formats)
	b.WriteString(strings.Join(formats, ","))
	keys := make([]string, 0, len(p.Weights))
	for category := range p.Weights {
		keys = append(keys, string(category))
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "|w:%s=%d", key, p.Weights[Category(key)])
	}
	rules := append([]Rule(nil), p.Rules...)
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	for _, rule := range rules {
		allowed := append([]string(nil), rule.Allowed...)
		sort.Strings(allowed)
		min, max := "", ""
		if rule.Min != nil {
			min = rule.Min.String()
		}
		if rule.Max != nil {
			max = rule.Max.String()
		}
		fmt.Fprintf(&b, "|r:%s:%s:%s:%s:%s:%d:%s:%s:%s:%s", rule.ID, rule.Category, rule.Field, rule.Kind, rule.Severity, rule.MaxLength, strings.Join(allowed, ","), min, max, rule.Message+"|"+rule.Remediation)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), nil
}

// Issue is bounded, safe evidence shown to operators.
type Issue struct {
	Code        string   `json:"code"`
	Category    Category `json:"category"`
	Severity    Severity `json:"severity"`
	FieldPath   string   `json:"field_path"`
	Message     string   `json:"message"`
	Expected    string   `json:"expected,omitempty"`
	Observed    string   `json:"observed,omitempty"`
	Remediation string   `json:"remediation"`
	SourceRef   string   `json:"source_ref,omitempty"`
}

// RemediationAction is an auditable, non-destructive proposal derived from an
// issue. Applying it requires a separate policy/approval boundary; the
// quality engine never edits canonical catalog or PIM state.
type RemediationAction struct {
	ID                     string    `json:"id"`
	RunID                  string    `json:"run_id"`
	IssueCode              string    `json:"issue_code"`
	ActionCode             string    `json:"action_code"`
	Status                 string    `json:"status"`
	ExpectedSnapshotDigest string    `json:"expected_snapshot_digest"`
	ProposedDiffDigest     string    `json:"proposed_diff_digest"`
	ApprovalID             string    `json:"approval_id,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
}

func (a RemediationAction) Validate() error {
	if !qualityRefPattern.MatchString(a.ID) || !qualityRefPattern.MatchString(a.RunID) || !qualityCodePattern.MatchString(a.IssueCode) || !qualityCodePattern.MatchString(a.ActionCode) || (a.Status != "proposed" && a.Status != "approved" && a.Status != "applied" && a.Status != "rejected" && a.Status != "expired") || len(a.ExpectedSnapshotDigest) != 64 || !isLowerHex(a.ExpectedSnapshotDigest) || len(a.ProposedDiffDigest) != 64 || !isLowerHex(a.ProposedDiffDigest) || (a.ApprovalID != "" && !qualityRefPattern.MatchString(a.ApprovalID)) || !isUTC(a.CreatedAt) {
		return ErrInvalid
	}
	return nil
}

func (i Issue) Validate() error {
	if !qualityCodePattern.MatchString(i.Code) || !i.Category.Valid() || !i.Severity.Valid() || len(i.FieldPath) > 256 || len(i.Message) < 1 || len(i.Message) > 240 || len(i.Expected) > 256 || len(i.Observed) > 256 || len(i.Remediation) < 1 || len(i.Remediation) > 240 || (i.SourceRef != "" && !qualityRefPattern.MatchString(i.SourceRef)) {
		return ErrInvalid
	}
	return nil
}

// QualityRun is an immutable evaluation result after its terminal transition.
type QualityRun struct {
	ID                    string             `json:"id"`
	Target                Target             `json:"target"`
	ProductVersion        int64              `json:"product_version"`
	OfferVersion          int64              `json:"offer_version"`
	PriceVersion          int64              `json:"price_version"`
	InventoryVersion      int64              `json:"inventory_version"`
	MediaVersion          int64              `json:"media_version"`
	MappingVersion        int64              `json:"mapping_version"`
	CapabilityVersion     int64              `json:"capability_version"`
	SnapshotDigest        string             `json:"snapshot_digest"`
	ProfileDigest         string             `json:"profile_digest"`
	ComplianceFingerprint string             `json:"compliance_fingerprint"`
	EvaluatedAt           time.Time          `json:"evaluated_at"`
	ValidUntil            time.Time          `json:"valid_until"`
	Status                string             `json:"status"`
	Decision              Decision           `json:"decision"`
	ScoreBPS              int64              `json:"score_bps"`
	CategoryScoresBPS     map[Category]int64 `json:"category_scores_bps"`
	Issues                []Issue            `json:"issues"`
	Version               int64              `json:"version"`
}

func (r QualityRun) Validate() error {
	if !qualityRefPattern.MatchString(r.ID) || r.Target.Validate() != nil || r.ProductVersion < 1 || r.OfferVersion < 0 || r.PriceVersion < 0 || r.InventoryVersion < 0 || r.MediaVersion < 0 || r.MappingVersion < 0 || r.CapabilityVersion < 0 || !isLowerHex(r.SnapshotDigest) || len(r.SnapshotDigest) != 64 || !isLowerHex(r.ProfileDigest) || len(r.ProfileDigest) != 64 || len(r.ComplianceFingerprint) != 64 || !isLowerHex(r.ComplianceFingerprint) || !isOptionalUTC(r.EvaluatedAt) || !isOptionalUTC(r.ValidUntil) || (r.ValidUntil.IsZero() != r.EvaluatedAt.IsZero()) || (!r.EvaluatedAt.IsZero() && r.ValidUntil.Before(r.EvaluatedAt)) || (r.Status != "queued" && r.Status != "running" && r.Status != "completed" && r.Status != "failed" && r.Status != "cancelled") || !r.Decision.Valid() || r.ScoreBPS < 0 || r.ScoreBPS > 10_000 || r.Version < 1 || len(r.Issues) > 1024 {
		return ErrInvalid
	}
	if (r.Status == "completed" || r.Status == "failed" || r.Status == "cancelled") && (r.EvaluatedAt.IsZero() || r.ValidUntil.IsZero()) {
		return ErrInvalid
	}
	for category, score := range r.CategoryScoresBPS {
		if !category.Valid() || score < 0 || score > 10_000 {
			return ErrInvalid
		}
	}
	for _, issue := range r.Issues {
		if issue.Validate() != nil {
			return ErrInvalid
		}
	}
	return nil
}

// PublicationGateReceipt binds a successful decision to every input version.
type PublicationGateReceipt struct {
	ID                    string    `json:"id"`
	Target                Target    `json:"target"`
	ProductVersion        int64     `json:"product_version"`
	OfferVersion          int64     `json:"offer_version"`
	PriceVersion          int64     `json:"price_version"`
	InventoryVersion      int64     `json:"inventory_version"`
	MediaVersion          int64     `json:"media_version"`
	MappingVersion        int64     `json:"mapping_version"`
	CapabilityVersion     int64     `json:"capability_version"`
	SnapshotDigest        string    `json:"snapshot_digest"`
	ProfileDigest         string    `json:"profile_digest"`
	ComplianceFingerprint string    `json:"compliance_fingerprint"`
	Decision              Decision  `json:"decision"`
	IssuedAt              time.Time `json:"issued_at"`
	ValidUntil            time.Time `json:"valid_until"`
	RunID                 string    `json:"run_id"`
	Version               int64     `json:"version"`
}

func (r PublicationGateReceipt) Validate() error {
	if !qualityRefPattern.MatchString(r.ID) || r.Target.Validate() != nil || r.ProductVersion < 1 || r.OfferVersion < 0 || r.PriceVersion < 0 || r.InventoryVersion < 0 || r.MediaVersion < 0 || r.MappingVersion < 0 || r.CapabilityVersion < 0 || len(r.SnapshotDigest) != 64 || !isLowerHex(r.SnapshotDigest) || len(r.ProfileDigest) != 64 || !isLowerHex(r.ProfileDigest) || len(r.ComplianceFingerprint) != 64 || !isLowerHex(r.ComplianceFingerprint) || !r.Decision.Valid() || !r.Decision.AllowsPublication() || !isUTC(r.IssuedAt) || !isUTC(r.ValidUntil) || r.ValidUntil.Before(r.IssuedAt) || !qualityRefPattern.MatchString(r.RunID) || r.Version < 1 {
		return ErrInvalid
	}
	return nil
}

func (r PublicationGateReceipt) Matches(s Snapshot, profileDigest string, at time.Time) bool {
	return r.Validate() == nil && s.Validate() == nil && r.Target.key() == s.Target.key() && r.ProductVersion == s.ProductVersion && r.OfferVersion == s.OfferVersion && r.PriceVersion == s.PriceVersion && r.InventoryVersion == s.InventoryVersion && r.MediaVersion == s.MediaVersion && r.MappingVersion == s.MappingVersion && r.CapabilityVersion == s.CapabilityVersion && r.SnapshotDigest == SnapshotDigest(s) && r.ProfileDigest == profileDigest && r.ComplianceFingerprint == complianceFingerprint(s) && !at.Before(r.IssuedAt) && at.Before(r.ValidUntil)
}

// Evaluate performs a deterministic local quality run. It can be called by a
// worker or API preview; it has no side effects.
func Evaluate(profile Profile, snapshot Snapshot, now time.Time) (QualityRun, PublicationGateReceipt, error) {
	if profile.Validate() != nil || snapshot.Validate() != nil || !isUTC(now) {
		return QualityRun{}, PublicationGateReceipt{}, ErrInvalid
	}
	profileDigest, err := profile.Digest()
	if err != nil {
		return QualityRun{}, PublicationGateReceipt{}, err
	}
	snapshotDigest := SnapshotDigest(snapshot)
	runID := "quality:" + snapshotDigest[:32]
	run := QualityRun{ID: runID, Target: snapshot.Target, ProductVersion: snapshot.ProductVersion, OfferVersion: snapshot.OfferVersion, PriceVersion: snapshot.PriceVersion, InventoryVersion: snapshot.InventoryVersion, MediaVersion: snapshot.MediaVersion, MappingVersion: snapshot.MappingVersion, CapabilityVersion: snapshot.CapabilityVersion, SnapshotDigest: snapshotDigest, ProfileDigest: profileDigest, ComplianceFingerprint: complianceFingerprint(snapshot), EvaluatedAt: now, ValidUntil: now.Add(profile.FreshnessTTL), Status: "completed", Decision: DecisionReady, CategoryScoresBPS: make(map[Category]int64), Version: 1}
	issues := evaluateIssues(profile, snapshot, now)
	issues = deduplicateIssues(issues)
	sort.Slice(issues, func(i, j int) bool {
		if severityRank(issues[i].Severity) != severityRank(issues[j].Severity) {
			return severityRank(issues[i].Severity) > severityRank(issues[j].Severity)
		}
		if issues[i].Category != issues[j].Category {
			return issues[i].Category < issues[j].Category
		}
		return issues[i].Code < issues[j].Code
	})
	run.Issues = issues
	run.ScoreBPS, run.CategoryScoresBPS = scoreIssues(profile, issues)
	run.Decision = decisionFor(issues)
	if run.Decision == DecisionReady && len(issues) > 0 {
		run.Decision = DecisionReadyWithWarnings
	}
	if run.Decision == DecisionReady || run.Decision == DecisionReadyWithWarnings {
		receipt := PublicationGateReceipt{ID: "receipt:" + snapshotDigest[:32], Target: snapshot.Target, ProductVersion: snapshot.ProductVersion, OfferVersion: snapshot.OfferVersion, PriceVersion: snapshot.PriceVersion, InventoryVersion: snapshot.InventoryVersion, MediaVersion: snapshot.MediaVersion, MappingVersion: snapshot.MappingVersion, CapabilityVersion: snapshot.CapabilityVersion, SnapshotDigest: snapshotDigest, ProfileDigest: profileDigest, ComplianceFingerprint: complianceFingerprint(snapshot), Decision: run.Decision, IssuedAt: now, ValidUntil: run.ValidUntil, RunID: run.ID, Version: 1}
		return run, receipt, nil
	}
	return run, PublicationGateReceipt{}, nil
}

// CheckReceipt is the host-side gate invoked immediately before remote egress.
func CheckReceipt(receipt PublicationGateReceipt, snapshot Snapshot, profile Profile, now time.Time) error {
	if profile.Validate() != nil || snapshot.Validate() != nil || !isUTC(now) {
		return ErrInvalid
	}
	digest, err := profile.Digest()
	if err != nil {
		return err
	}
	if receipt.Decision == DecisionApprovalRequired {
		return ErrApprovalPending
	}
	if !receipt.Decision.AllowsPublication() {
		if receipt.Decision == DecisionUnsupported || receipt.Decision == DecisionNotConfigured {
			return ErrUnsupported
		}
		if receipt.Decision == DecisionUnknown {
			return ErrQualityUnknown
		}
		return ErrGateDenied
	}
	if !receipt.Matches(snapshot, digest, now) {
		return ErrReceiptStale
	}
	return nil
}

// Store is the persistence port needed by API/runtime adapters.
type Store interface {
	SaveRun(QualityRun) error
	Run(string, string, string) (QualityRun, error)
	SaveReceipt(PublicationGateReceipt) error
	Receipt(string, string, string) (PublicationGateReceipt, error)
}

// MemoryStore is deterministic reference storage for tests and local previews.
// Production adapters must use the PostgreSQL migration and preserve the same
// conflict/tenant semantics.
type MemoryStore struct {
	mu       sync.RWMutex
	runs     map[string]QualityRun
	receipts map[string]PublicationGateReceipt
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]QualityRun), receipts: make(map[string]PublicationGateReceipt)}
}

func (s *MemoryStore) SaveRun(run QualityRun) error {
	if s == nil || run.Validate() != nil {
		return ErrInvalid
	}
	key := run.Target.key() + "\x00" + run.ID
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.runs[key]; ok {
		if old.SnapshotDigest != run.SnapshotDigest || old.ProfileDigest != run.ProfileDigest || old.ComplianceFingerprint != run.ComplianceFingerprint || (old.Status == "completed" && (run.Status != old.Status || run.Version != old.Version)) {
			return ErrConflict
		}
	}
	s.runs[key] = run
	return nil
}

func (s *MemoryStore) Run(org, workspace, id string) (QualityRun, error) {
	if s == nil || !qualityRefPattern.MatchString(org) || !qualityRefPattern.MatchString(workspace) || !qualityRefPattern.MatchString(id) {
		return QualityRun{}, ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, run := range s.runs {
		if run.Target.OrganizationID == org && run.Target.WorkspaceID == workspace && run.ID == id {
			return run, nil
		}
	}
	return QualityRun{}, ErrNotFound
}

func (s *MemoryStore) SaveReceipt(receipt PublicationGateReceipt) error {
	if s == nil || receipt.Validate() != nil {
		return ErrInvalid
	}
	key := receipt.Target.key() + "\x00" + receipt.ID
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.receipts[key]; ok && (old.SnapshotDigest != receipt.SnapshotDigest || old.ProfileDigest != receipt.ProfileDigest || old.ComplianceFingerprint != receipt.ComplianceFingerprint) {
		return ErrConflict
	}
	s.receipts[key] = receipt
	return nil
}

func (s *MemoryStore) Receipt(org, workspace, id string) (PublicationGateReceipt, error) {
	if s == nil || !qualityRefPattern.MatchString(org) || !qualityRefPattern.MatchString(workspace) || !qualityRefPattern.MatchString(id) {
		return PublicationGateReceipt{}, ErrInvalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, receipt := range s.receipts {
		if receipt.Target.OrganizationID == org && receipt.Target.WorkspaceID == workspace && receipt.ID == id {
			return receipt, nil
		}
	}
	return PublicationGateReceipt{}, ErrNotFound
}

func evaluateIssues(profile Profile, snapshot Snapshot, now time.Time) []Issue {
	issues := make([]Issue, 0, 16)
	add := func(code string, category Category, severity Severity, field, message, expected, observed, remediation string) {
		issues = append(issues, Issue{Code: code, Category: category, Severity: severity, FieldPath: field, Message: message, Expected: expected, Observed: observed, Remediation: remediation})
	}
	targetValues := []string{snapshot.Target.ConnectorID, snapshot.Target.ChannelFamily, snapshot.Target.Locale, snapshot.Target.Jurisdiction}
	profileValues := []string{profile.ConnectorID, profile.ChannelFamily, profile.Locale, profile.Jurisdiction}
	if strings.Join(targetValues, "\x00") != strings.Join(profileValues, "\x00") {
		add("profile_target_mismatch", CategoryContract, SeverityBlock, "target", "Профиль публикации не соответствует цели", profile.ConnectorID, snapshot.Target.ConnectorID, "настроить профиль цели")
	}
	if !profile.Active {
		add("profile_not_configured", CategoryContract, SeverityBlock, "profile", "Профиль публикации не активен", "active", "inactive", "активировать проверенный профиль")
	}
	if now.After(snapshot.SourceFreshAt.Add(profile.FreshnessTTL)) {
		add("source_stale", CategoryContract, SeverityBlock, "source_fresh_at", "Источники товара устарели", "fresh", snapshot.SourceFreshAt.UTC().Format(time.RFC3339), "обновить данные товара")
	}
	if snapshot.ProductVersion < 1 || strings.TrimSpace(snapshot.SKU) == "" || strings.TrimSpace(snapshot.Title) == "" {
		add("identity_required", CategoryIdentity, SeverityBlock, "product", "Не заполнены обязательные идентификаторы или название", "SKU и title", "missing", "заполнить карточку товара")
	}
	if snapshot.ProductStatus == "archived" {
		add("product_archived", CategoryIdentity, SeverityBlock, "product.status", "Архивный товар нельзя публиковать", "active_or_draft", snapshot.ProductStatus, "вернуть товар в рабочий статус")
	}
	if snapshot.Target.OfferID != "" && snapshot.OfferVersion < 1 {
		add("offer_required", CategoryIdentity, SeverityBlock, "offer", "Для цели требуется версия предложения", "version >= 1", "missing", "создать или активировать offer")
	}
	if snapshot.CategoryCode == "" {
		add("category_required", CategoryAttributes, SeverityBlock, "category", "Не задана категория товара", "configured", "missing", "назначить категорию")
	}
	for _, rule := range profile.Rules {
		value, present := snapshotField(snapshot, rule.Field)
		switch rule.Kind {
		case RuleRequired:
			if !present || strings.TrimSpace(value) == "" {
				add(rule.ID, rule.Category, rule.Severity, rule.Field, rule.Message, "present", "missing", rule.Remediation)
			}
		case RuleMaxLength:
			if present && len([]rune(value)) > rule.MaxLength {
				add(rule.ID, rule.Category, rule.Severity, rule.Field, rule.Message, fmt.Sprintf("<= %d", rule.MaxLength), fmt.Sprintf("%d", len([]rune(value))), rule.Remediation)
			}
		case RuleEnum:
			if present && !contains(rule.Allowed, value) {
				add(rule.ID, rule.Category, rule.Severity, rule.Field, rule.Message, strings.Join(rule.Allowed, ","), value, rule.Remediation)
			}
		case RuleMinValue, RuleMaxValue:
			if !present {
				continue
			}
			observed, err := domain.ParseDecimal(value)
			if err != nil {
				add(rule.ID, rule.Category, rule.Severity, rule.Field, rule.Message, "decimal", "invalid", rule.Remediation)
				continue
			}
			if rule.Kind == RuleMinValue && rule.Min != nil {
				cmp, cmpErr := observed.Cmp(*rule.Min)
				if cmpErr != nil || cmp < 0 {
					add(rule.ID, rule.Category, rule.Severity, rule.Field, rule.Message, ">= "+rule.Min.String(), value, rule.Remediation)
				}
			}
			if rule.Kind == RuleMaxValue && rule.Max != nil {
				cmp, cmpErr := observed.Cmp(*rule.Max)
				if cmpErr != nil || cmp > 0 {
					add(rule.ID, rule.Category, rule.Severity, rule.Field, rule.Message, "<= "+rule.Max.String(), value, rule.Remediation)
				}
			}
		}
	}
	if profile.RequiredMedia > len(snapshot.Media) {
		add("media_count", CategoryMedia, SeverityBlock, "media", "Недостаточно опубликованных медиафайлов", fmt.Sprintf(">= %d", profile.RequiredMedia), fmt.Sprintf("%d", len(snapshot.Media)), "загрузить и выпустить медиафайл")
	}
	allowedFormats := make(map[string]struct{}, len(profile.AllowedMediaFormats))
	for _, format := range profile.AllowedMediaFormats {
		allowedFormats[strings.ToLower(format)] = struct{}{}
	}
	for _, media := range snapshot.Media {
		if !media.Released || !media.Safe {
			add("media_not_released", CategoryMedia, SeverityBlock, "media."+media.ID, "Медиа не прошло security release", "released_and_safe", "quarantined", "завершить проверку загрузки")
		}
		if media.Bytes > profile.MaxMediaBytes {
			add("media_size", CategoryMedia, SeverityBlock, "media."+media.ID, "Размер медиа превышает лимит профиля", fmt.Sprintf("<= %d", profile.MaxMediaBytes), fmt.Sprintf("%d", media.Bytes), "уменьшить файл")
		}
		if len(allowedFormats) > 0 {
			if _, ok := allowedFormats[strings.ToLower(media.Format)]; !ok {
				add("media_format", CategoryMedia, SeverityBlock, "media."+media.ID, "Формат медиа не поддержан профилем", strings.Join(profile.AllowedMediaFormats, ","), media.Format, "конвертировать медиа")
			}
		}
	}
	if profile.RequirePrice {
		if snapshot.Price == nil {
			add("price_required", CategoryPriceStock, SeverityBlock, "price", "Не задана цена предложения", "present", "missing", "задать цену")
		} else if snapshot.Price.Currency().String() != profile.Currency {
			add("price_currency", CategoryPriceStock, SeverityBlock, "price.currency", "Валюта цены не совпадает с профилем", profile.Currency, snapshot.Price.Currency().String(), "настроить валюту цены")
		}
	}
	if profile.RequireStock {
		if snapshot.Available == nil {
			add("stock_required", CategoryPriceStock, SeverityBlock, "inventory.available", "Остаток недоступен", "present", "missing", "обновить остатки")
		} else if snapshot.Available.Value.Coefficient() == 0 {
			add("stock_zero", CategoryPriceStock, SeverityWarn, "inventory.available", "Остаток равен нулю", "> 0", "0", "пополнить остаток или проверить policy")
		}
	}
	if !snapshot.CompliancePresent {
		add("compliance_unknown", CategoryCompliance, SeverityBlock, "compliance", "Нет актуального решения соответствия", "fingerprint", "missing", "запустить проверку соответствия")
	} else {
		switch snapshot.Compliance.Outcome {
		case "block":
			add("compliance_block", CategoryCompliance, SeverityBlock, "compliance", "Публикация запрещена compliance policy", "allow", "block", "исправить документы соответствия")
		case "approval_required":
			add("compliance_approval", CategoryCompliance, SeverityApproval, "compliance", "Требуется согласование соответствия", "approved", "approval_required", "отправить на согласование")
		case "warn":
			add("compliance_warning", CategoryCompliance, SeverityWarn, "compliance", "Есть предупреждение compliance policy", "allow", "warn", "проверить предупреждение")
		}
	}
	if !snapshot.MappingConfigured {
		add("mapping_missing", CategoryCapability, SeverityBlock, "mapping", "Не настроено сопоставление цели", "configured", "missing", "настроить mapping")
	}
	if !snapshot.CapabilityAdmitted || !snapshot.ProductsWriteEnabled {
		add("capability_unavailable", CategoryCapability, SeverityBlock, "capability.products_write", "Runtime не допускает запись товаров для цели", "admitted_and_enabled", "unavailable", "включить только проверенную capability")
	}
	if snapshot.Target.Locale == "" {
		add("locale_missing", CategoryLocalization, SeverityBlock, "target.locale", "Не задана локаль публикации", "locale", "missing", "настроить локаль")
	}
	return issues
}

func scoreIssues(profile Profile, issues []Issue) (int64, map[Category]int64) {
	scores := make(map[Category]int64, len(categoryOrder))
	counts := make(map[Category]int64, len(categoryOrder))
	for _, category := range categoryOrder {
		scores[category] = 10_000
	}
	for _, issue := range issues {
		counts[issue.Category]++
		delta := int64(0)
		switch issue.Severity {
		case SeverityBlock:
			delta = 10_000
		case SeverityApproval:
			delta = 8_000
		case SeverityWarn:
			delta = 1_500
		case SeverityInfo:
			delta = 250
		}
		scores[issue.Category] -= delta
		if scores[issue.Category] < 0 {
			scores[issue.Category] = 0
		}
	}
	var weighted, weightTotal int64
	for _, category := range categoryOrder {
		weight := profile.Weights[category]
		if weight == 0 {
			weight = 1
		}
		weighted += scores[category] * weight
		weightTotal += weight
	}
	if weightTotal == 0 {
		return 0, scores
	}
	return (weighted + weightTotal/2) / weightTotal, scores
}

func decisionFor(issues []Issue) Decision {
	result := DecisionReady
	var special Decision
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityBlock:
			switch issue.Code {
			case "source_stale":
				if special == "" {
					special = DecisionStale
				}
			case "profile_not_configured":
				special = DecisionNotConfigured
			case "capability_unavailable":
				if special == "" || special == DecisionStale {
					special = DecisionUnsupported
				}
			case "compliance_unknown":
				if special == "" || special == DecisionStale || special == DecisionUnsupported {
					special = DecisionUnknown
				}
			default:
				return DecisionBlocked
			}
		case SeverityApproval:
			if result != DecisionBlocked {
				result = DecisionApprovalRequired
			}
		case SeverityWarn:
			if result == DecisionReady {
				result = DecisionReadyWithWarnings
			}
		}
	}
	if special != "" {
		return special
	}
	return result
}

func deduplicateIssues(issues []Issue) []Issue {
	seen := make(map[string]struct{}, len(issues))
	out := make([]Issue, 0, len(issues))
	for _, issue := range issues {
		key := string(issue.Category) + "\x00" + issue.Code + "\x00" + issue.FieldPath
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, issue)
	}
	return out
}

func snapshotField(snapshot Snapshot, field string) (string, bool) {
	switch field {
	case "sku":
		return snapshot.SKU, snapshot.SKU != ""
	case "gtin":
		return snapshot.GTIN, snapshot.GTIN != ""
	case "title":
		return snapshot.Title, snapshot.Title != ""
	case "description":
		return snapshot.Description, snapshot.Description != ""
	case "category":
		return snapshot.CategoryCode, snapshot.CategoryCode != ""
	case "product_status":
		return snapshot.ProductStatus, snapshot.ProductStatus != ""
	case "price_minor":
		if snapshot.Price == nil {
			return "", false
		}
		return strconv.FormatInt(snapshot.Price.MinorUnits(), 10), true
	case "price_currency":
		if snapshot.Price == nil {
			return "", false
		}
		return snapshot.Price.Currency().String(), true
	case "inventory_available":
		if snapshot.Available == nil {
			return "", false
		}
		return snapshot.Available.Value.String(), true
	case "inventory_unit":
		if snapshot.Available == nil {
			return "", false
		}
		return snapshot.Available.Unit.String(), true
	default:
		value, ok := snapshot.Attributes[field]
		return value, ok
	}
}

func SnapshotDigest(snapshot Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d|%d|%d|%d|%d|%d|%d|%s|%s|%s|%s|%s|%s|%s|%s|%s|%t|%t|%t|%s|%s|%s|%s|%s", snapshot.Target.key(), snapshot.ProductVersion, snapshot.OfferVersion, snapshot.PriceVersion, snapshot.InventoryVersion, snapshot.MediaVersion, snapshot.MappingVersion, snapshot.CapabilityVersion, snapshot.SKU, snapshot.GTIN, snapshot.Title, snapshot.Description, snapshot.CategoryCode, snapshot.ProductStatus, snapshot.Compliance.Outcome, snapshot.Compliance.Fingerprint, snapshot.Compliance.EvaluatedAt.UTC().Format(time.RFC3339Nano), snapshot.MappingConfigured, snapshot.CapabilityAdmitted, snapshot.ProductsWriteEnabled, snapshot.SourceFreshAt.UTC().Format(time.RFC3339Nano), snapshot.CatalogDigest, snapshot.PIMDigest, snapshot.MediaDigest, snapshot.AssembledAt.UTC().Format(time.RFC3339Nano))
	keys := make([]string, 0, len(snapshot.Attributes))
	for key := range snapshot.Attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "|a:%s=%s", key, snapshot.Attributes[key])
	}
	media := append([]MediaAsset(nil), snapshot.Media...)
	sort.Slice(media, func(i, j int) bool { return media[i].ID < media[j].ID })
	for _, item := range media {
		fmt.Fprintf(&b, "|m:%s:%s:%d:%d:%d:%t:%t", item.ID, item.Format, item.Bytes, item.Width, item.Height, item.Released, item.Safe)
	}
	if snapshot.Price != nil {
		fmt.Fprintf(&b, "|p:%d:%s", snapshot.Price.MinorUnits(), snapshot.Price.Currency().String())
	}
	if snapshot.Available != nil {
		fmt.Fprintf(&b, "|q:%d:%d:%s", snapshot.Available.Value.Coefficient(), snapshot.Available.Value.Scale(), snapshot.Available.Unit.String())
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func complianceFingerprint(snapshot Snapshot) string {
	if snapshot.CompliancePresent {
		return snapshot.Compliance.Fingerprint
	}
	return strings.Repeat("0", 64)
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func isUTC(value time.Time) bool { return !value.IsZero() && value.Location() == time.UTC }

func isOptionalUTC(value time.Time) bool { return value.IsZero() || value.Location() == time.UTC }

func isLowerHex(value string) bool {
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
