// Package mcpaccounts holds the tenant-scoped MCP client account shape
// shared by the settings API and its Postgres repository. An account is
// the only credential an external MCP (Model Context Protocol) caller can
// present to authenticate against POST /mcp; which tools it may invoke is
// resolved only from its own Permissions, never by branching on any other
// field here.
package mcpaccounts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalid  = errors.New("mcpaccounts: invalid value")
	ErrNotFound = errors.New("mcpaccounts: not found")
	ErrConflict = errors.New("mcpaccounts: version conflict")
)

// KnownPermissions mirrors the exact tool-permission strings declared in
// internal/app/mcp/server.go. It is duplicated here rather than imported:
// internal/app/mcp is an application-layer package and this is a
// platform-layer package, so importing it would invert the dependency
// direction the architecture checker enforces. Keep both lists in sync by
// hand if a tool's permission string ever changes.
var KnownPermissions = []string{
	"commerce.products.read",
	"commerce.orders.read",
	"party.counterparties.read",
	"commerce.price.change.request",
}

func isKnownPermission(value string) bool {
	for _, known := range KnownPermissions {
		if value == known {
			return true
		}
	}
	return false
}

// Account is a tenant-scoped MCP client account. TokenHash is the only
// credential material persisted (SHA-256 of the raw bearer token); the raw
// token itself is never stored or returned again after creation.
type Account struct {
	ID            string
	Label         string
	AgentID       string
	ModelID       string
	IntegrationID string
	Permissions   []string
	Enabled       bool
	Version       int64
	RotatedFromID string
	ExpiresAt     time.Time
	RevokedAt     *time.Time
	LastUsedAt    *time.Time
	UseCount      int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateAccount is the validated command used to create a new Account.
type CreateAccount struct {
	ID            string
	Label         string
	AgentID       string
	ModelID       string
	IntegrationID string
	Permissions   []string
}

const (
	maxLabelLength   = 120
	maxAgentIDLength = 160
	minPermissions   = 1
	maxPermissions   = 4 // == len(KnownPermissions); Go consts can't derive from a package var
)

// ValidateCreate checks only generic field shape and that every requested
// permission is one of the known tool-permission strings; it never
// branches on which tool a permission names.
func ValidateCreate(cmd CreateAccount) error {
	label := strings.TrimSpace(cmd.Label)
	agentID := strings.TrimSpace(cmd.AgentID)
	modelID := strings.TrimSpace(cmd.ModelID)
	integrationID := strings.TrimSpace(cmd.IntegrationID)
	if label == "" || len(label) > maxLabelLength ||
		agentID == "" || len(agentID) > maxAgentIDLength ||
		modelID == "" || len(modelID) > maxAgentIDLength ||
		integrationID == "" || len(integrationID) > maxAgentIDLength {
		return ErrInvalid
	}
	if len(cmd.Permissions) < minPermissions || len(cmd.Permissions) > maxPermissions {
		return ErrInvalid
	}
	seen := make(map[string]struct{}, len(cmd.Permissions))
	for _, permission := range cmd.Permissions {
		if !isKnownPermission(permission) {
			return ErrInvalid
		}
		if _, duplicate := seen[permission]; duplicate {
			return ErrInvalid
		}
		seen[permission] = struct{}{}
	}
	return nil
}

// TokenPrefix marks a value as an MCP client bearer token.
const TokenPrefix = "mcp_"

// SecretLength is the raw random secret's byte length before hashing.
const SecretLength = 32

// GenerateSecret returns a fresh cryptographically random secret suitable
// for HashSecret/EncodeToken. Callers must zero it after use.
func GenerateSecret() ([]byte, error) {
	secret := make([]byte, SecretLength)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// HashSecret returns the SHA-256 digest stored as Account.TokenHash.
func HashSecret(secret []byte) []byte {
	sum := sha256.Sum256(secret)
	return sum[:]
}

// EncodeToken builds the bearer token returned to the caller exactly once,
// at account-creation time. It embeds organization/workspace/account IDs so
// the MCP identity resolver can build a tenancy.Scope directly from the
// token, without a cross-tenant database search (Postgres RLS has no
// tenant context to enforce until a scope is already known). The embedded
// IDs are only a routing hint: HashSecret's comparison against the stored
// token_hash is what actually authenticates the caller.
func EncodeToken(organizationID, workspaceID, accountID string, secret []byte) string {
	return TokenPrefix + strings.Join([]string{
		base64.RawURLEncoding.EncodeToString([]byte(organizationID)),
		base64.RawURLEncoding.EncodeToString([]byte(workspaceID)),
		base64.RawURLEncoding.EncodeToString([]byte(accountID)),
		base64.RawURLEncoding.EncodeToString(secret),
	}, ".")
}

// DecodedToken is a parsed-but-not-yet-verified bearer token.
type DecodedToken struct {
	OrganizationID string
	WorkspaceID    string
	AccountID      string
	Secret         []byte
}

// DecodeToken parses a token previously returned by EncodeToken. It proves
// nothing on its own: the caller must still look up the account by
// (OrganizationID, WorkspaceID, AccountID) and compare HashSecret(Secret)
// against the stored token_hash before trusting anything it contains.
func DecodeToken(token string) (DecodedToken, error) {
	if !strings.HasPrefix(token, TokenPrefix) {
		return DecodedToken{}, ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(token, TokenPrefix), ".")
	if len(parts) != 4 {
		return DecodedToken{}, ErrInvalid
	}
	decoded := make([][]byte, len(parts))
	for i, part := range parts {
		value, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil || len(value) == 0 || base64.RawURLEncoding.EncodeToString(value) != part {
			return DecodedToken{}, ErrInvalid
		}
		decoded[i] = value
	}
	return DecodedToken{
		OrganizationID: string(decoded[0]),
		WorkspaceID:    string(decoded[1]),
		AccountID:      string(decoded[2]),
		Secret:         decoded[3],
	}, nil
}
