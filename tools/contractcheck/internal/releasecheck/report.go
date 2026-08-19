package releasecheck

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/tools/contractcheck/internal/licensepolicy"
)

type scannerEnvelope struct {
	Version int               `json:"version"`
	Reports []json.RawMessage `json:"reports"`
}

type trivyReport struct {
	SchemaVersion int           `json:"SchemaVersion"`
	ArtifactType  string        `json:"ArtifactType"`
	Results       []trivyResult `json:"Results"`
}

type trivyResult struct {
	Vulnerabilities   []trivyFinding `json:"Vulnerabilities"`
	Misconfigurations []trivyFinding `json:"Misconfigurations"`
	Secrets           []trivyFinding `json:"Secrets"`
	Licenses          []trivyFinding `json:"Licenses"`
}

type trivyFinding struct {
	VulnerabilityID string `json:"VulnerabilityID"`
	ID              string `json:"ID"`
	RuleID          string `json:"RuleID"`
	Name            string `json:"Name"`
	Severity        string `json:"Severity"`
}

type gosecReport struct {
	Errors map[string][]json.RawMessage `json:"Golang errors"`
	Issues []gosecIssue                 `json:"Issues"`
	Stats  gosecStats                   `json:"Stats"`
}

type gosecIssue struct {
	Severity string `json:"severity"`
	RuleID   string `json:"rule_id"`
}

type gosecStats struct {
	Found int `json:"found"`
}

type govulnMessage struct {
	Config  *govulnConfig  `json:"config"`
	Finding *govulnFinding `json:"finding"`
}

type govulnConfig struct {
	ProtocolVersion string `json:"protocol_version"`
	ScannerName     string `json:"scanner_name"`
	ScannerVersion  string `json:"scanner_version"`
	DB              string `json:"db"`
	DBLastModified  string `json:"db_last_modified"`
	ScanLevel       string `json:"scan_level"`
	ScanMode        string `json:"scan_mode"`
}

type govulnFinding struct {
	OSV   string        `json:"osv"`
	Trace []govulnFrame `json:"trace"`
}

type govulnFrame struct {
	Module   string `json:"module"`
	Package  string `json:"package"`
	Function string `json:"function"`
}

