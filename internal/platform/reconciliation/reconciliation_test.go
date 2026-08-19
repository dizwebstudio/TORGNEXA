package reconciliation

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/syncengine"
)

const (
	orgID     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	wsID      = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	policyID  = "sync_policy_014"
	accountID = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
)

var now014 = time.Date(2026, 8, 10, 15, 0, 0, 0, time.UTC)

type fakeClock struct{ t time.Time }

func (c fakeClock) Now() time.Time { return c.t }

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) NewID(prefix string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return prefix + "test_" + string(rune('a'+s.n)), nil
}
func scope014(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope(orgID, wsID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func syncPolicy(source syncengine.SourceOfTruth, dir syncengine.Direction) syncengine.Policy {
	return syncengine.Policy{ID: policyID, OrganizationID: orgID, WorkspaceID: wsID, ConnectorAccountID: accountID, EntityType: "product", Direction: dir, SourceOfTruth: source, Enabled: true, Version: 1, CreatedAt: now014, UpdatedAt: now014}
}

type fakeSyncRepo struct{ p syncengine.Policy }

func (f *fakeSyncRepo) CreatePolicy(context.Context, tenancy.Scope, syncengine.PolicyCreate) (syncengine.Policy, error) {
	return syncengine.Policy{}, errors.New("unused")
}
func (f *fakeSyncRepo) UpdatePolicy(context.Context, tenancy.Scope, syncengine.PolicyUpdate) (syncengine.Policy, error) {
	return syncengine.Policy{}, errors.New("unused")
}
func (f *fakeSyncRepo) Policy(_ context.Context, _ tenancy.Scope, id string) (syncengine.Policy, error) {
	if id != f.p.ID {
		return syncengine.Policy{}, syncengine.ErrPolicyNotFound
	}
	return f.p, nil
}
func (f *fakeSyncRepo) Checkpoint(context.Context, tenancy.Scope, string) (syncengine.Checkpoint, error) {
	return syncengine.Checkpoint{}, errors.New("unused")
}
func (f *fakeSyncRepo) AdvanceCheckpoint(context.Context, tenancy.Scope, string, int64, string, time.Time) (syncengine.Checkpoint, error) {
	return syncengine.Checkpoint{}, errors.New("unused")
}
func (f *fakeSyncRepo) EntityState(context.Context, tenancy.Scope, string, string) (syncengine.EntityState, error) {
	return syncengine.EntityState{}, syncengine.ErrNotFound
}
func (f *fakeSyncRepo) SaveEntityState(context.Context, tenancy.Scope, syncengine.EntityState, int64) (syncengine.EntityState, error) {
	return syncengine.EntityState{}, errors.New("unused")
}
func (f *fakeSyncRepo) LocalReceipt(context.Context, tenancy.Scope, string, string) (syncengine.Receipt, error) {
	return syncengine.Receipt{}, syncengine.ErrNotFound
}
func (f *fakeSyncRepo) RecordLocalReceipt(context.Context, tenancy.Scope, syncengine.Receipt) error {
	return errors.New("unused")
}
func (f *fakeSyncRepo) RemoteReceipt(context.Context, tenancy.Scope, string, string) (syncengine.Receipt, error) {
	return syncengine.Receipt{}, syncengine.ErrNotFound
}
func (f *fakeSyncRepo) RecordRemoteReceipt(context.Context, tenancy.Scope, syncengine.Receipt) error {
	return errors.New("unused")
}

type memRepo struct {
	mu      sync.Mutex
	runs    map[string]Run
	drifts  map[string]Drift
	actions []ActionRecord
}

func newMemRepo() *memRepo { return &memRepo{runs: map[string]Run{}, drifts: map[string]Drift{}} }
func (r *memRepo) CreateRun(_ context.Context, _ tenancy.Scope, v Run) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[v.ID]; ok {
		return Run{}, ErrConflict
	}
	r.runs[v.ID] = v
	return v, nil
}
func (r *memRepo) Run(_ context.Context, _ tenancy.Scope, id string) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.runs[id]
	if !ok {
		return Run{}, ErrNotFound
	}
	return v, nil
}
func (r *memRepo) UpdateRun(_ context.Context, _ tenancy.Scope, v Run, expected int64) (Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.runs[v.ID]
	if !ok {
		return Run{}, ErrNotFound
	}
	if old.Version != expected {
		return Run{}, ErrConflict
	}
	v.Version = old.Version + 1
	r.runs[v.ID] = v
	return v, nil
}
func (r *memRepo) RecordDrift(_ context.Context, _ tenancy.Scope, v Drift) (Drift, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.drifts[v.ID]; ok {
		return old, false, nil
	}
	r.drifts[v.ID] = v
	return v, true, nil
}
func (r *memRepo) Drift(_ context.Context, _ tenancy.Scope, id string) (Drift, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.drifts[id]
	if !ok {
		return Drift{}, ErrNotFound
	}
	return v, nil
}
func (r *memRepo) UpdateDrift(_ context.Context, _ tenancy.Scope, v Drift, expected int64) (Drift, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.drifts[v.ID]
	if !ok {
		return Drift{}, ErrNotFound
	}
	if old.Version != expected {
		return Drift{}, ErrConflict
	}
	v.Version = old.Version + 1
	r.drifts[v.ID] = v
	return v, nil
}
func (r *memRepo) ListDrifts(_ context.Context, _ tenancy.Scope, runID string, limit int) ([]Drift, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Drift{}
	for _, v := range r.drifts {
		if v.RunID == runID {
			out = append(out, v)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
func (r *memRepo) RecordAction(_ context.Context, _ tenancy.Scope, a ActionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.actions = append(r.actions, a)
	return nil
}
func (r *memRepo) ListActions(_ context.Context, _ tenancy.Scope, driftID string, limit int) ([]ActionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []ActionRecord{}
	for _, a := range r.actions {
		if a.DriftID == driftID {
			out = append(out, a)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

type pageSource struct {
	mu       sync.Mutex
	pages    map[string]ScanPage
	failOnce bool
}

func (s *pageSource) Scan(_ context.Context, _ tenancy.Scope, r ScanRequest) (ScanPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failOnce {
		s.failOnce = false
		return ScanPage{}, errors.New("scan failed")
	}
	p, ok := s.pages[r.Cursor]
	if !ok {
		return ScanPage{}, errors.New("cursor missing")
	}
	return p, nil
}

type actionSink struct {
	mu        sync.Mutex
	repairs   []RepairRequest
	notifies  []NotifyRequest
	approvals []ApprovalRequest
	fail      bool
}

func (s *actionSink) AutoFix(_ context.Context, _ tenancy.Scope, r RepairRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.repairs = append(s.repairs, r)
	if s.fail {
		return errors.New("failed")
	}
	return nil
}
func (s *actionSink) Notify(_ context.Context, _ tenancy.Scope, r NotifyRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifies = append(s.notifies, r)
	if s.fail {
		return errors.New("failed")
	}
	return nil
}
func (s *actionSink) RequestApproval(_ context.Context, _ tenancy.Scope, r ApprovalRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.approvals = append(s.approvals, r)
	if s.fail {
		return errors.New("failed")
	}
	return nil
}
func fp(ch byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = ch
	}
	return string(b)
}
func baseSubject() Subject {
	return Subject{LocalEntityID: "local_014", RemoteID: "remote-014", LocalPresent: true, RemotePresent: true, MappingLocalCount: 1, MappingRemoteCount: 1, LocalFingerprint: fp('a'), RemoteFingerprint: fp('b'), LocalStatus: "active", RemoteStatus: "paused", LocalVersion: 4, RemoteRevision: "rev-9", ObservedAt: now014}
}

func TestRunDetectsDriftAndSafeLocalAuthoritativeRepairs(t *testing.T) {
	repo := newMemRepo()
	sink := &actionSink{}
	ids := &seqIDs{}
	engine, _ := newEngine(&fakeSyncRepo{p: syncPolicy(syncengine.SourceLocal, syncengine.DirectionBidirectional)}, repo, sink, ids, fakeClock{now014})
	src := &pageSource{pages: map[string]ScanPage{"": {Subjects: []Subject{baseSubject()}, RemoteObservedAt: now014}}}
	actions := ActionPolicy{Content: ActionAutoFix, MissingMapping: ActionApproval, OrphanMapping: ActionApproval, DuplicateMapping: ActionApproval, StatusMismatch: ActionAutoFix, StaleConnector: ActionNotify}
	run, err := engine.Start(context.Background(), scope014(t), RunRequest{PolicyID: policyID, Mode: ModeOnDemand, TriggerRef: "user_014", Limit: 100, StaleAfter: 10 * time.Minute, Actions: actions}, src)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != RunCompleted || run.ScannedCount != 1 || run.DriftCount != 2 {
		t.Fatalf("run=%+v", run)
	}
	if len(sink.repairs) != 2 {
		t.Fatalf("repairs=%d", len(sink.repairs))
	}
	for _, r := range sink.repairs {
		if r.Direction != RepairLocalToRemote || r.IdempotencyKey == "" {
			t.Fatalf("repair=%+v", r)
		}
	}
	for _, d := range repo.drifts {
		if d.Status != DriftAutoFixed {
			t.Fatalf("drift=%+v", d)
		}
	}
}

func TestRunDetectsMappingClassesAndNeverAutoFixesAmbiguousMapping(t *testing.T) {
	repo := newMemRepo()
	sink := &actionSink{}
	engine, _ := newEngine(&fakeSyncRepo{p: syncPolicy(syncengine.SourceManual, syncengine.DirectionBidirectional)}, repo, sink, &seqIDs{}, fakeClock{now014})
	missing := baseSubject()
	missing.MappingLocalCount = 0
	missing.MappingRemoteCount = 0
	missing.CanAutoMap = true
	missing.LocalFingerprint = fp('a')
	missing.RemoteFingerprint = fp('a')
	missing.LocalStatus = "active"
	missing.RemoteStatus = "active"
	duplicate := baseSubject()
	duplicate.MappingLocalCount = 2
	duplicate.MappingRemoteCount = 1
	duplicate.LocalFingerprint = fp('a')
	duplicate.RemoteFingerprint = fp('a')
	duplicate.LocalStatus = "active"
	duplicate.RemoteStatus = "active"
	orphan := baseSubject()
	orphan.RemotePresent = false
	orphan.RemoteID = ""
	orphan.RemoteRevision = ""
	orphan.RemoteFingerprint = ""
	orphan.RemoteStatus = ""
	orphan.MappingLocalCount = 1
	orphan.MappingRemoteCount = 0
	src := &pageSource{pages: map[string]ScanPage{"": {Subjects: []Subject{missing, duplicate, orphan}, RemoteObservedAt: now014}}}
	actions := ActionPolicy{Content: ActionApproval, MissingMapping: ActionAutoFix, OrphanMapping: ActionApproval, DuplicateMapping: ActionApproval, StatusMismatch: ActionApproval, StaleConnector: ActionNotify}
	run, err := engine.Start(context.Background(), scope014(t), RunRequest{PolicyID: policyID, Mode: ModeScheduledFull, Limit: 10, StaleAfter: 10 * time.Minute, Actions: actions}, src)
	if err != nil {
		t.Fatal(err)
	}
	if run.DriftCount != 3 || len(sink.repairs) != 1 || sink.repairs[0].Direction != RepairCreateMapping || len(sink.approvals) != 2 {
		t.Fatalf("run=%+v repairs=%d approvals=%d", run, len(sink.repairs), len(sink.approvals))
	}
}

func TestStaleConnectorNotifiesAndRunResumeUsesPersistedCursor(t *testing.T) {
	repo := newMemRepo()
	sink := &actionSink{}
	engine, _ := newEngine(&fakeSyncRepo{p: syncPolicy(syncengine.SourceRemote, syncengine.DirectionBidirectional)}, repo, sink, &seqIDs{}, fakeClock{now014})
	page1 := ScanPage{Subjects: []Subject{baseSubject()}, NextCursor: "c2", HasMore: true, RemoteObservedAt: now014.Add(-time.Hour)}
	page2 := ScanPage{Subjects: nil, RemoteObservedAt: now014}
	src := &pageSource{pages: map[string]ScanPage{"": page1, "c2": page2}}
	// Fail on second call through wrapper state after the first page has persisted cursor.
	calls := 0
	wrapped := SourceFunc(func(ctx context.Context, sc tenancy.Scope, req ScanRequest) (ScanPage, error) {
		calls++
		if calls == 2 {
			return ScanPage{}, errors.New("temporary")
		}
		return src.Scan(ctx, sc, req)
	})
	actions := ActionPolicy{Content: ActionNone, MissingMapping: ActionNone, OrphanMapping: ActionNone, DuplicateMapping: ActionNone, StatusMismatch: ActionNone, StaleConnector: ActionNotify}
	run, err := engine.Start(context.Background(), scope014(t), RunRequest{PolicyID: policyID, Mode: ModeIncremental, Limit: 10, StaleAfter: 5 * time.Minute, Actions: actions}, wrapped)
	if err == nil || run.Status != RunInterrupted || run.Cursor != "c2" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	resumed, err := engine.Resume(context.Background(), scope014(t), run.ID, 5*time.Minute, actions, 10, src)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != RunCompleted || resumed.Cursor != "" {
		t.Fatalf("resumed=%+v", resumed)
	}
	if len(sink.notifies) != 1 {
		t.Fatalf("notifications=%d", len(sink.notifies))
	}
}

type SourceFunc func(context.Context, tenancy.Scope, ScanRequest) (ScanPage, error)

func (f SourceFunc) Scan(ctx context.Context, s tenancy.Scope, r ScanRequest) (ScanPage, error) {
	return f(ctx, s, r)
}

func TestUnsafeAutoFixFailsClosedAndRecordsNoSuccessfulTransition(t *testing.T) {
	repo := newMemRepo()
	sink := &actionSink{}
	engine, _ := newEngine(&fakeSyncRepo{p: syncPolicy(syncengine.SourceManual, syncengine.DirectionBidirectional)}, repo, sink, &seqIDs{}, fakeClock{now014})
	d := Drift{ID: "rcd_manual", RunID: "rec_manual", PolicyID: policyID, Kind: DriftContent, LocalEntityID: "local_014", RemoteID: "remote-014", LocalFingerprint: fp('a'), RemoteFingerprint: fp('b'), LocalVersion: 1, RemoteRevision: "rev-1", MappingLocalCount: 1, MappingRemoteCount: 1, DetectedAt: now014, Status: DriftOpen, RecommendedAction: ActionApproval, Version: 1}
	repo.drifts[d.ID] = d
	if _, err := engine.ApplyAction(context.Background(), scope014(t), d.ID, ActionAutoFix); !errors.Is(err, ErrActionUnsafe) {
		t.Fatalf("err=%v", err)
	}
	got, _ := repo.Drift(context.Background(), scope014(t), d.ID)
	if got.Status != DriftOpen || len(repo.actions) != 0 {
		t.Fatalf("drift=%+v actions=%d", got, len(repo.actions))
	}
}

func TestActionFailureIsRecordedWithMachineCodeOnly(t *testing.T) {
	repo := newMemRepo()
	sink := &actionSink{fail: true}
	engine, _ := newEngine(&fakeSyncRepo{p: syncPolicy(syncengine.SourceLocal, syncengine.DirectionBidirectional)}, repo, sink, &seqIDs{}, fakeClock{now014})
	d := Drift{ID: "rcd_notify", RunID: "rec_notify", PolicyID: policyID, Kind: DriftStaleConnector, DetectedAt: now014, Status: DriftOpen, RecommendedAction: ActionNotify, Version: 1}
	repo.drifts[d.ID] = d
	if _, err := engine.ApplyAction(context.Background(), scope014(t), d.ID, ActionNotify); err == nil {
		t.Fatal("expected error")
	}
	if len(repo.actions) != 1 || repo.actions[0].Result != ActionFailed || repo.actions[0].ErrorCode != "executor_failed" {
		t.Fatalf("actions=%+v", repo.actions)
	}
	got, _ := repo.Drift(context.Background(), scope014(t), d.ID)
	if got.Status != DriftOpen {
		t.Fatalf("drift=%+v", got)
	}
}

func TestReplayOfOpenDriftRetriesActionWithStableIdempotencyKey(t *testing.T) {
	repo := newMemRepo()
	sink := &actionSink{}
	engine, _ := newEngine(&fakeSyncRepo{p: syncPolicy(syncengine.SourceLocal, syncengine.DirectionBidirectional)}, repo, sink, &seqIDs{}, fakeClock{now014})
	run := Run{ID: "rec_replay_014", PolicyID: policyID, Mode: ModeOnDemand, Status: RunRunning, Version: 1, StartedAt: now014, UpdatedAt: now014}
	s := baseSubject()
	d := engine.newDrift(run, DriftContent, s, ActionAutoFix)
	repo.drifts[d.ID] = d
	inserted, err := engine.persistAndAct(context.Background(), scope014(t), syncPolicy(syncengine.SourceLocal, syncengine.DirectionBidirectional), d, s, ActionAutoFix)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("existing drift must not increment run count")
	}
	if len(sink.repairs) != 1 {
		t.Fatalf("repairs=%d", len(sink.repairs))
	}
	got, _ := repo.Drift(context.Background(), scope014(t), d.ID)
	if got.Status != DriftAutoFixed {
		t.Fatalf("drift=%+v", got)
	}
}

func TestIgnoreIsDurableActionEvidence(t *testing.T) {
	repo := newMemRepo()
	engine, _ := newEngine(&fakeSyncRepo{p: syncPolicy(syncengine.SourceManual, syncengine.DirectionBidirectional)}, repo, nil, &seqIDs{}, fakeClock{now014})
	d := Drift{ID: "rcd_ignore_014", RunID: "rec_ignore_014", PolicyID: policyID, Kind: DriftDuplicateMapping, MappingLocalCount: 2, MappingRemoteCount: 1, DetectedAt: now014, Status: DriftOpen, RecommendedAction: ActionIgnore, Version: 1}
	repo.drifts[d.ID] = d
	got, err := engine.ApplyAction(context.Background(), scope014(t), d.ID, ActionIgnore)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != DriftIgnored || len(repo.actions) != 1 || repo.actions[0].Action != ActionIgnore || repo.actions[0].Result != ActionSucceeded {
		t.Fatalf("got=%+v actions=%+v", got, repo.actions)
	}
}
