// Package migration defines database migration metadata and resumable backfill
// orchestration without depending on a PostgreSQL driver.
package migration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxCatalogBytes   = 4 << 20
	maxMigrationBytes = 4 << 20
	maxMigrationTotal = 64 << 20
	maxMigrationFiles = 10000
	maxJSONNodes      = 100000
	maxJSONDepth      = 128
)

var (
	migrationNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,62}$`)
	jobKeyPattern        = regexp.MustCompile(`^[a-z][a-z0-9_.-]{2,127}$`)
	sha256Pattern        = regexp.MustCompile(`^[0-9a-f]{64}$`)

	// ErrChecksumDrift means an applied or repository migration no longer has
	// the immutable checksum declared by the catalog.
	ErrChecksumDrift = errors.New("migration checksum drift")
	// ErrUnknownAppliedMigration means the database was migrated by a catalog
	// newer than the running binary understands. Downgrade/startup must stop.
	ErrUnknownAppliedMigration = errors.New("unknown applied migration")
	// ErrHistoryGap means applied migration history is not a contiguous prefix.
	ErrHistoryGap = errors.New("migration history gap")
)

// Catalog is the strict, ordered inventory of repository SQL migrations.
type Catalog struct {
	SchemaVersion int         `json:"schema_version"`
	Migrations    []Migration `json:"migrations"`
}

// Migration describes one immutable expand, migrate, or contract step.
type Migration struct {
	Version        int           `json:"version"`
	Name           string        `json:"name"`
	File           string        `json:"file"`
	Phase          Phase         `json:"phase"`
	Risk           Risk          `json:"risk"`
	Transaction    string        `json:"transaction"`
	Policy         string        `json:"policy"`
	HistoryMode    string        `json:"history_mode"`
	RequiresBackup bool          `json:"requires_backup"`
	Dependencies   []int         `json:"dependencies"`
	Compatibility  Compatibility `json:"compatibility"`
	Backfill       *BackfillPlan `json:"backfill"`
	SHA256         string        `json:"sha256"`
}

// Compatibility declares rolling compatibility around a migration phase.
type Compatibility struct {
	OldReaders            bool     `json:"old_readers"`
	OldWriters            bool     `json:"old_writers"`
	NewBinaryOnOldSchema  bool     `json:"new_binary_on_old_schema"`
	ContractPreconditions []string `json:"contract_preconditions"`
}

// BackfillPlan declares bounded and idempotent resumable work for a migrate
// phase. Cursor values identify progress only and must never contain payloads.
type BackfillPlan struct {
	JobKey       string `json:"job_key"`
	TenantScoped bool   `json:"tenant_scoped"`
	Cursor       string `json:"cursor"`
	BatchSize    int    `json:"batch_size"`
	Idempotency  string `json:"idempotency"`
}

// Phase identifies an expand/migrate/contract stage.
type Phase string

const (
	PhaseExpand   Phase = "expand"
	PhaseMigrate  Phase = "migrate"
	PhaseContract Phase = "contract"
)

// Risk is the operational migration risk class.
type Risk string

const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
)

// AppliedMigration is immutable history read from a target database.
type AppliedMigration struct {
	Version int
	Name    string
	SHA256  string
}

// LoadCatalog loads and verifies the exact migration inventory beneath root.
// It rejects symlinks, unsafe/extra paths, checksum drift, phase-policy drift,
// destructive pre-contract SQL, and unbounded files.
func LoadCatalog(ctx context.Context, root string) (Catalog, error) {
	if ctx == nil {
		return Catalog{}, errors.New("migration catalog: context is required")
	}
	if err := ctx.Err(); err != nil {
		return Catalog{}, fmt.Errorf("migration catalog: %w", err)
	}
	rootPath, err := safeRoot(root)
	if err != nil {
		return Catalog{}, err
	}
	migrationsDir := filepath.Join(rootPath, "migrations")
	if err := requireDirectory(migrationsDir); err != nil {
		return Catalog{}, fmt.Errorf("migration catalog: %w", err)
	}
	catalogPath := filepath.Join(migrationsDir, "catalog.json")
	data, err := readRegularBounded(catalogPath, maxCatalogBytes)
	if err != nil {
		return Catalog{}, fmt.Errorf("migration catalog: %w", err)
	}
	var catalog Catalog
	if err := decodeStrict(data, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("migration catalog: decode: %w", err)
	}
	if err := validateCatalog(ctx, migrationsDir, catalog); err != nil {
		return Catalog{}, err
	}
	return catalog, nil
}

// Plan verifies that applied history is an exact contiguous catalog prefix and
// returns the remaining migrations. It never attempts a downgrade or repairs
// history implicitly.
func Plan(catalog Catalog, applied []AppliedMigration) ([]Migration, error) {
	if len(applied) > len(catalog.Migrations) {
		return nil, ErrUnknownAppliedMigration
	}
	for index, history := range applied {
		expectedVersion := index + 1
		if history.Version != expectedVersion {
			return nil, fmt.Errorf("migration %d: %w", history.Version, ErrHistoryGap)
		}
		if index >= len(catalog.Migrations) {
			return nil, fmt.Errorf("migration %d: %w", history.Version, ErrUnknownAppliedMigration)
		}
		expected := catalog.Migrations[index]
		if history.Version != expected.Version || history.Name != expected.Name {
			return nil, fmt.Errorf("migration %d: %w", history.Version, ErrUnknownAppliedMigration)
		}
		if history.SHA256 != expected.SHA256 {
			return nil, fmt.Errorf("migration %d: %w", history.Version, ErrChecksumDrift)
		}
	}
	pending := make([]Migration, len(catalog.Migrations)-len(applied))
	copy(pending, catalog.Migrations[len(applied):])
	return pending, nil
}

func validateCatalog(ctx context.Context, migrationsDir string, catalog Catalog) error {
	if catalog.SchemaVersion != 1 {
		return errors.New("migration catalog: schema_version must be 1")
	}
	if len(catalog.Migrations) == 0 || len(catalog.Migrations) > maxMigrationFiles {
		return fmt.Errorf("migration catalog: migrations count must be between 1 and %d", maxMigrationFiles)
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("migration catalog: list migrations: %w", err)
	}
	if len(entries) > maxMigrationFiles+1 {
		return fmt.Errorf("migration catalog: migrations directory exceeds %d entries", maxMigrationFiles+1)
	}
	files := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("migration catalog: %w", err)
		}
		name := entry.Name()
		if name == "catalog.json" || name == "baseline-manifest.json" {
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
				return fmt.Errorf("migration catalog: unsafe metadata path: %s", name)
			}
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration catalog: symlink is forbidden: %s", name)
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".sql") {
			return fmt.Errorf("migration catalog: unregistered path is forbidden: %s", name)
		}
		files[name] = struct{}{}
	}
	if len(files) != len(catalog.Migrations) {
		return fmt.Errorf("migration catalog: SQL inventory has %d files, catalog has %d", len(files), len(catalog.Migrations))
	}
	seenNames := make(map[string]struct{}, len(catalog.Migrations))
	seenJobs := make(map[string]struct{})
	totalBytes := 0
	for index, migration := range catalog.Migrations {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("migration catalog: %w", err)
		}
		expectedVersion := index + 1
		if migration.Version != expectedVersion {
			return fmt.Errorf("migration catalog: version %d is out of order; expected %d", migration.Version, expectedVersion)
		}
		if !migrationNamePattern.MatchString(migration.Name) {
			return fmt.Errorf("migration %d: invalid name", migration.Version)
		}
		if _, duplicate := seenNames[migration.Name]; duplicate {
			return fmt.Errorf("migration %d: duplicate name", migration.Version)
		}
		seenNames[migration.Name] = struct{}{}
		expectedFile := fmt.Sprintf("%06d_%s.sql", migration.Version, migration.Name)
		if migration.File != expectedFile || filepath.Base(migration.File) != migration.File {
			return fmt.Errorf("migration %d: file must be %s", migration.Version, expectedFile)
		}
		if _, exists := files[migration.File]; !exists {
			return fmt.Errorf("migration %d: catalog file is missing", migration.Version)
		}
		if migration.Transaction != "embedded" {
			return fmt.Errorf("migration %d: transaction must be embedded", migration.Version)
		}
		if migration.Policy != "v1" && migration.Policy != "legacy" {
			return fmt.Errorf("migration %d: unsupported policy", migration.Version)
		}
		if migration.Policy == "legacy" && migration.Version != 1 {
			return fmt.Errorf("migration %d: only migration 1 may use legacy policy", migration.Version)
		}
		if migration.HistoryMode != "bootstrap" && migration.HistoryMode != "atomic" {
			return fmt.Errorf("migration %d: unsupported history mode", migration.Version)
		}
		if migration.HistoryMode == "bootstrap" && migration.Version > 3 {
			return fmt.Errorf("migration %d: bootstrap history mode is restricted to framework adoption", migration.Version)
		}
		if migration.HistoryMode == "atomic" && migration.Version <= 3 {
			return fmt.Errorf("migration %d: pre-framework migrations must use bootstrap history mode", migration.Version)
		}
		if !sha256Pattern.MatchString(migration.SHA256) {
			return fmt.Errorf("migration %d: invalid SHA-256", migration.Version)
		}
		if err := validateDependencies(migration); err != nil {
			return err
		}
		if err := validateMetadata(migration, seenJobs); err != nil {
			return err
		}
		sqlPath := filepath.Join(migrationsDir, migration.File)
		sqlData, err := readRegularBounded(sqlPath, maxMigrationBytes)
		if err != nil {
			return fmt.Errorf("migration %d: %w", migration.Version, err)
		}
		totalBytes += len(sqlData)
		if totalBytes > maxMigrationTotal {
			return fmt.Errorf("migration catalog: aggregate SQL exceeds %d bytes", maxMigrationTotal)
		}
		digest := sha256.Sum256(sqlData)
		if hex.EncodeToString(digest[:]) != migration.SHA256 {
			return fmt.Errorf("migration %d: %w", migration.Version, ErrChecksumDrift)
		}
		if err := validateSQL(migration, sqlData); err != nil {
			return err
		}
	}
	return nil
}

func validateDependencies(migration Migration) error {
	previous := 0
	for _, dependency := range migration.Dependencies {
		if dependency <= previous || dependency >= migration.Version {
			return fmt.Errorf("migration %d: dependencies must be sorted, unique, and earlier", migration.Version)
		}
		previous = dependency
	}
	if migration.Version == 1 {
		if len(migration.Dependencies) != 0 {
			return errors.New("migration 1: dependencies must be empty")
		}
		return nil
	}
	if len(migration.Dependencies) == 0 || migration.Dependencies[len(migration.Dependencies)-1] != migration.Version-1 {
		return fmt.Errorf("migration %d: immediate predecessor dependency is required", migration.Version)
	}
	return nil
}

func validateMetadata(migration Migration, seenJobs map[string]struct{}) error {
	if migration.Risk != RiskLow && migration.Risk != RiskMedium && migration.Risk != RiskHigh && migration.Risk != RiskCritical {
		return fmt.Errorf("migration %d: invalid risk", migration.Version)
	}
	if (migration.Risk == RiskHigh || migration.Risk == RiskCritical) && !migration.RequiresBackup {
		return fmt.Errorf("migration %d: high/critical risk requires a backup checkpoint", migration.Version)
	}
	preconditions := migration.Compatibility.ContractPreconditions
	if len(preconditions) > 32 {
		return fmt.Errorf("migration %d: too many contract preconditions", migration.Version)
	}
	previous := ""
	for _, precondition := range preconditions {
		if !jobKeyPattern.MatchString(precondition) || (previous != "" && precondition <= previous) {
			return fmt.Errorf("migration %d: contract preconditions must be sorted unique safe keys", migration.Version)
		}
		previous = precondition
	}
	switch migration.Phase {
	case PhaseExpand:
		if !migration.Compatibility.OldReaders || !migration.Compatibility.OldWriters || !migration.Compatibility.NewBinaryOnOldSchema {
			return fmt.Errorf("migration %d: expand must preserve rolling read/write compatibility", migration.Version)
		}
		if len(preconditions) != 0 || migration.Backfill != nil {
			return fmt.Errorf("migration %d: expand cannot declare contract preconditions or a backfill", migration.Version)
		}
	case PhaseMigrate:
		if !migration.Compatibility.OldReaders || !migration.Compatibility.OldWriters {
			return fmt.Errorf("migration %d: migrate must preserve old readers and writers", migration.Version)
		}
		if migration.Backfill == nil {
			return fmt.Errorf("migration %d: migrate requires a backfill plan", migration.Version)
		}
	case PhaseContract:
		if migration.Compatibility.OldReaders || migration.Compatibility.OldWriters || migration.Compatibility.NewBinaryOnOldSchema {
			return fmt.Errorf("migration %d: contract compatibility flags must be false", migration.Version)
		}
		if !migration.RequiresBackup || len(preconditions) == 0 || migration.Backfill != nil {
			return fmt.Errorf("migration %d: contract requires backup and completion preconditions", migration.Version)
		}
	default:
		return fmt.Errorf("migration %d: invalid phase", migration.Version)
	}
	if migration.Backfill != nil {
		plan := migration.Backfill
		if !jobKeyPattern.MatchString(plan.JobKey) || (plan.Cursor != "lexicographic" && plan.Cursor != "monotonic_id") || plan.BatchSize < 1 || plan.BatchSize > 10000 || plan.Idempotency != "required" {
			return fmt.Errorf("migration %d: invalid backfill plan", migration.Version)
		}
		if _, duplicate := seenJobs[plan.JobKey]; duplicate {
			return fmt.Errorf("migration %d: duplicate backfill job key", migration.Version)
		}
		seenJobs[plan.JobKey] = struct{}{}
	}
	return nil
}

func validateSQL(migration Migration, data []byte) error {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return fmt.Errorf("migration %d: SQL must be valid UTF-8 without NUL", migration.Version)
	}
	cleaned, err := stripSQLCommentsAndStrings(string(data))
	if err != nil {
		return fmt.Errorf("migration %d: SQL lexical validation: %w", migration.Version, err)
	}
	normalized := strings.ToUpper(strings.Join(strings.Fields(cleaned), " "))
	if !strings.HasPrefix(normalized, "BEGIN;") || !strings.HasSuffix(normalized, "COMMIT;") {
		return fmt.Errorf("migration %d: embedded migration must be one explicit transaction", migration.Version)
	}
	if strings.Count(normalized, "BEGIN;") != 1 || strings.Count(normalized, "COMMIT;") != 1 {
		return fmt.Errorf("migration %d: nested or multiple transaction boundaries are forbidden", migration.Version)
	}
	for _, line := range strings.Split(cleaned, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "\\") {
			return fmt.Errorf("migration %d: psql meta-commands are forbidden", migration.Version)
		}
	}
	for _, forbidden := range []string{"ON DELETE CASCADE", "CREATE INDEX CONCURRENTLY", "DO $$", " PROGRAM "} {
		if strings.Contains(normalized, forbidden) {
			return fmt.Errorf("migration %d: forbidden SQL construct %q", migration.Version, strings.TrimSpace(forbidden))
		}
	}
	// CREATE TRIGGER ... EXECUTE FUNCTION is declarative trigger syntax, not
	// dynamic SQL. Keep arbitrary EXECUTE forbidden while permitting only that
	// reviewed form. Function bodies remain subject to the lexical restrictions
	// above (notably, dollar-quoted dynamic blocks are forbidden).
	triggerExecute := regexp.MustCompile(`EXECUTE\s+FUNCTION\s+[A-Z_][A-Z0-9_.]*\s*\(`)
	withoutTriggerExecute := triggerExecute.ReplaceAllString(normalized, "TRIGGER_FUNCTION(")
	if strings.Contains(withoutTriggerExecute, "EXECUTE ") {
		return fmt.Errorf("migration %d: forbidden SQL construct %q", migration.Version, "EXECUTE")
	}
	if migration.Policy == "v1" {
		for _, required := range []string{"SET LOCAL LOCK_TIMEOUT", "SET LOCAL STATEMENT_TIMEOUT"} {
			if !strings.Contains(normalized, required) {
				return fmt.Errorf("migration %d: missing %s", migration.Version, strings.ToLower(required))
			}
		}
	}
	if migration.HistoryMode == "atomic" {
		if !strings.Contains(normalized, "INSERT INTO MIGRATION_HISTORY") || !strings.Contains(normalized, "CURRENT_SETTING") {
			return fmt.Errorf("migration %d: atomic history mode must record catalog metadata inside the migration transaction", migration.Version)
		}
	}
	if migration.Phase != PhaseContract {
		// Preserve the conservative substring gate for destructive SQL. The only
		// v1 exception needed by append-only audit DDL is `TRUNCATE ON`, which is
		// privilege/trigger grammar rather than a TRUNCATE statement.
		destructiveScan := regexp.MustCompile(`TRUNCATE\s+ON`).ReplaceAllString(normalized, "TRUNCATE_PRIVILEGE_ON")
		for _, destructive := range []string{"DROP TABLE", "DROP COLUMN", "TRUNCATE ", "DELETE FROM"} {
			if strings.Contains(destructiveScan, destructive) {
				return fmt.Errorf("migration %d: destructive SQL requires a contract phase", migration.Version)
			}
		}
	}
	return nil
}

func stripSQLCommentsAndStrings(source string) (string, error) {
	var output strings.Builder
	output.Grow(len(source))
	for index := 0; index < len(source); {
		switch {
		case source[index] == '\'':
			output.WriteString("''")
			index++
			closed := false
			for index < len(source) {
				if source[index] != '\'' {
					index++
					continue
				}
				if index+1 < len(source) && source[index+1] == '\'' {
					index += 2
					continue
				}
				index++
				closed = true
				break
			}
			if !closed {
				return "", errors.New("unterminated string literal")
			}
		case index+1 < len(source) && source[index:index+2] == "--":
			for index < len(source) && source[index] != '\n' {
				index++
			}
			output.WriteByte('\n')
		case index+1 < len(source) && source[index:index+2] == "/*":
			end := strings.Index(source[index+2:], "*/")
			if end < 0 {
				return "", errors.New("unterminated block comment")
			}
			if strings.Contains(source[index+2:index+2+end], "/*") {
				return "", errors.New("nested block comments are forbidden")
			}
			index += end + 4
			output.WriteByte(' ')
		case source[index] == '$':
			return "", errors.New("dollar-quoted/dynamic blocks are forbidden")
		default:
			output.WriteByte(source[index])
			index++
		}
	}
	return output.String(), nil
}

func safeRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("migration catalog: root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("migration catalog: resolve root: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return "", fmt.Errorf("migration catalog: stat root: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("migration catalog: root must be a non-symlink directory")
	}
	return filepath.Clean(absolute), nil
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("migrations path must be a non-symlink directory")
	}
	return nil
}

func readRegularBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("file must be regular and non-symlink")
	}
	if info.Size() < 1 || info.Size() > limit {
		return nil, fmt.Errorf("file size must be between 1 and %d bytes", limit)
	}
	// #nosec G304 -- callers construct paths beneath a validated repository root
	// and reject symlinks before reading.
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != info.Size() {
		return nil, errors.New("file changed while being read")
	}
	return data, nil
}

func decodeStrict(data []byte, destination any) error {
	if err := validateJSONTokens(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are forbidden")
		}
		return err
	}
	return nil
}

func validateJSONTokens(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	var parseValue func(int) error
	parseValue = func(depth int) error {
		if depth > maxJSONDepth {
			return errors.New("JSON nesting limit exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		nodes++
		if nodes > maxJSONNodes {
			return errors.New("JSON node limit exceeded")
		}
		delimiter, isDelimiter := token.(json.Delim)
		if !isDelimiter {
			return nil
		}
		switch delimiter {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("JSON object key must be a string")
				}
				nodes++
				if nodes > maxJSONNodes {
					return errors.New("JSON node limit exceeded")
				}
				if _, duplicate := seen[key]; duplicate {
					return errors.New("duplicate JSON object key")
				}
				seen[key] = struct{}{}
				if err := parseValue(depth + 1); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return errors.New("invalid JSON object closing delimiter")
			}
		case '[':
			for decoder.More() {
				if err := parseValue(depth + 1); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return errors.New("invalid JSON array closing delimiter")
			}
		default:
			return errors.New("unexpected JSON closing delimiter")
		}
		return nil
	}
	if err := parseValue(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values are forbidden")
		}
		return err
	}
	return nil
}