func validateReportPayload(ctx context.Context, kind string, data, licensePolicy []byte, now time.Time) error {
	if _, err := decodeJSONValue(ctx, data); err != nil {
		return err
	}
	switch kind {
	case "secret":
		return validateTrivy(data, "secret", false)
	case "license":
		if err := validateTrivy(data, "license", false); err != nil {
			return err
		}
		if _, err := licensepolicy.Check(licensePolicy, data); err != nil {
			return fmt.Errorf("dependency license policy: %w", err)
		}
		return nil
	case "sast":
		reports, err := decodeEnvelope(data, 2)
		if err != nil {
			return err
		}
		for index, report := range reports {
			if err := validateGosec(report); err != nil {
				return fmt.Errorf("gosec report %d: %w", index+1, err)
			}
		}
		return nil
	case "dependency":
		reports, err := decodeEnvelope(data, 3)
		if err != nil {
			return err
		}
		for index := 0; index < 2; index++ {
			if err := validateGovuln(reports[index], now); err != nil {
				return fmt.Errorf("govulncheck report %d: %w", index+1, err)
			}
		}
		return validateTrivy(reports[2], "vulnerability", false)
	case "container":
		reports, err := decodeEnvelope(data, 3)
		if err != nil {
			return err
		}
		if err := validateTrivy(reports[0], "vulnerability", true); err != nil {
			return fmt.Errorf("container vulnerability report: %w", err)
		}
		if err := validateTrivy(reports[1], "license", true); err != nil {
			return fmt.Errorf("container license report: %w", err)
		}
		if _, err := licensepolicy.Check(licensePolicy, reports[1]); err != nil {
			return fmt.Errorf("container license policy: %w", err)
		}
		if err := validateTrivy(reports[2], "secret", true); err != nil {
			return fmt.Errorf("container secret report: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported report kind %q", kind)
	}
}

func decodeEnvelope(data []byte, expected int) ([]json.RawMessage, error) {
	var envelope scannerEnvelope
	if err := decodePermissiveJSON(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode scanner envelope: %w", err)
	}
	if envelope.Version != 1 || len(envelope.Reports) != expected {
		return nil, fmt.Errorf("scanner envelope must have version 1 and exactly %d reports", expected)
	}
	return envelope.Reports, nil
}

func validateTrivy(data []byte, scanner string, container bool) error {
	var report trivyReport
	if err := decodePermissiveJSON(data, &report); err != nil {
		return fmt.Errorf("decode Trivy report: %w", err)
	}
	if report.SchemaVersion != 2 || report.Results == nil {
		return fmt.Errorf("Trivy report must use schema version 2 with a results array")
	}
	if container && report.ArtifactType != "container_image" {
		return fmt.Errorf("Trivy runtime report artifact type must be container_image")
	}
	for _, result := range report.Results {
		switch scanner {
		case "vulnerability":
			for _, finding := range result.Vulnerabilities {
				if blockingSeverity(finding.Severity) {
					return fmt.Errorf("blocking %s vulnerability %q", strings.ToUpper(finding.Severity), findingID(finding))
				}
			}
		case "secret":
			if len(result.Secrets) != 0 {
				return fmt.Errorf("active secret findings are not permitted")
			}
		case "license":
			// Legal severity is evaluated by the checked-in parsed SPDX policy,
			// not by scanner presentation labels.
		default:
			return fmt.Errorf("unsupported Trivy scanner %q", scanner)
		}
	}
	return nil
}

func validateGosec(data []byte) error {
	var report gosecReport
	if err := decodePermissiveJSON(data, &report); err != nil {
		return fmt.Errorf("decode gosec report: %w", err)
	}
	if report.Issues == nil || report.Errors == nil || report.Stats.Found != len(report.Issues) {
		return fmt.Errorf("gosec report is incomplete or internally inconsistent")
	}
	for _, errors := range report.Errors {
		if len(errors) != 0 {
			return fmt.Errorf("gosec analysis contains Go loading errors")
		}
	}
	for _, finding := range report.Issues {
		if blockingSeverity(finding.Severity) {
			return fmt.Errorf("blocking %s gosec finding %q", strings.ToUpper(finding.Severity), finding.RuleID)
		}
	}
	return nil
}

func validateGovuln(data []byte, now time.Time) error {
	var messages []govulnMessage
	if err := decodePermissiveJSON(data, &messages); err != nil {
		return fmt.Errorf("decode govulncheck stream: %w", err)
	}
	if len(messages) == 0 {
		return fmt.Errorf("govulncheck stream is empty")
	}
	var config *govulnConfig
	for _, message := range messages {
		if message.Config != nil {
			if config != nil {
				return fmt.Errorf("govulncheck stream has multiple config messages")
			}
			config = message.Config
		}
		if message.Finding != nil && len(message.Finding.Trace) > 0 && message.Finding.Trace[0].Function != "" {
			return fmt.Errorf("reachable Go vulnerability %q is not permitted", message.Finding.OSV)
		}
	}
	if config == nil || config.ProtocolVersion != "v1.0.0" || config.ScannerName != "govulncheck" || config.ScannerVersion != "v1.6.0" || config.DB != "https://vuln.go.dev" || config.ScanLevel != "symbol" || config.ScanMode != "source" {
		return fmt.Errorf("govulncheck config does not match the approved scanner protocol")
	}
	updated, err := parseUTCTime("govulncheck database last modified", config.DBLastModified)
	if err != nil {
		return err
	}
	if updated.After(now.Add(5*time.Minute)) || now.Sub(updated) > 30*24*time.Hour {
		return fmt.Errorf("govulncheck database content is stale or from the future")
	}
	return nil
}

func blockingSeverity(value string) bool {
	severity := strings.ToUpper(value)
	return severity == "HIGH" || severity == "CRITICAL"
}

func findingID(finding trivyFinding) string {
	for _, candidate := range []string{finding.VulnerabilityID, finding.ID, finding.RuleID, finding.Name} {
		if candidate != "" {
			return candidate
		}
	}
	return "unidentified"
}
