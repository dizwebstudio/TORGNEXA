package userprofilerepo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserProfileMigrationKeepsIdentityTenantAndAvatarEvidenceBoundaries(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations", "000024_user_profiles.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql := strings.ToLower(string(data))
	for _, required := range []string{
		"create table user_profiles",
		"force row level security",
		"user_profiles_picture_upload_fk",
		"user_profiles_guard_update",
		"user_profiles_no_delete",
		"insert into migration_history",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("profile migration missing %q", required)
		}
	}
}
