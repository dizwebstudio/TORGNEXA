package releasecheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/tools/contractcheck/internal/licensepolicy"
)

const (
	maxManifestEntries = 1_000
	maxTextLength      = 4_096
)

var (
	commitRE      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repositoryRE  = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})/[A-Za-z0-9](?:[A-Za-z0-9._-]{0,99})$`)
	nameRE        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	platformRE    = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9]+$`)
	identifierRE  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
	blockerCodeRE = regexp.MustCompile(`^[a-z][a-z0-9_]{2,127}$`)
	runIDRE       = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
	imageRE       = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*(?::[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}$`)
)

type validator struct {
	ctx           context.Context
	mode          Mode
	now           time.Time
	bundle        *evidenceFS
	manifestPath  string
	manifest      manifestV1
	usedPaths     map[string]string
	subjects      map[string]subject
	runtimes      map[string]runtimeSubject
	licensePolicy []byte
}

func validate(ctx context.Context, options Options) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("context is required")
	}
	if _, err := ParseMode(string(options.Mode)); err != nil {
		return Result{}, err
	}
	manifestPath, err := safeRelativePath(options.ManifestPath)
	if err != nil {
		return Result{}, fmt.Errorf("manifest path: %w", err)
	}
	bundle, err := openEvidenceFS(ctx, options.Root)
	if err != nil {
		return Result{}, err
	}
	data, err := bundle.readBytes(ctx, manifestPath, maxManifestSize)
	if err != nil {
		return Result{}, err
	}
	var manifest manifestV1
	if err := decodeStrictJSON(ctx, data, &manifest); err != nil {
		return Result{}, fmt.Errorf("decode manifest %q: %w", manifestPath, err)
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	check := &validator{
		ctx:          ctx,
		mode:         options.Mode,
		now:          now,
		bundle:       bundle,
		manifestPath: manifestPath,
		manifest:     manifest,
		usedPaths:    map[string]string{manifestPath: "manifest"},
		subjects:     make(map[string]subject),
		runtimes:     make(map[string]runtimeSubject),
	}
	return check.run()
}

func (check *validator) run() (Result, error) {
	if check.manifest.Version != 1 {
		return Result{}, fmt.Errorf("manifest version must be 1")
	}
	if check.manifest.Release == nil {
		return Result{}, fmt.Errorf("manifest release is required")
	}
	for name, present := range map[string]bool{
		"subjects":         check.manifest.Subjects != nil,
		"runtime_subjects": check.manifest.RuntimeSubjects != nil,
		"sboms":            check.manifest.SBOMs != nil,
		"reports":          check.manifest.Reports != nil,
		"signatures":       check.manifest.Signatures != nil,
		"provenance":       check.manifest.Provenance != nil,
		"exceptions":       check.manifest.Exceptions != nil,
		"blockers":         check.manifest.Blockers != nil,
	} {
		if !present {
			return Result{}, fmt.Errorf("manifest field %q must be an array, not missing or null", name)
		}
	}
	if err := check.validateRelease(); err != nil {
		return Result{}, err
	}
	if err := check.validateSubjects(); err != nil {
		return Result{}, err
	}
	if err := check.validateRuntimeSubjects(); err != nil {
		return Result{}, err
	}
	if err := check.validateSBOMs(); err != nil {
		return Result{}, err
	}
	if err := check.validateDependencyLicensePolicy(); err != nil {
		return Result{}, err
	}
	if err := check.validateReports(); err != nil {
		return Result{}, err
	}
	if err := check.validateExceptions(); err != nil {
		return Result{}, err
	}
	if err := check.validateLicense(); err != nil {
		return Result{}, err
	}
	if err := check.validateSubjectEvidence("signature", check.manifest.Signatures, false); err != nil {
		return Result{}, err
	}
	if err := check.validateSubjectEvidence("provenance", check.manifest.Provenance, true); err != nil {
		return Result{}, err
	}
	if err := check.validateGitHubAttestation(); err != nil {
		return Result{}, err
	}
	if err := check.validateExternalVerification(); err != nil {
		return Result{}, err
	}
	blockers, err := check.validateBlockers()
	if err != nil {
		return Result{}, err
	}
	for relative := range check.bundle.files {
		if _, used := check.usedPaths[relative]; !used {
			return Result{}, fmt.Errorf("unbound evidence file %q is not referenced by the manifest", relative)
		}
	}
	externalStatus := "pending"
	if check.manifest.ExternalVerification.Status == "pass" {
		externalStatus = "declared-pass; external cryptographic result structurally bound, not re-executed"
	}
	return Result{
		Status:                       "pass",
		Mode:                         check.mode,
		SubjectCount:                 len(check.subjects),
		RuntimeSubjectCount:          len(check.runtimes),
		ReportCount:                  len(check.manifest.Reports),
		PublicReady:                  *check.manifest.Release.PublicReady,
		ExternalIdentityVerification: externalStatus,
		Blockers:                     blockers,
	}, nil
}

func (check *validator) validateDependencyLicensePolicy() error {
	reference := check.manifest.DependencyLicensePolicy
	if reference == nil {
		return fmt.Errorf("dependency_license_policy is required")
	}
	if reference.Path != "policy/license-policy.json" || reference.RepositoryPath != "supply-chain/license-policy.json" || reference.SourceCommit != check.manifest.Release.Commit {
		return fmt.Errorf("dependency license policy evidence must bind supply-chain/license-policy.json at the release commit")
	}
	if err := requireSHA256("dependency license policy", reference.SHA256); err != nil {
		return err
	}
	if err := check.registerPath("dependency license policy", reference.Path); err != nil {
		return err
	}
	data, err := check.bundle.readBytes(check.ctx, reference.Path, maxLicenseSize)
	if err != nil {
		return err
	}
	if digestBytes(data) != reference.SHA256 {
		return fmt.Errorf("dependency license policy file SHA-256 mismatch")
	}
	if err := licensepolicy.ValidatePolicy(data); err != nil {
		return fmt.Errorf("dependency license policy is invalid: %w", err)
	}
	check.licensePolicy = append([]byte(nil), data...)
	return nil
}

func (check *validator) validateGitHubAttestation() error {
	reference := check.manifest.GitHubAttestation
	if reference == nil {
		if check.mode == ModePublic {
			return fmt.Errorf("public mode requires a retained GitHub attestation bundle")
		}
		return nil
	}
	if err := requireSHA256("GitHub attestation", reference.SHA256); err != nil {
		return err
	}
	if !strings.HasSuffix(reference.Path, ".json") {
		return fmt.Errorf("GitHub attestation bundle must be JSON")
	}
	if err := check.registerPath("GitHub attestation", reference.Path); err != nil {
		return err
	}
	data, err := check.bundle.readBytes(check.ctx, reference.Path, maxJSONFileSize)
	if err != nil {
		return err
	}
	if digestBytes(data) != reference.SHA256 {
		return fmt.Errorf("GitHub attestation bundle SHA-256 mismatch")
	}
	value, err := decodeJSONValue(check.ctx, data)
	if err != nil || !nonemptyJSON(value) {
		return fmt.Errorf("GitHub attestation bundle must be non-empty strict JSON")
	}
	return nil
}

func (check *validator) validateRelease() error {
	release := check.manifest.Release
	if !validSemver(release.Version) {
		return fmt.Errorf("release.version %q is not a canonical v-prefixed semantic version", release.Version)
	}
	if !commitRE.MatchString(release.Commit) {
		return fmt.Errorf("release.commit must be a lowercase 40-hex Git commit")
	}
	if !repositoryRE.MatchString(release.Repository) || strings.Contains(release.Repository, "..") || strings.HasSuffix(release.Repository, ".git") {
		return fmt.Errorf("release.repository must be a canonical owner/name")
	}
	if release.Ref != "refs/tags/"+release.Version {
		return fmt.Errorf("release.ref must equal %q", "refs/tags/"+release.Version)
	}
	workflow, err := safeRelativePath(release.Workflow)
	if err != nil || !strings.HasPrefix(workflow, ".github/workflows/") || !(strings.HasSuffix(workflow, ".yml") || strings.HasSuffix(workflow, ".yaml")) {
		return fmt.Errorf("release.workflow must be a safe .github/workflows YAML path")
	}
	if release.PublicReady == nil {
		return fmt.Errorf("release.public_ready must be explicitly true or false")
	}
	if check.mode == ModeDryRun && *release.PublicReady {
		return fmt.Errorf("dry-run mode requires release.public_ready=false")
	}
	if check.mode == ModePublic && !*release.PublicReady {
		return fmt.Errorf("public mode requires release.public_ready=true")
	}
	if !runIDRE.MatchString(release.WorkflowRunID) {
		return fmt.Errorf("release.workflow_run_id must be a positive decimal run ID")
	}
	runURL, err := url.Parse(release.WorkflowRunURL)
	expectedPath := "/" + release.Repository + "/actions/runs/" + release.WorkflowRunID
	if err != nil || runURL.Scheme != "https" || runURL.Host != "github.com" || runURL.Path != expectedPath || runURL.RawQuery != "" || runURL.Fragment != "" || runURL.User != nil {
		return fmt.Errorf("release.workflow_run_url must identify the exact github.com repository and run ID")
	}
	return nil
}

func (check *validator) validateSubjects() error {
	if len(check.manifest.Subjects) == 0 || len(check.manifest.Subjects) > maxManifestEntries {
		return fmt.Errorf("subjects must contain between 1 and %d entries", maxManifestEntries)
	}
	for _, item := range check.manifest.Subjects {
		if !nameRE.MatchString(item.Name) {
			return fmt.Errorf("subject name %q is invalid", item.Name)
		}
		if _, duplicate := check.subjects[item.Name]; duplicate {
			return fmt.Errorf("duplicate subject %q", item.Name)
		}
		if item.Type != "binary" {
			return fmt.Errorf("subject %q type must be binary", item.Name)
		}
		if !platformRE.MatchString(item.Platform) {
			return fmt.Errorf("subject %q platform must be os/arch", item.Name)
		}
		if err := requireSHA256("subject "+item.Name, item.SHA256); err != nil {
			return err
		}
		if err := check.registerPath("subject "+item.Name, item.Path); err != nil {
			return err
		}
		actual, err := check.bundle.hashFile(check.ctx, item.Path, maxArtifactSize)
		if err != nil {
			return err
		}
		if actual != item.SHA256 {
			return fmt.Errorf("subject %q SHA-256 mismatch", item.Name)
		}
		check.subjects[item.Name] = item
	}
	return nil
}

func (check *validator) validateRuntimeSubjects() error {
	if len(check.manifest.RuntimeSubjects) == 0 || len(check.manifest.RuntimeSubjects) > maxManifestEntries {
		return fmt.Errorf("runtime_subjects must contain development runtime image/platform entries")
	}
	platformsByName := make(map[string]map[string]struct{})
	imagesByName := make(map[string]string)
	for _, item := range check.manifest.RuntimeSubjects {
		if !nameRE.MatchString(item.Name) {
			return fmt.Errorf("runtime subject name %q is invalid", item.Name)
		}
		if item.Platform != "linux/amd64" && item.Platform != "linux/arm64" {
			return fmt.Errorf("runtime subject %q platform must be linux/amd64 or linux/arm64", item.Name)
		}
		if !imageRE.MatchString(item.Image) {
			return fmt.Errorf("runtime subject %q image must be digest-pinned", item.Name)
		}
		key := runtimeSubjectKey(item)
		if _, duplicate := check.runtimes[key]; duplicate {
			return fmt.Errorf("duplicate runtime subject %q", key)
		}
		if previous, exists := imagesByName[item.Name]; exists && previous != item.Image {
			return fmt.Errorf("runtime subject %q platforms must use one image digest", item.Name)
		}
		imagesByName[item.Name] = item.Image
		if platformsByName[item.Name] == nil {
			platformsByName[item.Name] = make(map[string]struct{})
		}
		platformsByName[item.Name][item.Platform] = struct{}{}
		check.runtimes[key] = item
	}
	for name, platforms := range platformsByName {
		if len(platforms) != 2 {
			return fmt.Errorf("runtime subject %q must cover both linux/amd64 and linux/arm64", name)
		}
	}
	return nil
}

func (check *validator) validateSBOMs() error {
	expectedCount := len(check.subjects) + len(check.runtimes)
	if len(check.manifest.SBOMs) != expectedCount {
		return fmt.Errorf("exactly one SBOM is required for each binary and runtime subject")
	}
	seen := make(map[string]struct{}, len(check.manifest.SBOMs))
	for _, reference := range check.manifest.SBOMs {
		binary, binaryExists := check.subjects[reference.Subject]
		runtime, runtimeExists := check.runtimes[reference.RuntimeSubject]
		if binaryExists == runtimeExists {
			return fmt.Errorf("SBOM must reference exactly one known binary or runtime subject")
		}
		key := "binary:" + reference.Subject
		label := reference.Subject
		if runtimeExists {
			key = "runtime:" + reference.RuntimeSubject
			label = reference.RuntimeSubject
			if reference.Subject != "" || reference.SubjectSHA256 != "" || reference.RuntimeImage != runtime.Image {
				return fmt.Errorf("runtime SBOM %q must bind its exact immutable image", label)
			}
		} else if reference.RuntimeSubject != "" || reference.RuntimeImage != "" || reference.SubjectSHA256 != binary.SHA256 {
			return fmt.Errorf("SBOM subject digest does not match subject %q", label)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate SBOM for subject %q", label)
		}
		seen[key] = struct{}{}
		if reference.Format != "SPDX-2.3-json" {
			return fmt.Errorf("SBOM for subject %q format must be SPDX-2.3-json", label)
		}
		if err := requireSHA256("SBOM "+label, reference.SHA256); err != nil {
			return err
		}
		if !strings.HasSuffix(reference.Path, ".json") {
			return fmt.Errorf("SBOM for subject %q must be a JSON file", label)
		}
		if err := check.registerPath("SBOM "+label, reference.Path); err != nil {
			return err
		}
		data, err := check.bundle.readBytes(check.ctx, reference.Path, maxJSONFileSize)
		if err != nil {
			return err
		}
		if digestBytes(data) != reference.SHA256 {
			return fmt.Errorf("SBOM for subject %q file SHA-256 mismatch", label)
		}
		if runtimeExists {
			if err := validateRuntimeSPDX(check.ctx, data, runtime, check.now); err != nil {
				return fmt.Errorf("SBOM for runtime subject %q: %w", label, err)
			}
		} else if err := validateSPDX(check.ctx, data, binary, check.now); err != nil {
			return fmt.Errorf("SBOM for subject %q: %w", label, err)
		}
	}
	return nil
}

func (check *validator) validateReports() error {
	required := make(map[string]bool, 4+len(check.runtimes))
	for _, kind := range []string{"secret", "sast", "dependency", "license"} {
		required[reportKey(kind, "source")] = false
	}
	for runtime := range check.runtimes {
		required[reportKey("container", runtime)] = false
	}
	if len(check.manifest.Reports) != len(required) {
		return fmt.Errorf("reports must contain one source report per required kind and one container report per runtime subject")
	}
	for _, report := range check.manifest.Reports {
		key := reportKey(report.Kind, report.Subject)
		if _, known := required[key]; !known {
			return fmt.Errorf("unexpected report kind/subject %q/%q", report.Kind, report.Subject)
		}
		if required[key] {
			return fmt.Errorf("duplicate report kind/subject %q/%q", report.Kind, report.Subject)
		}
		required[key] = true
		if report.Status != "pass" {
			return fmt.Errorf("report %q status must be pass", report.Kind)
		}
		if report.Sanitized == nil || !*report.Sanitized {
			return fmt.Errorf("report %q must explicitly be sanitized", report.Kind)
		}
		if err := requireText("report tool", report.Tool, 1, 128); err != nil {
			return err
		}
		if err := requireText("report tool_version", report.ToolVersion, 1, 128); err != nil {
			return err
		}
		if strings.EqualFold(report.ToolVersion, "latest") {
			return fmt.Errorf("report %q tool_version must be immutable", report.Kind)
		}
		if err := check.validateDatabase(report); err != nil {
			return err
		}
		if err := requireSHA256("report "+report.Kind, report.SHA256); err != nil {
			return err
		}
		if err := check.registerPath("report "+report.Kind, report.Path); err != nil {
			return err
		}
		data, err := check.bundle.readBytes(check.ctx, report.Path, maxJSONFileSize)
		if err != nil {
			return err
		}
		if digestBytes(data) != report.SHA256 {
			return fmt.Errorf("report %q file SHA-256 mismatch", report.Kind)
		}
		if err := validateReportPayload(check.ctx, report.Kind, data, check.licensePolicy, check.now); err != nil {
			return fmt.Errorf("report %q/%q is invalid: %w", report.Kind, report.Subject, err)
		}
	}
	return nil
}

func (check *validator) validateDatabase(report reportReference) error {
	if report.Database == nil {
		return fmt.Errorf("report %q/%q must declare database identity or explicit not_applicable", report.Kind, report.Subject)
	}
	database := report.Database
	requiresDatabase := report.Kind == "dependency" || report.Kind == "container"
	if requiresDatabase {
		if database.Status != "present" {
			return fmt.Errorf("report %q/%q requires vulnerability database identity", report.Kind, report.Subject)
		}
		parsed, err := url.Parse(database.URI)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("report %q/%q database URI must be fixed HTTPS without credentials, query, or fragment", report.Kind, report.Subject)
		}
		updated, err := parseUTCTime("report database updated_at", database.UpdatedAt)
		if err != nil {
			return fmt.Errorf("report %q/%q: %w", report.Kind, report.Subject, err)
		}
		if updated.After(check.now.Add(5*time.Minute)) || check.now.Sub(updated) > 48*time.Hour {
			return fmt.Errorf("report %q/%q vulnerability database is stale or from the future", report.Kind, report.Subject)
		}
		if err := requireSHA256("report database", database.Digest); err != nil {
			return fmt.Errorf("report %q/%q: %w", report.Kind, report.Subject, err)
		}
		if database.Reason != "" {
			return fmt.Errorf("report %q/%q present database must not include an N/A reason", report.Kind, report.Subject)
		}
		return nil
	}
	if database.Status != "not_applicable" {
		return fmt.Errorf("report %q/%q must explicitly mark database not_applicable", report.Kind, report.Subject)
	}
	if database.URI != "" || database.UpdatedAt != "" || database.Digest != "" {
		return fmt.Errorf("report %q/%q not_applicable database must not claim identity fields", report.Kind, report.Subject)
	}
	if err := requireText("report database N/A reason", database.Reason, 5, 256); err != nil {
		return fmt.Errorf("report %q/%q: %w", report.Kind, report.Subject, err)
	}
	return nil
}

func (check *validator) validateExceptions() error {
	if len(check.manifest.Exceptions) > maxManifestEntries {
		return fmt.Errorf("exceptions exceed %d entries", maxManifestEntries)
	}
	allowedKinds := map[string]bool{"sast": true, "dependency": true, "license": true, "container": true}
	ids := make(map[string]struct{}, len(check.manifest.Exceptions))
	scopes := make(map[string]struct{}, len(check.manifest.Exceptions))
	for _, exception := range check.manifest.Exceptions {
		if !identifierRE.MatchString(exception.ID) {
			return fmt.Errorf("exception ID %q is invalid", exception.ID)
		}
		if _, duplicate := ids[exception.ID]; duplicate {
			return fmt.Errorf("duplicate exception ID %q", exception.ID)
		}
		ids[exception.ID] = struct{}{}
		if !allowedKinds[exception.Kind] {
			return fmt.Errorf("exception %q kind must be sast, dependency, license, or container", exception.ID)
		}
		if !identifierRE.MatchString(exception.Finding) || hasWildcard(exception.Finding) {
			return fmt.Errorf("exception %q finding must be exact and contain no wildcard", exception.ID)
		}
		if !exactScope(exception.Scope) {
			return fmt.Errorf("exception %q scope must bind an exact component version or digest", exception.ID)
		}
		key := exception.Kind + "\x00" + exception.Finding + "\x00" + exception.Scope
		if _, duplicate := scopes[key]; duplicate {
			return fmt.Errorf("duplicate exception scope for %q", exception.ID)
		}
		scopes[key] = struct{}{}
		if err := requireText("exception justification", exception.Justification, 10, 1_000); err != nil {
			return fmt.Errorf("exception %q: %w", exception.ID, err)
		}
		for label, value := range map[string]string{"ticket": exception.Ticket, "approver": exception.Approver} {
			if err := requireText("exception "+label, value, 1, 256); err != nil {
				return fmt.Errorf("exception %q: %w", exception.ID, err)
			}
			if hasWildcard(value) {
				return fmt.Errorf("exception %q %s contains a wildcard", exception.ID, label)
			}
		}
		approvedAt, err := parseUTCTime("exception approved_at", exception.ApprovedAt)
		if err != nil {
			return fmt.Errorf("exception %q: %w", exception.ID, err)
		}
		expiresAt, err := parseUTCTime("exception expires_at", exception.ExpiresAt)
		if err != nil {
			return fmt.Errorf("exception %q: %w", exception.ID, err)
		}
		if approvedAt.After(check.now) {
			return fmt.Errorf("exception %q approval is in the future", exception.ID)
		}
		if !expiresAt.After(check.now) {
			return fmt.Errorf("exception %q is expired", exception.ID)
		}
		if !expiresAt.After(approvedAt) {
			return fmt.Errorf("exception %q expires before approval", exception.ID)
		}
	}
	return nil
}

func (check *validator) validateLicense() error {
	if check.manifest.License == nil {
		if check.mode == ModePublic {
			return fmt.Errorf("public mode requires repository LICENSE evidence")
		}
		return nil
	}
	license := check.manifest.License
	if license.Path != "repository/LICENSE" || license.RepositoryPath != "LICENSE" || license.SourceCommit != check.manifest.Release.Commit {
		return fmt.Errorf("repository license evidence must bind top-level LICENSE at the release commit")
	}
	if err := requireSHA256("LICENSE", license.SHA256); err != nil {
		return err
	}
	if err := check.registerPath("LICENSE", license.Path); err != nil {
		return err
	}
	data, err := check.bundle.readBytes(check.ctx, license.Path, maxLicenseSize)
	if err != nil {
		return err
	}
	if digestBytes(data) != license.SHA256 {
		return fmt.Errorf("LICENSE file SHA-256 mismatch")
	}
	if !utf8.Valid(data) || len(bytes.TrimSpace(data)) < 20 {
		return fmt.Errorf("LICENSE must be non-empty UTF-8 license text")
	}
	lower := strings.ToLower(string(data))
	for _, marker := range []string{"license decision required", "not yet approved", "unresolved license", "todo: choose license", "tbd license"} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("LICENSE still contains unresolved decision marker")
		}
	}
	return nil
}

func (check *validator) validateSubjectEvidence(kind string, entries []subjectEvidence, provenance bool) error {
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		item, exists := check.subjects[entry.Subject]
		if !exists {
			return fmt.Errorf("%s references unknown subject %q", kind, entry.Subject)
		}
		if _, duplicate := seen[entry.Subject]; duplicate {
			return fmt.Errorf("duplicate %s for subject %q", kind, entry.Subject)
		}
		seen[entry.Subject] = struct{}{}
		if entry.SubjectSHA256 != item.SHA256 {
			return fmt.Errorf("%s subject digest does not match subject %q", kind, item.Name)
		}
		if err := requireSHA256(kind+" "+item.Name, entry.SHA256); err != nil {
			return err
		}
		if err := check.registerPath(kind+" "+item.Name, entry.Path); err != nil {
			return err
		}
		data, err := check.bundle.readBytes(check.ctx, entry.Path, maxJSONFileSize)
		if err != nil {
			return err
		}
		if digestBytes(data) != entry.SHA256 {
			return fmt.Errorf("%s for subject %q file SHA-256 mismatch", kind, item.Name)
		}
		if provenance {
			if err := validateProvenance(check.ctx, data, item, *check.manifest.Release); err != nil {
				return fmt.Errorf("provenance for subject %q: %w", item.Name, err)
			}
		} else {
			value, err := decodeJSONValue(check.ctx, data)
			if err != nil || !nonemptyJSON(value) {
				return fmt.Errorf("signature bundle for subject %q must be non-empty strict JSON", item.Name)
			}
		}
	}
	if check.mode == ModePublic && len(seen) != len(check.subjects) {
		return fmt.Errorf("public mode requires exactly one %s for every first-party subject", kind)
	}
	return nil
}

func (check *validator) validateExternalVerification() error {
	verification := check.manifest.ExternalVerification
	if verification == nil {
		return fmt.Errorf("external_identity_verification is required")
	}
	if verification.Status == "pending" {
		if check.mode == ModePublic {
			return fmt.Errorf("public mode requires external identity verification status pass")
		}
		if verification.Tool != "" || verification.ToolVersion != "" || verification.Path != "" || verification.SHA256 != "" {
			return fmt.Errorf("pending external identity verification must not claim tool evidence")
		}
		return nil
	}
	if verification.Status != "pass" {
		return fmt.Errorf("external identity verification status must be pending or pass")
	}
	if verification.Tool != "cosign" {
		return fmt.Errorf("external verification tool must be cosign")
	}
	if verification.ToolVersion != "v3.1.3" {
		return fmt.Errorf("external verification tool_version must be exactly v3.1.3")
	}
	if err := requireSHA256("external verification", verification.SHA256); err != nil {
		return err
	}
	if err := check.registerPath("external identity verification", verification.Path); err != nil {
		return err
	}
	data, err := check.bundle.readBytes(check.ctx, verification.Path, maxJSONFileSize)
	if err != nil {
		return err
	}
	if digestBytes(data) != verification.SHA256 {
		return fmt.Errorf("external identity verification file SHA-256 mismatch")
	}
	var record externalVerificationRecord
	if err := decodeStrictJSON(check.ctx, data, &record); err != nil {
		return fmt.Errorf("external identity verification evidence must be strict JSON: %w", err)
	}
	return check.validateExternalVerificationRecord(record)
}

func (check *validator) validateExternalVerificationRecord(record externalVerificationRecord) error {
	release := check.manifest.Release
	if record.Version != 1 || record.Status != "pass" {
		return fmt.Errorf("external identity verification record must declare version 1 and pass status")
	}
	if record.Repository != release.Repository || record.SourceRevision != release.Commit || record.Ref != release.Ref || record.Workflow != release.Workflow {
		return fmt.Errorf("external identity verification record does not match release identity")
	}
	if record.EventName != "push" && (check.mode == ModePublic || record.EventName != "workflow_dispatch") {
		return fmt.Errorf("external identity verification event is invalid for %s mode", check.mode)
	}
	if record.Issuer != "https://token.actions.githubusercontent.com" {
		return fmt.Errorf("external identity verification issuer is invalid")
	}
	expectedIdentity := "https://github.com/" + release.Repository + "/" + release.Workflow + "@" + release.Ref
	if record.CertificateIdentity != expectedIdentity {
		return fmt.Errorf("external identity verification certificate identity is invalid")
	}
	if check.manifest.GitHubAttestation == nil || record.GitHubAttestationSHA256 != check.manifest.GitHubAttestation.SHA256 {
		return fmt.Errorf("external identity verification does not bind the retained GitHub attestation")
	}
	if len(record.Subjects) != len(check.subjects) {
		return fmt.Errorf("external identity verification must cover every first-party subject")
	}
	signatures := make(map[string]subjectEvidence, len(check.manifest.Signatures))
	for _, signature := range check.manifest.Signatures {
		signatures[signature.Subject] = signature
	}
	previous := ""
	seen := make(map[string]struct{}, len(record.Subjects))
	for _, verified := range record.Subjects {
		if previous != "" && verified.Name <= previous {
			return fmt.Errorf("external identity verification subjects must be strictly sorted and unique")
		}
		previous = verified.Name
		item, exists := check.subjects[verified.Name]
		if !exists || verified.SHA256 != item.SHA256 {
			return fmt.Errorf("external identity verification subject %q does not match retained bytes", verified.Name)
		}
		signature, exists := signatures[verified.Name]
		if !exists || verified.SignatureBundleSHA256 != signature.SHA256 {
			return fmt.Errorf("external identity verification subject %q does not bind its signature bundle", verified.Name)
		}
		if verified.SignatureVerified == nil || !*verified.SignatureVerified || verified.GitHubAttestationVerified == nil || !*verified.GitHubAttestationVerified {
			return fmt.Errorf("external identity verification subject %q is not fully verified", verified.Name)
		}
		seen[verified.Name] = struct{}{}
	}
	if len(seen) != len(check.subjects) {
		return fmt.Errorf("external identity verification has incomplete subject coverage")
	}
	return nil
}

func (check *validator) validateBlockers() ([]string, error) {
	codes := make(map[string]struct{}, len(check.manifest.Blockers))
	for _, item := range check.manifest.Blockers {
		if !blockerCodeRE.MatchString(item.Code) {
			return nil, fmt.Errorf("blocker code %q is invalid", item.Code)
		}
		if _, duplicate := codes[item.Code]; duplicate {
			return nil, fmt.Errorf("duplicate blocker code %q", item.Code)
		}
		codes[item.Code] = struct{}{}
		if err := requireText("blocker detail", item.Detail, 1, 1_000); err != nil {
			return nil, fmt.Errorf("blocker %q: %w", item.Code, err)
		}
	}
	if check.mode == ModePublic {
		if len(codes) != 0 {
			return nil, fmt.Errorf("public mode does not permit unresolved blockers")
		}
		return []string{}, nil
	}
	required := []string{"public_release_not_ready"}
	if check.manifest.ExternalVerification.Status != "pass" {
		required = append(required, "external_identity_verification_pending")
	}
	if check.manifest.License == nil {
		required = append(required, "repository_license_unresolved")
	}
	for _, code := range required {
		if _, exists := codes[code]; !exists {
			return nil, fmt.Errorf("dry-run manifest must record blocker %q", code)
		}
	}
	result := make([]string, 0, len(codes))
	for code := range codes {
		result = append(result, code)
	}
	sort.Strings(result)
	return result, nil
}

func (check *validator) registerPath(label, relative string) error {
	cleaned, err := safeRelativePath(relative)
	if err != nil {
		return fmt.Errorf("%s path %q: %w", label, relative, err)
	}
	if previous, duplicate := check.usedPaths[cleaned]; duplicate {
		return fmt.Errorf("evidence path %q is reused by %s and %s", cleaned, previous, label)
	}
	check.usedPaths[cleaned] = label
	return nil
}

func requireSHA256(label, value string) error {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return fmt.Errorf("%s SHA-256 must be 64 lowercase hex characters", label)
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("%s SHA-256 must be 64 lowercase hex characters", label)
	}
	return nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func requireText(label, value string, minimum, maximum int) error {
	if value != strings.TrimSpace(value) || len(value) < minimum || len(value) > maximum || len(value) > maxTextLength {
		return fmt.Errorf("%s must contain %d..%d trimmed bytes", label, minimum, maximum)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s is not valid UTF-8", label)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains a control character", label)
		}
	}
	return nil
}

func parseUTCTime(label, value string) (time.Time, error) {
	if !strings.HasSuffix(value, "Z") {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 UTC timestamp", label)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.Location() != time.UTC {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 UTC timestamp", label)
	}
	return parsed, nil
}

func hasWildcard(value string) bool {
	return strings.ContainsAny(value, "*?[]{}") || strings.Contains(value, "${{") || strings.EqualFold(value, "all")
}

func exactScope(value string) bool {
	if err := requireText("exception scope", value, 3, 512); err != nil || hasWildcard(value) || strings.Count(value, "@") != 1 {
		return false
	}
	parts := strings.SplitN(value, "@", 2)
	return parts[0] != "" && parts[1] != "" && !strings.EqualFold(parts[1], "latest")
}

func runtimeSubjectKey(item runtimeSubject) string {
	return item.Name + "@" + item.Platform
}

func reportKey(kind, subject string) string {
	return kind + "\x00" + subject
}

func nonemptyJSON(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	default:
		return false
	}
}

func decodePermissiveJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validSemver(value string) bool {
	if !strings.HasPrefix(value, "v") || len(value) > 128 {
		return false
	}
	version := value[1:]
	coreAndPrerelease := version
	if index := strings.IndexByte(version, '+'); index >= 0 {
		if !validSemverIdentifiers(version[index+1:], false) {
			return false
		}
		coreAndPrerelease = version[:index]
		if strings.Contains(version[index+1:], "+") {
			return false
		}
	}
	core := coreAndPrerelease
	if index := strings.IndexByte(coreAndPrerelease, '-'); index >= 0 {
		if !validSemverIdentifiers(coreAndPrerelease[index+1:], true) {
			return false
		}
		core = coreAndPrerelease[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if !validSemverNumber(part) {
			return false
		}
	}
	return true
}

func validSemverIdentifiers(value string, rejectNumericLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		numeric := true
		for _, r := range identifier {
			if !(r >= '0' && r <= '9') {
				numeric = false
			}
			if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '-') {
				return false
			}
		}
		if rejectNumericLeadingZero && numeric && len(identifier) > 1 && identifier[0] == '0' {
			return false
		}
	}
	return true
}

func validSemverNumber(value string) bool {
	if value == "" || (len(value) > 1 && value[0] == '0') {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
