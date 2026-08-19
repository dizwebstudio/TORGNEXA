package releasecheck

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type spdxDocument struct {
	SPDXVersion       string           `json:"spdxVersion"`
	DataLicense       string           `json:"dataLicense"`
	SPDXID            string           `json:"SPDXID"`
	Name              string           `json:"name"`
	DocumentNamespace string           `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo `json:"creationInfo"`
	Packages          []spdxPackage    `json:"packages"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name      string         `json:"name"`
	SPDXID    string         `json:"SPDXID"`
	Checksums []spdxChecksum `json:"checksums"`
}

type spdxChecksum struct {
	Algorithm     string `json:"algorithm"`
	ChecksumValue string `json:"checksumValue"`
}

func validateSPDX(ctx context.Context, data []byte, expected subject, now time.Time) error {
	document, err := validateSPDXDocument(ctx, data, now)
	if err != nil {
		return err
	}
	digestMapped := false
	for _, pkg := range document.Packages {
		for _, checksum := range pkg.Checksums {
			if checksum.Algorithm == "SHA256" && checksum.ChecksumValue == expected.SHA256 {
				digestMapped = true
			}
		}
	}
	if !digestMapped {
		return fmt.Errorf("SPDX packages do not map subject %q SHA-256", expected.Name)
	}
	return nil
}

func validateRuntimeSPDX(ctx context.Context, data []byte, expected runtimeSubject, now time.Time) error {
	document, err := validateSPDXDocument(ctx, data, now)
	if err != nil {
		return err
	}
	if document.Name != expected.Image {
		return fmt.Errorf("SPDX name must equal immutable runtime image %q", expected.Image)
	}
	return nil
}

func validateSPDXDocument(ctx context.Context, data []byte, now time.Time) (spdxDocument, error) {
	if err := ctx.Err(); err != nil {
		return spdxDocument{}, fmt.Errorf("validation interrupted: %w", err)
	}
	if _, err := decodeJSONValue(ctx, data); err != nil {
		return spdxDocument{}, fmt.Errorf("invalid SPDX JSON: %w", err)
	}
	var document spdxDocument
	decoderTarget := &document
	// SPDX permits many standard fields beyond the minimum inspected here, so
	// decode without DisallowUnknownFields after duplicate-key validation.
	if err := decodePermissiveJSON(data, decoderTarget); err != nil {
		return spdxDocument{}, fmt.Errorf("decode SPDX document: %w", err)
	}
	if document.SPDXVersion != "SPDX-2.3" {
		return spdxDocument{}, fmt.Errorf("spdxVersion must be SPDX-2.3")
	}
	if document.DataLicense != "CC0-1.0" {
		return spdxDocument{}, fmt.Errorf("SPDX dataLicense must be CC0-1.0")
	}
	if document.SPDXID != "SPDXRef-DOCUMENT" {
		return spdxDocument{}, fmt.Errorf("SPDX document ID must be SPDXRef-DOCUMENT")
	}
	if err := requireText("SPDX name", document.Name, 1, 256); err != nil {
		return spdxDocument{}, err
	}
	namespace, err := url.Parse(document.DocumentNamespace)
	if err != nil || namespace.Scheme != "https" || namespace.Host == "" || namespace.Fragment != "" {
		return spdxDocument{}, fmt.Errorf("SPDX documentNamespace must be an absolute HTTPS URL without a fragment")
	}
	created, err := parseUTCTime("SPDX creationInfo.created", document.CreationInfo.Created)
	if err != nil {
		return spdxDocument{}, err
	}
	if created.After(now.Add(5 * time.Minute)) {
		return spdxDocument{}, fmt.Errorf("SPDX creationInfo.created is in the future")
	}
	if len(document.CreationInfo.Creators) == 0 {
		return spdxDocument{}, fmt.Errorf("SPDX creationInfo.creators must not be empty")
	}
	for _, creator := range document.CreationInfo.Creators {
		if err := requireText("SPDX creator", creator, 1, 256); err != nil {
			return spdxDocument{}, err
		}
		if !strings.Contains(creator, ": ") {
			return spdxDocument{}, fmt.Errorf("SPDX creator %q must include a creator type", creator)
		}
	}
	if len(document.Packages) == 0 {
		return spdxDocument{}, fmt.Errorf("SPDX packages must not be empty")
	}
	packageIDs := make(map[string]struct{}, len(document.Packages))
	for index, pkg := range document.Packages {
		if err := requireText(fmt.Sprintf("SPDX package %d name", index), pkg.Name, 1, 256); err != nil {
			return spdxDocument{}, err
		}
		if !strings.HasPrefix(pkg.SPDXID, "SPDXRef-") || len(pkg.SPDXID) <= len("SPDXRef-") {
			return spdxDocument{}, fmt.Errorf("SPDX package %d has an invalid SPDXID", index)
		}
		if _, duplicate := packageIDs[pkg.SPDXID]; duplicate {
			return spdxDocument{}, fmt.Errorf("duplicate SPDX package ID %q", pkg.SPDXID)
		}
		packageIDs[pkg.SPDXID] = struct{}{}
	}
	return document, nil
}
