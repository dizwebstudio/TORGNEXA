package uploads

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const (
	maxReasonCodeBytes  = 96
	maxScannerTextBytes = 128
)

var machineCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var rescanReasonPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)

// ValidRescanReasonCode is shared with persistence adapters so direct repository
// callers cannot bypass the event-contract bound enforced by Pipeline.
func ValidRescanReasonCode(value string) bool { return rescanReasonPattern.MatchString(value) }

// QuarantinedObject is a bounded random-access view of the immutable quarantine
// object. Object-store adapters may implement ReaderAt with range requests; local
// adapters may use a file. The pipeline never accepts a client path or URL.
type QuarantinedObject interface {
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Closer
}

// QuarantineReader opens only the server-derived quarantine object for an upload.
type QuarantineReader interface {
	OpenQuarantined(context.Context, tenancy.Scope, ID, string) (QuarantinedObject, error)
}

type CheckOutcome string

const (
	CheckPass CheckOutcome = "pass"
	CheckFail CheckOutcome = "fail"
)

func (v CheckOutcome) Valid() bool { return v == CheckPass || v == CheckFail }

type CheckResult struct {
	Code    string       `json:"code"`
	Outcome CheckOutcome `json:"outcome"`
}

func (v CheckResult) Validate() error {
	if !machineCodePattern.MatchString(v.Code) || !v.Outcome.Valid() {
		return ErrInvalid
	}
	return nil
}

type ScannerStatus string

const (
	ScannerClean    ScannerStatus = "clean"
	ScannerInfected ScannerStatus = "infected"
	ScannerError    ScannerStatus = "error"
	ScannerNotRun   ScannerStatus = "not_run"
)

func (v ScannerStatus) Valid() bool {
	return v == ScannerClean || v == ScannerInfected || v == ScannerError || v == ScannerNotRun
}

type ScanRequest struct {
	UploadID    ID
	SHA256      string
	SizeBytes   int64
	MediaType   string
	Policy      string
	ContentType string
}

type ScanResult struct {
	ScannerName      string        `json:"scanner_name"`
	EngineVersion    string        `json:"engine_version"`
	SignatureVersion string        `json:"signature_version"`
	Status           ScannerStatus `json:"status"`
	ThreatCode       string        `json:"threat_code,omitempty"`
}

func (v ScanResult) Validate() error {
	if !machineCodePattern.MatchString(v.ScannerName) || !validScannerText(v.EngineVersion) || !validScannerText(v.SignatureVersion) || !v.Status.Valid() {
		return ErrInvalid
	}
	if v.Status == ScannerInfected {
		if !machineCodePattern.MatchString(v.ThreatCode) {
			return ErrInvalid
		}
	} else if v.ThreatCode != "" {
		return ErrInvalid
	}
	return nil
}

type MalwareScanner interface {
	Scan(context.Context, ScanRequest, io.Reader) (ScanResult, error)
}

type EvidenceDecision string

const (
	DecisionClean    EvidenceDecision = "clean"
	DecisionRejected EvidenceDecision = "rejected"
	DecisionError    EvidenceDecision = "error"
)

func (v EvidenceDecision) Valid() bool {
	return v == DecisionClean || v == DecisionRejected || v == DecisionError
}

