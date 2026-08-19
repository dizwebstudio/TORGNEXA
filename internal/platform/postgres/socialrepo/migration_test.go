package socialrepo

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationEncodesTenantMediaCapabilityAndLifecycleGuards(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "migrations_legacy_pre_v1", "000027_social_core.sql"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"create table social_contents",
		"create table social_content_variants",
		"create table social_variant_media_refs",
		"create table social_channel_accounts",
		"create table social_publications",
		"create table social_publication_status_events",
		"force row level security",
		"social media requires current released upload",
		"social channel requires social connector account",
		"social publication capability missing",
		"social publication lifecycle transition invalid",
		"if new.status in (''ready'',''publishing'') then",
		"social publication status history is append-only",
		"social core history cannot be hard-deleted",
		"insert into migration_history",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("migration missing %q", needle)
		}
	}
	for _, forbidden := range []string{"remote_post_id", "provider_payload", "access_token", "refresh_token", "signed_url text", "object_key text"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("provider/secret field %q found in social core migration", forbidden)
		}
	}
}

func TestRepositoryKeepsTenantScopeOutboxAuditAndSafeEventPayloads(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(source), "repository.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(raw))
	for _, needle := range []string{
		"set_config('app.organization_id'",
		"set_config('app.workspace_id'",
		"appendaudit(",
		"outboxrepo.newtransactionenqueuer",
		"commerce.social.content_changed.v1",
		"commerce.social.variant_changed.v1",
		"commerce.social.channel_account_changed.v1",
		"commerce.social.publication_status_changed.v1",
		"state != \"released\"",
		"version=$",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("repository missing %q", needle)
		}
	}
	// Social event payloads intentionally carry IDs/state only. Content body and raw remote errors stay out of outbox events.
	eventSection := text[strings.Index(text, "func enqueuecontentevent"):]
	for _, forbidden := range []string{"raw_error", "provider_payload", "access_token", "refresh_token"} {
		if strings.Contains(eventSection, forbidden) {
			t.Fatalf("unsafe event field %q found", forbidden)
		}
	}
}
