// Package aiadvisory holds the tenant-scoped configured AI provider account
// shape shared by the settings API and its Postgres repository. It never
// dispatches by provider identity: which connector implementation answers a
// given account is resolved only inside internal/platform/builtinruntime,
// the repository's sole registered provider-composition boundary. Enum
// membership for Provider is enforced by the ai_provider_accounts table's
// CHECK constraint and by internal/platform/builtinruntime's connector
// registry lookup, not by a conditional in this package.
package aiadvisory

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("aiadvisory: invalid value")
	ErrNotFound = errors.New("aiadvisory: not found")
	ErrConflict = errors.New("aiadvisory: version conflict")
)

// Provider identifies a supported external AI service. It is opaque data:
// this package stores and forwards it but never branches on its value.
type Provider string

// Account is a tenant-scoped configured AI provider account. Credential
// material is never held here: SecretReference points at a secret managed by
// secrets.SecretProvider (secrets.ClassAIProviderCredential).
type Account struct {
	ID              string
	Provider        Provider
	Label           string
	Model           string
	BaseURL         string
	FolderID        string
	SecretReference string
	Enabled         bool
	Version         int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CreateAccount is the validated command used to create a new Account. The
// caller supplies raw Credential bytes; the API layer is responsible for
// exchanging them for a secrets.Reference before calling the repository.
type CreateAccount struct {
	ID         string
	Provider   Provider
	Label      string
	Model      string
	BaseURL    string
	FolderID   string
	Credential []byte
}

const (
	maxLabelLength      = 120
	maxModelLength      = 120
	maxFolderIDLength   = 120
	maxBaseURLLength    = 2039
	maxCredentialLength = 65536
	minIdentifierLength = 1
	maxIdentifierLength = 63
)

// ValidateCreate checks only generic, provider-neutral field shape. Which
// provider values are dispatchable, and which of them require FolderID or
// accept a BaseURL override, is enforced downstream: the database CHECK
// constraint rejects an unknown Provider, and the connector resolved through
// builtinruntime rejects a call it cannot serve (e.g. YandexGPT with an
// empty FolderID) before any remote request is made.
func ValidateCreate(cmd CreateAccount) error {
	label := strings.TrimSpace(cmd.Label)
	model := strings.TrimSpace(cmd.Model)
	folderID := strings.TrimSpace(cmd.FolderID)
	baseURL := strings.TrimSpace(cmd.BaseURL)
	identifierLength := len(strings.TrimSpace(string(cmd.Provider)))
	if identifierLength < minIdentifierLength || identifierLength > maxIdentifierLength ||
		label == "" || len(label) > maxLabelLength ||
		model == "" || len(model) > maxModelLength ||
		len(folderID) > maxFolderIDLength ||
		len(baseURL) > maxBaseURLLength || strings.ContainsAny(baseURL, " \t\r\n") ||
		len(cmd.Credential) == 0 || len(cmd.Credential) > maxCredentialLength {
		return ErrInvalid
	}
	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return ErrInvalid
		}
		if port := parsed.Port(); port != "" {
			value, parseErr := strconv.Atoi(port)
			if parseErr != nil || value < 1 || value > 65535 {
				return ErrInvalid
			}
		}
	}
	return nil
}

const MaxPromptLength = 32_000

// ValidateCompletionRequest checks only bounded length; the caller decides
// what analytics text is safe to send off-tenant before calling this.
func ValidateCompletionRequest(systemPrompt, userPrompt string) error {
	prompt := strings.TrimSpace(userPrompt)
	if prompt == "" || len(prompt) > MaxPromptLength || len(systemPrompt) > MaxPromptLength {
		return ErrInvalid
	}
	return nil
}
