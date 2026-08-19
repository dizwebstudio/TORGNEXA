package syncengine

import (
	"testing"
	"time"
)

func TestBootstrapPreviewAndScheduleValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	preview := BootstrapPreview{ID: "preview-1", AccountID: "cabinet-1", AccountVersion: 3, PolicyCount: 2, ReadCount: 2, WriteCount: 1, CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute)}
	if err := preview.Validate(); err != nil {
		t.Fatal(err)
	}
	next := now.Add(time.Hour)
	schedule := AccountSchedule{AccountID: "cabinet-1", Mode: ScheduleIncremental, IntervalMinutes: 60, Enabled: true, NextRunAt: &next, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := schedule.Validate(); err != nil {
		t.Fatal(err)
	}
	schedule.IntervalMinutes = 1
	if schedule.Validate() == nil {
		t.Fatal("unsafe high-frequency schedule accepted")
	}
}

func TestSyncJobRequiresPreviewOnlyForInitialImport(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	started := now
	job := SyncJob{ID: "job-1", OrganizationID: "org-1", WorkspaceID: "ws-1", AccountID: "cabinet-1", Kind: SyncJobInitialImport, Mode: ScheduleIncremental, Status: SyncJobRunning, PreviewID: "preview-1", AttemptCount: 1, MaxAttempts: 5, AvailableAt: now, CreatedAt: now, UpdatedAt: now, StartedAt: &started}
	if err := job.Validate(); err != nil {
		t.Fatal(err)
	}
	job.PreviewID = ""
	if job.Validate() == nil {
		t.Fatal("initial import without dry-run evidence accepted")
	}
}
