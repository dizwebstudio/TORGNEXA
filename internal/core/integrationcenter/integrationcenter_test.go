package integrationcenter

import (
	"strings"
	"testing"
	"time"
)

func evidence(now time.Time, visibility Visibility) EvidenceRef {
	return EvidenceRef{ObservedAt: now.Add(-time.Minute), CheckedAt: now.Add(-time.Minute), SourceKind: "test", SourceRef: "source:1", Visibility: visibility, StaleAfterSeconds: 3600, AgeSeconds: 60}
}

func dimensions(now time.Time) Dimensions {
	e := evidence(now, VisibilityFull)
	return Dimensions{
		Runtime: Dimension{Status: string(RuntimeReady), Evidence: e}, Account: Dimension{Status: string(AccountActive), Evidence: e}, Credential: Dimension{Status: string(CredentialPresent), Evidence: e}, Configuration: Dimension{Status: string(ConfigurationValid), Evidence: e}, Health: Dimension{Status: string(HealthHealthy), Evidence: e}, Capability: Dimension{Status: string(CapabilityEnabled), Evidence: e}, Sync: Dimension{Status: string(SyncIdle), Evidence: e}, Reconciliation: Dimension{Status: string(ReconciliationHealthy), Evidence: e}, Webhook: Dimension{Status: string(WebhookReceiving), Evidence: e}, RateLimit: Dimension{Status: string(RateLimitAvailable), Evidence: e},
	}
}

func inputWith(now time.Time, d Dimensions) Input {
	return Input{AccountID: "account-1", ConnectorID: "storefront", Family: "storefront", Surface: "integrations", Version: 1, DisplayName: "Demo", Dimensions: d, SourceWatermarks: []string{"accounts:1"}, Now: now}
}

func TestReduceDominanceAndSecondaryIssues(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(*Dimensions)
		want   OverallStatus
	}{
		{"healthy", func(*Dimensions) {}, OverallHealthy},
		{"unsupported dominates", func(d *Dimensions) {
			d.Runtime.Status = string(RuntimeUnsupported)
			d.Health.Status = string(HealthHealthy)
		}, OverallUnsupported},
		{"blocked dominates setup", func(d *Dimensions) {
			d.Capability.Status = string(CapabilityBlocked)
			d.Credential.Status = string(CredentialMissing)
		}, OverallBlocked},
		{"setup before reauth", func(d *Dimensions) {
			d.Credential.Status = string(CredentialMissing)
			d.Configuration.Status = string(ConfigurationMissing)
		}, OverallSetupRequired},
		{"reauth", func(d *Dimensions) { d.Credential.Status = string(CredentialExpired) }, OverallReauthorizationRequired},
		{"disabled", func(d *Dimensions) { d.Account.Status = string(AccountDisabled) }, OverallDisabled},
		{"degraded", func(d *Dimensions) { d.Health.Status = string(HealthUnavailable) }, OverallDegraded},
		{"stale", func(d *Dimensions) { d.Health.Status = string(HealthStale) }, OverallStale},
		{"attention", func(d *Dimensions) { d.RateLimit.Status = string(RateLimitLimited) }, OverallAttention},
		{"syncing", func(d *Dimensions) { d.Sync.Status = string(SyncRunning) }, OverallSyncing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := dimensions(now)
			tc.mutate(&d)
			got, err := Reduce(inputWith(now, d))
			if err != nil {
				t.Fatal(err)
			}
			if got.Overall != tc.want {
				t.Fatalf("overall=%s want %s", got.Overall, tc.want)
			}
			if tc.name == "blocked dominates setup" && len(got.Issues) < 2 {
				t.Fatal("secondary setup issue was lost")
			}
		})
	}
}

func TestReduceFreshnessAndRedaction(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	d := dimensions(now)
	d.Health.Evidence.ExpiresAt = ptr(now.Add(-time.Second))
	got, err := Reduce(inputWith(now, d))
	if err != nil {
		t.Fatal(err)
	}
	if got.Dimensions.Health.Status != string(HealthStale) || got.Overall != OverallStale {
		t.Fatalf("health freshness not applied: %+v", got)
	}
	d = dimensions(now)
	d.Sync.Evidence.Visibility = VisibilityRedacted
	d.Sync.Status = string(SyncRunning)
	got, err = Reduce(inputWith(now, d))
	if err != nil {
		t.Fatal(err)
	}
	if got.Dimensions.Sync.Status != "unknown" {
		t.Fatalf("redacted sync status=%s", got.Dimensions.Sync.Status)
	}
}

func TestReduceDigestIsDeterministicAndSafe(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	one, err := Reduce(inputWith(now, dimensions(now)))
	if err != nil {
		t.Fatal(err)
	}
	two, err := Reduce(inputWith(now, dimensions(now)))
	if err != nil {
		t.Fatal(err)
	}
	if one.SnapshotDigest != two.SnapshotDigest || one.SnapshotID != two.SnapshotID {
		t.Fatal("identical inputs must have identical digest and id")
	}
	if len(one.SnapshotDigest) != 64 || !strings.HasPrefix(one.SnapshotID, "ic:") {
		t.Fatalf("invalid snapshot identity: %s %s", one.SnapshotID, one.SnapshotDigest)
	}
	bad := inputWith(now, dimensions(now))
	bad.Issues = []Issue{{Code: "bad", Dimension: "health", Severity: "warning", Title: "token=secret", ReasonCode: "unsafe", OccurrenceCount: 1, FirstSeenAt: now, LastSeenAt: now, Visibility: VisibilityFull}}
	if _, err := Reduce(bad); err == nil {
		t.Fatal("unsafe issue title must be rejected")
	}
}

func TestBuildSummary(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	rows := make([]Snapshot, 0, 3)
	for _, status := range []OverallStatus{OverallHealthy, OverallBlocked, OverallUnsupported} {
		row, err := Reduce(inputWith(now, dimensions(now)))
		if err != nil {
			t.Fatal(err)
		}
		row.Overall = status
		rows = append(rows, row)
	}
	s := BuildSummary(rows)
	if s.Total != 3 || s.Healthy != 1 || s.Blocked != 1 || s.Unsupported != 1 {
		t.Fatalf("summary=%+v", s)
	}
}

func ptr(t time.Time) *time.Time { return &t }
