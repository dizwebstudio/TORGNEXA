// Package runtimeposture proves that a TORGNEXA process is running with the
// non-privileged database and patched Go runtime required by the trust boundary.
package runtimeposture

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const MinimumGoVersion = "go1.26.7"

var (
	// ErrInvalid means posture inspection could not be performed safely.
	ErrInvalid = errors.New("runtime posture: invalid")
	// ErrUnsafe means one or more mandatory runtime controls are absent.
	ErrUnsafe = errors.New("runtime posture: unsafe")
)

const databaseIdentityQuery = `SELECT current_user,
       r.rolsuper,
       r.rolbypassrls,
       r.rolcreaterole,
       r.rolcreatedb,
       has_schema_privilege(current_user, 'public', 'CREATE'),
       EXISTS (
         SELECT 1
           FROM pg_class c
           JOIN pg_namespace n ON n.oid = c.relnamespace
          WHERE c.relowner = r.oid
            AND n.nspname NOT IN ('pg_catalog', 'information_schema')
            AND c.relkind IN ('r','p','S','v','m','f')
       )
  FROM pg_roles r
 WHERE r.rolname = current_user`

type rowScanner interface{ Scan(...any) error }

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// DatabaseIdentity is minimized database-role evidence. It contains no DSN,
// password, host, tenant data or database contents.
type DatabaseIdentity struct {
	Role             string `json:"role"`
	Superuser        bool   `json:"superuser"`
	BypassRLS        bool   `json:"bypass_rls"`
	CanCreateRole    bool   `json:"can_create_role"`
	CanCreateDB      bool   `json:"can_create_database"`
	CanCreateSchema  bool   `json:"can_create_schema"`
	OwnsRuntimeState bool   `json:"owns_runtime_state"`
}

// Assessment is safe to expose through the administrator posture endpoint.
type Assessment struct {
	Status           string           `json:"status"`
	CheckedAt        time.Time        `json:"checked_at"`
	GoVersion        string           `json:"go_version"`
	MinimumGoVersion string           `json:"minimum_go_version"`
	Database         DatabaseIdentity `json:"database"`
	GoRuntimePatched bool             `json:"go_runtime_patched"`
	LeastPrivilege   bool             `json:"least_privilege"`
}

// Inspector reevaluates the runtime database identity for readiness and
// administrator inspection. It is safe for concurrent use.
type Inspector struct{ database queryer }

// NewInspector constructs a posture inspector over the runtime application pool.
func NewInspector(database *sql.DB) (*Inspector, error) {
	if database == nil {
		return nil, ErrInvalid
	}
	return &Inspector{database: database}, nil
}

// Inspect returns minimized evidence and ErrUnsafe when any mandatory control
// fails. Callers may still return the assessment to an authorized operator.
func (inspector *Inspector) Inspect(ctx context.Context) (Assessment, error) {
	if ctx == nil || inspector == nil || inspector.database == nil {
		return Assessment{}, ErrInvalid
	}
	var identity DatabaseIdentity
	row := inspector.database.QueryRowContext(ctx, databaseIdentityQuery)
	if err := scanIdentity(row, &identity); err != nil {
		return Assessment{}, fmt.Errorf("%w: database identity", ErrInvalid)
	}
	assessment := evaluate(identity, runtime.Version(), time.Now().UTC())
	if assessment.Status != "pass" {
		return assessment, ErrUnsafe
	}
	return assessment, nil
}

func scanIdentity(row rowScanner, identity *DatabaseIdentity) error {
	if row == nil || identity == nil {
		return ErrInvalid
	}
	return row.Scan(&identity.Role, &identity.Superuser, &identity.BypassRLS, &identity.CanCreateRole, &identity.CanCreateDB, &identity.CanCreateSchema, &identity.OwnsRuntimeState)
}

func evaluate(identity DatabaseIdentity, goVersion string, checkedAt time.Time) Assessment {
	leastPrivilege := strings.TrimSpace(identity.Role) != "" && !identity.Superuser && !identity.BypassRLS && !identity.CanCreateRole && !identity.CanCreateDB && !identity.CanCreateSchema && !identity.OwnsRuntimeState
	patched := versionAtLeast(goVersion, MinimumGoVersion)
	status := "fail"
	if leastPrivilege && patched {
		status = "pass"
	}
	return Assessment{Status: status, CheckedAt: checkedAt.UTC(), GoVersion: goVersion, MinimumGoVersion: MinimumGoVersion, Database: identity, GoRuntimePatched: patched, LeastPrivilege: leastPrivilege}
}

func versionAtLeast(actual, minimum string) bool {
	parse := func(value string) ([3]int, bool) {
		var out [3]int
		value = strings.TrimSpace(strings.TrimPrefix(value, "devel "))
		value = strings.TrimPrefix(value, "go")
		value = strings.SplitN(value, "-", 2)[0]
		parts := strings.Split(value, ".")
		if len(parts) < 2 || len(parts) > 3 {
			return out, false
		}
		for index := range parts {
			parsed, err := strconv.Atoi(parts[index])
			if err != nil || parsed < 0 {
				return out, false
			}
			out[index] = parsed
		}
		return out, true
	}
	a, okA := parse(actual)
	m, okM := parse(minimum)
	if !okA || !okM {
		return false
	}
	for index := range a {
		if a[index] != m[index] {
			return a[index] > m[index]
		}
	}
	return true
}
