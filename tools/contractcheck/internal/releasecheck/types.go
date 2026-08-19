// Package releasecheck validates a self-contained release evidence bundle.
//
// It verifies structure and content digests only. Cryptographic signature and
// OIDC identity verification must be performed by an external trusted tool.
package releasecheck

import (
	"context"
	"fmt"
	"time"
)

// Mode selects the evidence policy applied by Validate.
type Mode string

const (
	// ModeDryRun validates a deliberately non-public evidence rehearsal.
	ModeDryRun Mode = "dry-run"
	// ModePublic validates the structural prerequisites for public evidence.
	ModePublic Mode = "public"
)

// ParseMode parses a CLI mode without accepting aliases or case folding.
func ParseMode(value string) (Mode, error) {
	mode := Mode(value)
	switch mode {
	case ModeDryRun, ModePublic:
		return mode, nil
	default:
		return "", fmt.Errorf("mode must be %q or %q", ModeDryRun, ModePublic)
	}
}

// Options configures evidence validation.
type Options struct {
	Root         string
	ManifestPath string
	Mode         Mode
	// Now is optional and exists for deterministic expiry validation in tests.
	// A zero value uses the current UTC time.
	Now time.Time
}

// Result is safe machine-readable validation output. Status describes
// structural and digest validation, not cryptographic identity verification.
type Result struct {
	Status                       string   `json:"status"`
	Mode                         Mode     `json:"mode"`
	SubjectCount                 int      `json:"subject_count"`
	RuntimeSubjectCount          int      `json:"runtime_subject_count"`
	ReportCount                  int      `json:"report_count"`
	PublicReady                  bool     `json:"public_ready"`
	ExternalIdentityVerification string   `json:"external_identity_verification"`
	Blockers                     []string `json:"blockers"`
}

// Validate validates one evidence manifest and every referenced file beneath
// its evidence root. It never performs network access.
func Validate(ctx context.Context, options Options) (Result, error) {
	return validate(ctx, options)
}

type manifestV1 struct {
	Version                 int                   `json:"version"`
	Release                 *releaseMetadata      `json:"release"`
	Subjects                []subject             `json:"subjects"`
	RuntimeSubjects         []runtimeSubject      `json:"runtime_subjects"`
	SBOMs                   []sbomReference       `json:"sboms"`
	Reports                 []reportReference     `json:"reports"`
	Signatures              []subjectEvidence     `json:"signatures"`
	Provenance              []subjectEvidence     `json:"provenance"`
	GitHubAttestation       *digestEvidence       `json:"github_attestation"`
	Exceptions              []riskException       `json:"exceptions"`
	DependencyLicensePolicy *fileEvidence         `json:"dependency_license_policy"`
	License                 *fileEvidence         `json:"license"`
	ExternalVerification    *externalVerification `json:"external_identity_verification"`
	Blockers                []blocker             `json:"blockers"`
}

type releaseMetadata struct {
	Version        string `json:"version"`
	Commit         string `json:"commit"`
	Repository     string `json:"repository"`
	Ref            string `json:"ref"`
	Workflow       string `json:"workflow"`
	WorkflowRunID  string `json:"workflow_run_id"`
	WorkflowRunURL string `json:"workflow_run_url"`
	PublicReady    *bool  `json:"public_ready"`
}

type subject struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Platform string `json:"platform"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
}

type runtimeSubject struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Image    string `json:"image"`
}

type sbomReference struct {
	Subject        string `json:"subject"`
	RuntimeSubject string `json:"runtime_subject"`
	RuntimeImage   string `json:"runtime_image"`
	Format         string `json:"format"`
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	SubjectSHA256  string `json:"subject_sha256"`
}

type reportReference struct {
	Kind        string            `json:"kind"`
	Subject     string            `json:"subject"`
	Status      string            `json:"status"`
	Tool        string            `json:"tool"`
	ToolVersion string            `json:"tool_version"`
	Path        string            `json:"path"`
	SHA256      string            `json:"sha256"`
	Sanitized   *bool             `json:"sanitized"`
	Database    *databaseIdentity `json:"database"`
}

type databaseIdentity struct {
	Status    string `json:"status"`
	URI       string `json:"uri"`
	UpdatedAt string `json:"updated_at"`
	Digest    string `json:"digest"`
	Reason    string `json:"reason"`
}

type subjectEvidence struct {
	Subject       string `json:"subject"`
	Path          string `json:"path"`
	SHA256        string `json:"sha256"`
	SubjectSHA256 string `json:"subject_sha256"`
}

type digestEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type fileEvidence struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	RepositoryPath string `json:"repository_path"`
	SourceCommit   string `json:"source_commit"`
}

type externalVerification struct {
	Status      string `json:"status"`
	Tool        string `json:"tool"`
	ToolVersion string `json:"tool_version"`
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
}

type externalVerificationRecord struct {
	Version                 int                       `json:"version"`
	Status                  string                    `json:"status"`
	Repository              string                    `json:"repository"`
	SourceRevision          string                    `json:"source_revision"`
	Ref                     string                    `json:"ref"`
	Workflow                string                    `json:"workflow"`
	EventName               string                    `json:"event_name"`
	Issuer                  string                    `json:"issuer"`
	CertificateIdentity     string                    `json:"certificate_identity"`
	GitHubAttestationSHA256 string                    `json:"github_attestation_sha256"`
	Subjects                []externalVerifiedSubject `json:"subjects"`
}

type externalVerifiedSubject struct {
	Name                      string `json:"name"`
	SHA256                    string `json:"sha256"`
	SignatureBundleSHA256     string `json:"signature_bundle_sha256"`
	SignatureVerified         *bool  `json:"signature_verified"`
	GitHubAttestationVerified *bool  `json:"github_attestation_verified"`
}

type riskException struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"`
	Finding       string `json:"finding"`
	Scope         string `json:"scope"`
	Justification string `json:"justification"`
	Ticket        string `json:"ticket"`
	Approver      string `json:"approver"`
	ApprovedAt    string `json:"approved_at"`
	ExpiresAt     string `json:"expires_at"`
}

type blocker struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
}
