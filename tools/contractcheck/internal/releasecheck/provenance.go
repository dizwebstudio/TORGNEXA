package releasecheck

import (
	"context"
	"fmt"
)

type provenanceStatement struct {
	Type          string              `json:"_type"`
	Subject       []provenanceSubject `json:"subject"`
	PredicateType string              `json:"predicateType"`
	Predicate     provenancePredicate `json:"predicate"`
}

type provenanceSubject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type provenancePredicate struct {
	BuildDefinition provenanceBuildDefinition `json:"buildDefinition"`
	RunDetails      provenanceRunDetails      `json:"runDetails"`
}

type provenanceBuildDefinition struct {
	BuildType          string                       `json:"buildType"`
	ExternalParameters provenanceExternalParameters `json:"externalParameters"`
}

type provenanceExternalParameters struct {
	Repository     string `json:"repository"`
	Commit         string `json:"commit"`
	Ref            string `json:"ref"`
	Workflow       string `json:"workflow"`
	WorkflowRunID  string `json:"workflow_run_id"`
	WorkflowRunURL string `json:"workflow_run_url"`
}

type provenanceRunDetails struct {
	Builder provenanceBuilder `json:"builder"`
}

type provenanceBuilder struct {
	ID string `json:"id"`
}

func validateProvenance(ctx context.Context, data []byte, expected subject, release releaseMetadata) error {
	if _, err := decodeJSONValue(ctx, data); err != nil {
		return fmt.Errorf("invalid provenance JSON: %w", err)
	}
	var statement provenanceStatement
	if err := decodePermissiveJSON(data, &statement); err != nil {
		return fmt.Errorf("decode provenance: %w", err)
	}
	if statement.Type != "https://in-toto.io/Statement/v1" {
		return fmt.Errorf("provenance _type must be the in-toto Statement v1 URI")
	}
	if statement.PredicateType != "https://slsa.dev/provenance/v1" {
		return fmt.Errorf("provenance predicateType must be the SLSA provenance v1 URI")
	}
	if len(statement.Subject) != 1 {
		return fmt.Errorf("provenance must contain exactly one subject")
	}
	actual := statement.Subject[0]
	if actual.Name != expected.Name || actual.Digest["sha256"] != expected.SHA256 {
		return fmt.Errorf("provenance subject does not match %q and its SHA-256", expected.Name)
	}
	parameters := statement.Predicate.BuildDefinition.ExternalParameters
	if parameters.Repository != release.Repository || parameters.Commit != release.Commit || parameters.Ref != release.Ref || parameters.Workflow != release.Workflow || parameters.WorkflowRunID != release.WorkflowRunID || parameters.WorkflowRunURL != release.WorkflowRunURL {
		return fmt.Errorf("provenance source identity does not match release metadata")
	}
	if err := requireText("provenance buildType", statement.Predicate.BuildDefinition.BuildType, 1, 512); err != nil {
		return err
	}
	if err := requireText("provenance builder.id", statement.Predicate.RunDetails.Builder.ID, 1, 512); err != nil {
		return err
	}
	return nil
}