// SecurityEvidence is immutable and content-free. It contains only bounded
// machine-readable outcomes needed to reproduce a release decision.
type SecurityEvidence struct {
	ID                string                 `json:"id"`
	UploadID          ID                     `json:"upload_id"`
	OrganizationID    tenancy.OrganizationID `json:"-"`
	WorkspaceID       tenancy.WorkspaceID    `json:"-"`
	Attempt           int64                  `json:"attempt"`
	PolicyVersion     string                 `json:"policy_version"`
	ContentSHA256     string                 `json:"content_sha256"`
	ContentSizeBytes  int64                  `json:"content_size_bytes"`
	DetectedMediaType string                 `json:"detected_media_type"`
	Extension         string                 `json:"extension"`
	Decision          EvidenceDecision       `json:"decision"`
	ReasonCode        string                 `json:"reason_code"`
	Checks            []CheckResult          `json:"checks"`
	Scanner           ScanResult             `json:"scanner"`
	RescanOf          string                 `json:"rescan_of,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
}

func (e SecurityEvidence) Validate(scope tenancy.Scope, maxBytes int64) error {
	if !scope.Valid() || !securityEvidenceIDPattern.MatchString(e.ID) || !e.UploadID.Valid() || e.OrganizationID != scope.OrganizationID() || e.WorkspaceID != scope.WorkspaceID() || e.Attempt < 1 || !sourcePattern.MatchString(e.PolicyVersion) || !sha256Pattern.MatchString(e.ContentSHA256) || e.ContentSizeBytes < 0 || e.ContentSizeBytes > maxBytes || !validMediaType(e.DetectedMediaType) || !validExtension(e.Extension) || !e.Decision.Valid() || !machineCodePattern.MatchString(e.ReasonCode) || !isUTC(e.CreatedAt) || len(e.Checks) < 1 || len(e.Checks) > 32 {
		return ErrInvalid
	}
	for _, check := range e.Checks {
		if check.Validate() != nil {
			return ErrInvalid
		}
	}
	if e.Scanner.Validate() != nil {
		return ErrInvalid
	}
	if e.Decision == DecisionClean && e.Scanner.Status != ScannerClean {
		return ErrInvalid
	}
	if e.Decision == DecisionError && e.Scanner.Status != ScannerError {
		return ErrInvalid
	}
	if e.Decision == DecisionRejected && e.Scanner.Status != ScannerInfected && e.Scanner.Status != ScannerNotRun && e.Scanner.Status != ScannerError {
		return ErrInvalid
	}
	if e.RescanOf != "" && !securityEvidenceIDPattern.MatchString(e.RescanOf) {
		return ErrInvalid
	}
	return nil
}

// SecurityPipelineRepository owns every state transition after quarantine and
// stores immutable evidence in the same transaction as terminal decisions.
type SecurityPipelineRepository interface {
	Repository
	MarkValidated(context.Context, tenancy.Scope, ID, time.Time) (Record, error)
	MarkScanning(context.Context, tenancy.Scope, ID, time.Time) (Record, error)
	RecordDecision(context.Context, tenancy.Scope, ID, SecurityEvidence, Mutation) (Record, SecurityEvidence, error)
	MarkReleased(context.Context, tenancy.Scope, ID, string, StoredObject, Mutation) (Record, error)
	RequestRescan(context.Context, tenancy.Scope, ID, string, Mutation) (Record, error)
	ListSecurityEvidence(context.Context, tenancy.Scope, ID, int) ([]SecurityEvidence, error)
}

type MetricObservation struct {
	Operation string
	Outcome   string
	Bytes     int64
	Duration  time.Duration
}

type Metrics interface {
	ObserveUploadSecurity(MetricObservation)
}

type noopMetrics struct{}

func (noopMetrics) ObserveUploadSecurity(MetricObservation) {}

// Pipeline executes validation -> malware scan -> immutable decision -> release.
// It is resumable: scanner errors leave the record in SCANNING so a later call
// can retry without bypassing validation.
type Pipeline struct {
	repository SecurityPipelineRepository
	quarantine QuarantineReader
	release    ReleaseStore
	scanner    MalwareScanner
	metrics    Metrics
	policy     Policy
	now        func() time.Time
	random     io.Reader
}

func NewPipeline(repository SecurityPipelineRepository, quarantine QuarantineReader, release ReleaseStore, scanner MalwareScanner, metrics Metrics, policy Policy) (*Pipeline, error) {
	return newPipeline(repository, quarantine, release, scanner, metrics, policy, time.Now, rand.Reader)
}

func newPipeline(repository SecurityPipelineRepository, quarantine QuarantineReader, release ReleaseStore, scanner MalwareScanner, metrics Metrics, policy Policy, now func() time.Time, random io.Reader) (*Pipeline, error) {
	if repository == nil || quarantine == nil || release == nil || scanner == nil || now == nil || random == nil || policy.Validate() != nil {
		return nil, ErrInvalid
	}
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &Pipeline{repository: repository, quarantine: quarantine, release: release, scanner: scanner, metrics: metrics, policy: policy, now: now, random: random}, nil
}

// Process advances one upload as far as possible. A clean upload is promoted and
// returned as a current ReleasedObjectRef. Rejected content returns
// ErrSecurityRejected; scanner availability errors are fail-closed per policy.
func (p *Pipeline) Process(ctx context.Context, scope tenancy.Scope, id ID, base Mutation) (ReleasedObjectRef, error) {
	started := time.Now()
	outcome := "error"
	bytesObserved := int64(0)
	defer func() {
		p.metrics.ObserveUploadSecurity(MetricObservation{Operation: "process", Outcome: outcome, Bytes: bytesObserved, Duration: time.Since(started)})
	}()
	if ctx == nil || p == nil || p.repository == nil || !scope.Valid() || !id.Valid() || base.Validate() != nil {
		return ReleasedObjectRef{}, ErrInvalid
	}
	record, err := p.repository.Get(ctx, scope, id)
	if err != nil {
		return ReleasedObjectRef{}, err
	}
	if record.Validate(scope, p.policy.MaxFileBytes) != nil {
		return ReleasedObjectRef{}, ErrInvalid
	}
	bytesObserved = record.ContentSizeBytes
	var validation validationResult
	if record.State == StateQuarantined {
		validation, err = p.validateQuarantined(ctx, scope, record)
		if err != nil {
			if errors.Is(err, ErrSecurityRejected) {
				if _, _, recErr := p.recordValidationReject(ctx, scope, record, validation, base); recErr != nil {
					return ReleasedObjectRef{}, recErr
				}
				outcome = "rejected"
				return ReleasedObjectRef{}, ErrSecurityRejected
			}
			return ReleasedObjectRef{}, err
		}
		record, err = p.repository.MarkValidated(ctx, scope, id, p.now().UTC())
		if err != nil {
			return ReleasedObjectRef{}, err
		}
	}
	if record.State == StateValidated {
		record, err = p.repository.MarkScanning(ctx, scope, id, p.now().UTC())
		if err != nil {
			return ReleasedObjectRef{}, err
		}
	}
	if record.State == StateScanning {
		// Re-run static validation on every scanner retry so policy/signature retries
		// cannot accidentally scan bytes that changed after the first validation.
		validation, err = p.validateQuarantined(ctx, scope, record)
		if err != nil {
			if errors.Is(err, ErrSecurityRejected) {
				if _, _, recErr := p.recordValidationReject(ctx, scope, record, validation, base); recErr != nil {
					return ReleasedObjectRef{}, recErr
				}
				outcome = "rejected"
				return ReleasedObjectRef{}, ErrSecurityRejected
			}
			return ReleasedObjectRef{}, err
		}
		record, err = p.scan(ctx, scope, record, validation, base)
		if err != nil {
			if errors.Is(err, ErrSecurityRejected) {
				outcome = "rejected"
			}
			return ReleasedObjectRef{}, err
		}
	}
	if record.State == StateRejected {
		outcome = "rejected"
		return ReleasedObjectRef{}, ErrSecurityRejected
	}
	if record.State == StateClean {
		stored, promoteErr := p.release.Promote(ctx, scope, id, record.QuarantineObjectKey, record.ContentSHA256)
		if promoteErr != nil {
			return ReleasedObjectRef{}, fmt.Errorf("%w: release promote", ErrStorage)
		}
		if stored.ValidateReleasedFor(scope, id, p.policy.MaxFileBytes) != nil || stored.SizeBytes != record.ContentSizeBytes || stored.SHA256 != record.ContentSHA256 {
			return ReleasedObjectRef{}, ErrStorage
		}
		record, err = p.repository.MarkReleased(ctx, scope, id, record.SecurityEvidenceID, stored, p.childMutation(base, id, "released"))
		if err != nil {
			return ReleasedObjectRef{}, err
		}
	}
	if record.State == StateReleased {
		gate, gateErr := NewAccessGate(p.repository, p.policy)
		if gateErr != nil {
			return ReleasedObjectRef{}, gateErr
		}
		ref, gateErr := gate.ResolveReleased(ctx, scope, id)
		if gateErr != nil {
			return ReleasedObjectRef{}, gateErr
		}
		outcome = "released"
		return ref, nil
	}
	return ReleasedObjectRef{}, ErrConflict
}

// RequestRescan revokes current released capabilities before any re-scan work.
// Existing references fail ValidateReleasedRef after this transition.
func (p *Pipeline) RequestRescan(ctx context.Context, scope tenancy.Scope, id ID, reason string, base Mutation) (Record, error) {
	if ctx == nil || p == nil || !scope.Valid() || !id.Valid() || !ValidRescanReasonCode(reason) || base.Validate() != nil {
		return Record{}, ErrInvalid
	}
	return p.repository.RequestRescan(ctx, scope, id, reason, p.childMutation(base, id, "rescan"))
}

type validationResult struct {
	mediaType  string
	extension  string
	checks     []CheckResult
	reasonCode string
}

func (p *Pipeline) validateQuarantined(ctx context.Context, scope tenancy.Scope, record Record) (validationResult, error) {
	result := validationResult{checks: []CheckResult{}}
	add := func(code string, ok bool) {
		out := CheckPass
		if !ok {
			out = CheckFail
		}
		result.checks = append(result.checks, CheckResult{Code: code, Outcome: out})
		if !ok && result.reasonCode == "" {
			result.reasonCode = code
		}
	}
	filenameOK := safeUploadFilename(record.Metadata.OriginalFilename)
	add("path_normalization", filenameOK)
	if !filenameOK {
		result.mediaType = "application/octet-stream"
		result.extension = normalizedExtension(record.Metadata.OriginalFilename)
		return result, ErrSecurityRejected
	}
	result.extension = normalizedExtension(record.Metadata.OriginalFilename)
	obj, err := p.quarantine.OpenQuarantined(ctx, scope, record.ID, record.QuarantineObjectKey)
	if err != nil {
		return result, fmt.Errorf("%w: quarantine open", ErrStorage)
	}
	defer obj.Close()
	actualSize, actualSHA, err := verifyObject(obj, p.policy.MaxFileBytes)
	if err != nil {
		return result, err
	}
	integrityOK := actualSize == record.ContentSizeBytes && actualSHA == record.ContentSHA256
	add("content_integrity", integrityOK)
	if !integrityOK {
		result.mediaType = "application/octet-stream"
		return result, ErrSecurityRejected
	}
	mediaType, err := detectMediaType(obj, actualSize, result.extension, p.policy)
	if err != nil {
		add("content_type_sniff", false)
		result.mediaType = "application/octet-stream"
		return result, ErrSecurityRejected
	}
	result.mediaType = mediaType
	add("content_type_sniff", true)
	extensionOK := extensionMatchesMedia(result.extension, mediaType)
	add("extension_mismatch", extensionOK)
	if !extensionOK {
		return result, ErrSecurityRejected
	}
	declaredOK := declaredMediaMatches(record.Metadata.DeclaredMediaType, mediaType, result.extension)
	add("declared_mime_match", declaredOK)
	if !declaredOK {
		return result, ErrSecurityRejected
	}
	archiveOK := true
	if isZipMedia(mediaType) {
		if _, err := inspectZip(obj, actualSize, 1, p.policy, &archiveBudget{}); err != nil {
			archiveOK = false
		}
	}
	add("archive_limits", archiveOK)
	if !archiveOK {
		return result, ErrSecurityRejected
	}
	parserOK := validateParser(obj, mediaType, p.policy) == nil
	add("parser_limits", parserOK)
	if !parserOK {
		return result, ErrSecurityRejected
	}
	sort.Slice(result.checks, func(i, j int) bool { return result.checks[i].Code < result.checks[j].Code })
	result.reasonCode = "validation_passed"
	return result, nil
}

func (p *Pipeline) recordValidationReject(ctx context.Context, scope tenancy.Scope, record Record, validation validationResult, base Mutation) (Record, SecurityEvidence, error) {
	evidence, err := p.makeEvidence(scope, record, validation, DecisionRejected, "validation_rejected", ScanResult{ScannerName: "policy", EngineVersion: "v1", SignatureVersion: p.policy.Version, Status: ScannerNotRun}, "")
	if err != nil {
		return Record{}, SecurityEvidence{}, err
	}
	return p.repository.RecordDecision(ctx, scope, record.ID, evidence, p.childMutation(base, record.ID, "rejected_"+evidence.ID))
}

func (p *Pipeline) scan(ctx context.Context, scope tenancy.Scope, record Record, validation validationResult, base Mutation) (Record, error) {
	obj, err := p.quarantine.OpenQuarantined(ctx, scope, record.ID, record.QuarantineObjectKey)
	if err != nil {
		return Record{}, fmt.Errorf("%w: quarantine open", ErrStorage)
	}
	defer obj.Close()
	actualSize, actualSHA, err := verifyObject(obj, p.policy.MaxFileBytes)
	if err != nil || actualSize != record.ContentSizeBytes || actualSHA != record.ContentSHA256 {
		validation.checks = upsertCheck(validation.checks, "content_integrity", CheckFail)
		evidence, eErr := p.makeEvidence(scope, record, validation, DecisionRejected, "content_integrity_changed", ScanResult{ScannerName: "policy", EngineVersion: "v1", SignatureVersion: p.policy.Version, Status: ScannerNotRun}, record.SecurityEvidenceID)
		if eErr != nil {
			return Record{}, eErr
		}
		rejected, _, eErr := p.repository.RecordDecision(ctx, scope, record.ID, evidence, p.childMutation(base, record.ID, "rejected_"+evidence.ID))
		if eErr != nil {
			return Record{}, eErr
		}
		return rejected, ErrSecurityRejected
	}
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		return Record{}, ErrStorage
	}
	scanStarted := time.Now()
	counter := &countingReader{reader: io.LimitReader(obj, record.ContentSizeBytes+1)}
	result, scanErr := p.scanner.Scan(ctx, ScanRequest{UploadID: record.ID, SHA256: record.ContentSHA256, SizeBytes: record.ContentSizeBytes, MediaType: validation.mediaType, Policy: p.policy.Version, ContentType: validation.mediaType}, counter)
	if scanErr != nil || result.Validate() != nil || counter.read != record.ContentSizeBytes || (result.Status != ScannerClean && result.Status != ScannerInfected) {
		failure := ScanResult{ScannerName: "scanner", EngineVersion: "unknown", SignatureVersion: "unknown", Status: ScannerError}
		if result.ScannerName != "" && machineCodePattern.MatchString(result.ScannerName) {
			failure.ScannerName = result.ScannerName
		}
		if validScannerText(result.EngineVersion) {
			failure.EngineVersion = result.EngineVersion
		}
		if validScannerText(result.SignatureVersion) {
			failure.SignatureVersion = result.SignatureVersion
		}
		evidence, eErr := p.makeEvidence(scope, record, validation, DecisionError, "scanner_unavailable", failure, record.SecurityEvidenceID)
		if eErr == nil {
			_, _, _ = p.repository.RecordDecision(ctx, scope, record.ID, evidence, p.childMutation(base, record.ID, "scanner_error_"+evidence.ID))
		}
		p.metrics.ObserveUploadSecurity(MetricObservation{Operation: "malware_scan", Outcome: "error", Bytes: record.ContentSizeBytes, Duration: time.Since(scanStarted)})
		if p.policy.ScannerFailureMode == ScannerFailureReject {
			rejectEvidence, eErr := p.makeEvidence(scope, record, validation, DecisionRejected, "scanner_unavailable", failure, record.SecurityEvidenceID)
			if eErr != nil {
				return Record{}, eErr
			}
			rejected, _, eErr := p.repository.RecordDecision(ctx, scope, record.ID, rejectEvidence, p.childMutation(base, record.ID, "rejected_"+rejectEvidence.ID))
			if eErr != nil {
				return Record{}, eErr
			}
			return rejected, ErrSecurityRejected
		}
		return Record{}, ErrScannerUnavailable
	}
	p.metrics.ObserveUploadSecurity(MetricObservation{Operation: "malware_scan", Outcome: string(result.Status), Bytes: record.ContentSizeBytes, Duration: time.Since(scanStarted)})
	if result.Status == ScannerInfected {
		evidence, eErr := p.makeEvidence(scope, record, validation, DecisionRejected, "malware_detected", result, record.SecurityEvidenceID)
		if eErr != nil {
			return Record{}, eErr
		}
		rejected, _, eErr := p.repository.RecordDecision(ctx, scope, record.ID, evidence, p.childMutation(base, record.ID, "rejected_"+evidence.ID))
		if eErr != nil {
			return Record{}, eErr
		}
		return rejected, ErrSecurityRejected
	}
	if result.Status != ScannerClean {
		return Record{}, ErrScannerUnavailable
	}
	evidence, eErr := p.makeEvidence(scope, record, validation, DecisionClean, "security_checks_passed", result, record.SecurityEvidenceID)
	if eErr != nil {
		return Record{}, eErr
	}
	clean, _, eErr := p.repository.RecordDecision(ctx, scope, record.ID, evidence, p.childMutation(base, record.ID, "clean_"+evidence.ID))
	return clean, eErr
}

type countingReader struct {
	reader io.Reader
	read   int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func upsertCheck(checks []CheckResult, code string, outcome CheckOutcome) []CheckResult {
	out := append([]CheckResult(nil), checks...)
	for i := range out {
		if out[i].Code == code {
			out[i].Outcome = outcome
			return out
		}
	}
	return append(out, CheckResult{Code: code, Outcome: outcome})
}

func (p *Pipeline) makeEvidence(scope tenancy.Scope, record Record, validation validationResult, decision EvidenceDecision, reason string, scan ScanResult, rescanOf string) (SecurityEvidence, error) {
	id, err := newEvidenceID(p.random)
	if err != nil {
		return SecurityEvidence{}, err
	}
	if validation.mediaType == "" {
		validation.mediaType = "application/octet-stream"
	}
	if validation.extension == "" {
		validation.extension = "none"
	}
	e := SecurityEvidence{ID: id, UploadID: record.ID, OrganizationID: scope.OrganizationID(), WorkspaceID: scope.WorkspaceID(), Attempt: 1, PolicyVersion: p.policy.Version, ContentSHA256: record.ContentSHA256, ContentSizeBytes: record.ContentSizeBytes, DetectedMediaType: validation.mediaType, Extension: validation.extension, Decision: decision, ReasonCode: reason, Checks: append([]CheckResult(nil), validation.checks...), Scanner: scan, RescanOf: rescanOf, CreatedAt: p.now().UTC()}
	if len(e.Checks) == 0 {
		e.Checks = []CheckResult{{Code: "pipeline", Outcome: CheckFail}}
	}
	// Attempt is assigned atomically by the repository. Domain validation uses 1
	// here as a non-zero placeholder and the persisted copy is revalidated later.
	if e.Validate(scope, p.policy.MaxFileBytes) != nil {
		return SecurityEvidence{}, ErrInvalid
	}
	return e, nil
}

func (p *Pipeline) childMutation(base Mutation, id ID, action string) Mutation {
	h := sha256.Sum256([]byte(base.EventID + "\x00" + id.String() + "\x00" + action))
	base.EventID = "evt_" + hex.EncodeToString(h[:16])
	base.OccurredAt = p.now().UTC()
	return base
}

func newEvidenceID(random io.Reader) (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", err
	}
	return "uev_" + hex.EncodeToString(raw[:]), nil
}

func verifyObject(obj QuarantinedObject, maxBytes int64) (int64, string, error) {
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		return 0, "", ErrStorage
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(obj, maxBytes+1))
	if err != nil {
		return 0, "", ErrStorage
	}
	if n > maxBytes {
		return 0, "", ErrSecurityRejected
	}
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		return 0, "", ErrStorage
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func safeUploadFilename(name string) bool {
	if name == "" || name != strings.TrimSpace(name) || !utf8.ValidString(name) || strings.ContainsAny(name, `/\\`) || name == "." || name == ".." || strings.Contains(name, "\x00") {
		return false
	}
	if len([]rune(name)) > maxFilenameRunes {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func normalizedExtension(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext == "" {
		return "none"
	}
	if len(ext) > 16 {
		return "invalid"
	}
	for _, r := range ext[1:] {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return "invalid"
		}
	}
	return ext
}
func validExtension(ext string) bool {
	if ext == "none" {
		return true
	}
	if len(ext) < 2 || len(ext) > 16 || ext[0] != '.' {
		return false
	}
	for _, r := range ext[1:] {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
func validMediaType(value string) bool {
	base, _, err := mime.ParseMediaType(value)
	return err == nil && mediaTypePattern.MatchString(base)
}
func validScannerText(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && utf8.ValidString(value) && len(value) <= maxScannerTextBytes && !strings.ContainsAny(value, "\r\n\x00") && !secrets.SensitiveString(value)
}

func detectMediaType(obj QuarantinedObject, size int64, ext string, policy Policy) (string, error) {
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	prefix := make([]byte, 4096)
	n, err := io.ReadFull(obj, prefix)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	prefix = prefix[:n]
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if len(prefix) >= 4 && bytes.Equal(prefix[:4], []byte{'P', 'K', 3, 4}) {
		if ext == ".xlsx" {
			ok, err := looksLikeXLSX(obj, size, policy)
			if err != nil {
				return "", err
			}
			if ok {
				return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
			}
		}
		return "application/zip", nil
	}
	if bytes.HasPrefix(prefix, []byte("%PDF-")) {
		return "application/pdf", nil
	}
	if len(prefix) >= 8 && bytes.Equal(prefix[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png", nil
	}
	if len(prefix) >= 3 && prefix[0] == 0xff && prefix[1] == 0xd8 && prefix[2] == 0xff {
		return "image/jpeg", nil
	}
	if bytes.HasPrefix(prefix, []byte("GIF87a")) || bytes.HasPrefix(prefix, []byte("GIF89a")) {
		return "image/gif", nil
	}
	trim := bytes.TrimSpace(prefix)
	if len(trim) > 0 && (trim[0] == '{' || trim[0] == '[') {
		if validateJSON(obj, policy) == nil {
			return "application/json", nil
		}
	}
	if len(trim) > 0 && trim[0] == '<' {
		if validateXML(obj, policy) == nil {
			return "application/xml", nil
		}
	}
	detected := http.DetectContentType(prefix)
	base, _, _ := mime.ParseMediaType(detected)
	if base == "text/plain" {
		switch ext {
		case ".csv":
			return "text/csv", nil
		case ".yaml", ".yml":
			return "application/yaml", nil
		case ".txt":
			return "text/plain", nil
		}
	}
	if mediaTypePattern.MatchString(base) {
		return base, nil
	}
	return "application/octet-stream", nil
}

func extensionMatchesMedia(ext, media string) bool {
	allowed := map[string][]string{
		".csv": {"text/csv"}, ".json": {"application/json"}, ".xml": {"application/xml", "text/xml"},
		".yaml": {"application/yaml", "text/yaml", "text/plain"}, ".yml": {"application/yaml", "text/yaml", "text/plain"},
		".zip": {"application/zip"}, ".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		".pdf": {"application/pdf"}, ".png": {"image/png"}, ".jpg": {"image/jpeg"}, ".jpeg": {"image/jpeg"},
		".gif": {"image/gif"}, ".txt": {"text/plain"},
	}
	if ext == "none" {
		return media != "application/octet-stream" && media != "application/x-dosexec"
	}
	values, ok := allowed[ext]
	if !ok {
		return false
	}
	for _, v := range values {
		if media == v {
			return true
		}
	}
	return false
}

func declaredMediaMatches(declared, detected, ext string) bool {
	if declared == "" {
		return true
	}
	base, _, err := mime.ParseMediaType(declared)
	if err != nil {
		return false
	}
	if base == detected {
		return true
	}
	if ext == ".xml" && (base == "text/xml" || base == "application/xml") && (detected == "text/xml" || detected == "application/xml") {
		return true
	}
	if (ext == ".yaml" || ext == ".yml") && (base == "application/yaml" || base == "text/yaml" || base == "text/plain") && (detected == "application/yaml" || detected == "text/yaml" || detected == "text/plain") {
		return true
	}
	return false
}

func isZipMedia(media string) bool {
	return media == "application/zip" || media == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

func looksLikeXLSX(obj QuarantinedObject, size int64, policy Policy) (bool, error) {
	if size < 0 {
		return false, ErrInvalid
	}
	zr, err := zip.NewReader(obj, size)
	if err != nil {
		return false, err
	}
	hasTypes, hasWorkbook := false, false
	for i, f := range zr.File {
		if i >= policy.MaxArchiveEntries {
			return false, ErrSecurityRejected
		}
		switch f.Name {
		case "[Content_Types].xml":
			hasTypes = true
		case "xl/workbook.xml":
			hasWorkbook = true
		}
	}
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	return hasTypes && hasWorkbook, nil
}

type archiveBudget struct {
	entries    int
	expanded   int64
	compressed int64
}

func inspectZip(readerAt io.ReaderAt, size int64, depth int, policy Policy, budget *archiveBudget) (int64, error) {
	if depth > policy.MaxArchiveDepth || size < 0 {
		return 0, ErrSecurityRejected
	}
	zr, err := zip.NewReader(readerAt, size)
	if err != nil {
		return 0, ErrSecurityRejected
	}
	var localExpanded int64
	for _, f := range zr.File {
		budget.entries++
		if budget.entries > policy.MaxArchiveEntries {
			return 0, ErrSecurityRejected
		}
		if !safeArchivePath(f.Name) || f.Mode()&os.ModeSymlink != 0 || f.Flags&0x1 != 0 {
			return 0, ErrSecurityRejected
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if int64(f.UncompressedSize64) > policy.MaxArchiveEntryBytes {
			return 0, ErrSecurityRejected
		}
		compressed := int64(f.CompressedSize64)
		if compressed < 0 {
			return 0, ErrSecurityRejected
		}
		budget.compressed += compressed
		rc, err := f.Open()
		if err != nil {
			return 0, ErrSecurityRejected
		}
		limit := policy.MaxArchiveEntryBytes + 1
		var nested bytes.Buffer
		var writer io.Writer = io.Discard
		captureNested := strings.EqualFold(path.Ext(f.Name), ".zip")
		if captureNested {
			if int64(f.UncompressedSize64) > policy.MaxNestedArchiveBytes {
				rc.Close()
				return 0, ErrSecurityRejected
			}
			writer = &nested
		}
		n, copyErr := io.Copy(writer, io.LimitReader(rc, limit))
		closeErr := rc.Close()
		if copyErr != nil || closeErr != nil || n > policy.MaxArchiveEntryBytes || uint64(n) != f.UncompressedSize64 {
			return 0, ErrSecurityRejected
		}
		budget.expanded += n
		localExpanded += n
		if budget.expanded > policy.MaxArchiveExpandedBytes {
			return 0, ErrSecurityRejected
		}
		if compressed == 0 {
			if n > 0 {
				return 0, ErrSecurityRejected
			}
		} else if expansionRatioExceeded(n, compressed, policy.MaxExpansionRatio) {
			return 0, ErrSecurityRejected
		}
		if budget.compressed > 0 && expansionRatioExceeded(budget.expanded, budget.compressed, policy.MaxExpansionRatio) {
			return 0, ErrSecurityRejected
		}
		if captureNested && n > 0 {
			if _, err := inspectZip(bytes.NewReader(nested.Bytes()), n, depth+1, policy, budget); err != nil {
				return 0, err
			}
		}
	}
	return localExpanded, nil
}

func expansionRatioExceeded(expanded, compressed, maxRatio int64) bool {
	if compressed <= 0 || expanded < 0 || maxRatio < 1 {
		return expanded > 0
	}
	// Avoid integer-division truncation at the policy boundary and avoid
	// multiplication overflow even if policy limits are widened later.
	return expanded/compressed > maxRatio || (expanded/compressed == maxRatio && expanded%compressed != 0)
}

func safeArchivePath(name string) bool {
	if name == "" || !utf8.ValidString(name) || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "\x00") {
		return false
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != strings.TrimSuffix(name, "/") {
		return false
	}
	if len(clean) >= 2 && clean[1] == ':' {
		return false
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == ".." || segment == "" {
			return false
		}
	}
	return true
}

func validateParser(obj QuarantinedObject, media string, policy Policy) error {
	if _, err := obj.Seek(0, io.SeekStart); err != nil {
		return err
	}
	defer obj.Seek(0, io.SeekStart)
	switch media {
	case "application/json":
		return validateJSON(obj, policy)
	case "application/xml", "text/xml":
		return validateXML(obj, policy)
	case "text/csv":
		return validateCSV(obj, policy)
	case "application/yaml", "text/yaml":
		return validateText(obj, policy)
	default:
		return nil
	}
}

func validateJSON(r io.Reader, policy Policy) error {
	dec := json.NewDecoder(io.LimitReader(r, policy.MaxFileBytes+1))
	dec.UseNumber()
	depth, tokens := 0, 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrSecurityRejected
		}
		tokens++
		if tokens > policy.MaxParserTokens {
			return ErrSecurityRejected
		}
		if d, ok := tok.(json.Delim); ok {
			if d == '{' || d == '[' {
				depth++
				if depth > policy.MaxParserDepth {
					return ErrSecurityRejected
				}
			}
			if d == '}' || d == ']' {
				depth--
				if depth < 0 {
					return ErrSecurityRejected
				}
			}
		}
	}
	if depth != 0 || tokens == 0 {
		return ErrSecurityRejected
	}
	return nil
}

func validateXML(r io.Reader, policy Policy) error {
	dec := xml.NewDecoder(io.LimitReader(r, policy.MaxFileBytes+1))
	dec.Strict = true
	depth, tokens := 0, 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrSecurityRejected
		}
		tokens++
		if tokens > policy.MaxParserTokens {
			return ErrSecurityRejected
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth > policy.MaxParserDepth || len(t.Attr) > 128 {
				return ErrSecurityRejected
			}
		case xml.EndElement:
			depth--
			if depth < 0 {
				return ErrSecurityRejected
			}
		case xml.Directive:
			upper := strings.ToUpper(string(t))
			if strings.Contains(upper, "DOCTYPE") || strings.Contains(upper, "ENTITY") {
				return ErrSecurityRejected
			}
		}
	}
	if depth != 0 || tokens == 0 {
		return ErrSecurityRejected
	}
	return nil
}

func validateCSV(r io.Reader, policy Policy) error {
	cr := csv.NewReader(io.LimitReader(r, policy.MaxFileBytes+1))
	cr.FieldsPerRecord = -1
	rows := 0
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrSecurityRejected
		}
		rows++
		if rows > policy.MaxCSVRows || len(rec) > policy.MaxCSVColumns {
			return ErrSecurityRejected
		}
		for _, field := range rec {
			if len(field) > policy.MaxFieldBytes || !utf8.ValidString(field) {
				return ErrSecurityRejected
			}
		}
	}
	if rows == 0 {
		return ErrSecurityRejected
	}
	return nil
}

func validateText(r io.Reader, policy Policy) error {
	s := bufio.NewScanner(io.LimitReader(r, policy.MaxFileBytes+1))
	s.Buffer(make([]byte, 64*1024), policy.MaxFieldBytes)
	lines := 0
	for s.Scan() {
		lines++
		if lines > policy.MaxCSVRows || !utf8.ValidString(s.Text()) {
			return ErrSecurityRejected
		}
	}
	if s.Err() != nil || lines == 0 {
		return ErrSecurityRejected
	}
	return nil
}
