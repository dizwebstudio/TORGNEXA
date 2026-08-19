package syncengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	testOrg     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWS      = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	testAccount = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003"
	localID     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0010"
)

var fixedNow = time.Date(2026, 8, 10, 13, 0, 0, 0, time.UTC)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func mustScope(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope(testOrg, testWS)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func policyFixture(id string, direction Direction, source SourceOfTruth) Policy {
	return Policy{ID: id, OrganizationID: testOrg, WorkspaceID: testWS, ConnectorAccountID: testAccount, EntityType: "product", Direction: direction, SourceOfTruth: source, Enabled: true, Version: 1, CreatedAt: fixedNow, UpdatedAt: fixedNow}
}

func localMutationFixture(eventID string, version int64) LocalMutation {
	return LocalMutation{EventID: eventID, EntityType: "product", LocalEntityID: localID, LocalVersion: version, Operation: OperationUpsert, Payload: json.RawMessage(`{"code":"SKU-1","title":"One"}`), Source: "catalog", CorrelationID: "corr_013", OccurredAt: fixedNow}
}

func remoteMutationFixture(changeID, revision string) RemoteMutation {
	return RemoteMutation{ChangeID: changeID, EntityType: "product", RemoteID: "remote-product-1", Revision: revision, Operation: OperationUpsert, Payload: json.RawMessage(`{"code":"SKU-1","title":"One remote"}`), OccurredAt: fixedNow}
}

type memoryRepo struct {
	mu             sync.Mutex
	policies       map[string]Policy
	checkpoints    map[string]Checkpoint
	states         map[string]EntityState
	localReceipts  map[string]Receipt
	remoteReceipts map[string]Receipt
	failStateOnce  bool
}

func newMemoryRepo(policies ...Policy) *memoryRepo {
	r := &memoryRepo{policies: map[string]Policy{}, checkpoints: map[string]Checkpoint{}, states: map[string]EntityState{}, localReceipts: map[string]Receipt{}, remoteReceipts: map[string]Receipt{}}
	for _, p := range policies {
		r.policies[p.ID] = p
		r.checkpoints[p.ID] = Checkpoint{PolicyID: p.ID, Version: 1, UpdatedAt: fixedNow}
	}
	return r
}
func key(policy, id string) string { return policy + "|" + id }
func (r *memoryRepo) CreatePolicy(_ context.Context, s tenancy.Scope, c PolicyCreate) (Policy, error) {
	if !s.Valid() || c.Validate() != nil {
		return Policy{}, ErrInvalidRecord
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.policies[c.ID]; ok {
		return Policy{}, ErrPolicyConflict
	}
	p := Policy{ID: c.ID, OrganizationID: s.OrganizationID().String(), WorkspaceID: s.WorkspaceID().String(), ConnectorAccountID: c.ConnectorAccountID, EntityType: c.EntityType, Direction: c.Direction, SourceOfTruth: c.SourceOfTruth, Enabled: c.Enabled, Version: 1, CreatedAt: fixedNow, UpdatedAt: fixedNow}
	r.policies[p.ID] = p
	r.checkpoints[p.ID] = Checkpoint{PolicyID: p.ID, Version: 1, UpdatedAt: fixedNow}
	return p, nil
}
func (r *memoryRepo) UpdatePolicy(_ context.Context, _ tenancy.Scope, u PolicyUpdate) (Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.policies[u.ID]
	if !ok {
		return Policy{}, ErrPolicyNotFound
	}
	if p.Version != u.ExpectedVersion {
		return Policy{}, ErrPolicyConflict
	}
	p.Direction = u.Direction
	p.SourceOfTruth = u.SourceOfTruth
	p.Enabled = u.Enabled
	p.Version++
	p.UpdatedAt = fixedNow
	r.policies[u.ID] = p
	return p, nil
}
func (r *memoryRepo) Policy(_ context.Context, _ tenancy.Scope, id string) (Policy, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.policies[id]
	if !ok {
		return Policy{}, ErrPolicyNotFound
	}
	return p, nil
}
func (r *memoryRepo) Checkpoint(_ context.Context, _ tenancy.Scope, id string) (Checkpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.checkpoints[id]
	if !ok {
		return Checkpoint{}, ErrPolicyNotFound
	}
	return c, nil
}
func (r *memoryRepo) AdvanceCheckpoint(_ context.Context, _ tenancy.Scope, id string, expected int64, cursor string, at time.Time) (Checkpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.checkpoints[id]
	if c.Version != expected {
		return Checkpoint{}, ErrCheckpointConflict
	}
	c.Cursor = cursor
	c.Version++
	c.UpdatedAt = at
	r.checkpoints[id] = c
	return c, nil
}
func (r *memoryRepo) EntityState(_ context.Context, _ tenancy.Scope, policyID, local string) (EntityState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.states[key(policyID, local)]
	if !ok {
		return EntityState{}, ErrNotFound
	}
	return s, nil
}
func (r *memoryRepo) SaveEntityState(_ context.Context, _ tenancy.Scope, s EntityState, expected int64) (EntityState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failStateOnce {
		r.failStateOnce = false
		return EntityState{}, errors.New("synthetic state persistence failure")
	}
	k := key(s.PolicyID, s.LocalEntityID)
	old, ok := r.states[k]
	if expected == 0 && ok {
		return EntityState{}, ErrStateConflict
	}
	if expected > 0 && (!ok || old.Version != expected) {
		return EntityState{}, ErrStateConflict
	}
	s.Version = 1
	if ok {
		s.Version = old.Version + 1
	}
	r.states[k] = s
	return s, nil
}
func (r *memoryRepo) LocalReceipt(_ context.Context, _ tenancy.Scope, p, id string) (Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.localReceipts[key(p, id)]
	if !ok {
		return Receipt{}, ErrNotFound
	}
	return v, nil
}
func (r *memoryRepo) RecordLocalReceipt(_ context.Context, _ tenancy.Scope, v Receipt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.PolicyID, v.ChangeID)
	if old, ok := r.localReceipts[k]; ok {
		if old.Fingerprint != v.Fingerprint {
			return ErrReceiptCollision
		}
		return nil
	}
	r.localReceipts[k] = v
	return nil
}
func (r *memoryRepo) RemoteReceipt(_ context.Context, _ tenancy.Scope, p, id string) (Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.remoteReceipts[key(p, id)]
	if !ok {
		return Receipt{}, ErrNotFound
	}
	return v, nil
}
func (r *memoryRepo) RecordRemoteReceipt(_ context.Context, _ tenancy.Scope, v Receipt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(v.PolicyID, v.ChangeID)
	if old, ok := r.remoteReceipts[k]; ok {
		if old.Fingerprint != v.Fingerprint {
			return ErrReceiptCollision
		}
		return nil
	}
	r.remoteReceipts[k] = v
	return nil
}

type memoryMappings struct {
	mu       sync.Mutex
	byLocal  map[string]connectors.EntityMapping
	byRemote map[string]connectors.EntityMapping
}

func newMemoryMappings() *memoryMappings {
	return &memoryMappings{byLocal: map[string]connectors.EntityMapping{}, byRemote: map[string]connectors.EntityMapping{}}
}
func mapLocal(account, typ, local string) string   { return account + "|" + typ + "|" + local }
func mapRemote(account, typ, remote string) string { return account + "|" + typ + "|" + remote }
func (m *memoryMappings) UpsertMapping(_ context.Context, c connectors.MappingUpsert) (connectors.EntityMapping, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lk := mapLocal(c.ConnectorAccountID, c.EntityType, c.LocalEntityID)
	rk := mapRemote(c.ConnectorAccountID, c.EntityType, c.RemoteID)
	if _, ok := m.byLocal[lk]; ok {
		return connectors.EntityMapping{}, connectors.ErrMappingConflict
	}
	if _, ok := m.byRemote[rk]; ok {
		return connectors.EntityMapping{}, connectors.ErrMappingConflict
	}
	v := connectors.EntityMapping{OrganizationID: c.OrganizationID, WorkspaceID: c.WorkspaceID, ConnectorAccountID: c.ConnectorAccountID, EntityType: c.EntityType, LocalEntityID: c.LocalEntityID, RemoteID: c.RemoteID, Version: 1, CreatedAt: fixedNow, UpdatedAt: fixedNow}
	m.byLocal[lk] = v
	m.byRemote[rk] = v
	return v, nil
}
func (m *memoryMappings) MappingByLocal(_ context.Context, _, _, account, typ, local string) (connectors.EntityMapping, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.byLocal[mapLocal(account, typ, local)]
	if !ok {
		return connectors.EntityMapping{}, connectors.ErrMappingNotFound
	}
	return v, nil
}
func (m *memoryMappings) MappingByRemote(_ context.Context, _, _, account, typ, remote string) (connectors.EntityMapping, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.byRemote[mapRemote(account, typ, remote)]
	if !ok {
		return connectors.EntityMapping{}, connectors.ErrMappingNotFound
	}
	return v, nil
}

type fakeRemoteWriter struct {
	mu               sync.Mutex
	calls            []RemoteApplyRequest
	effects          int
	results          map[string]RemoteApplyResult
	conflictRevision string
	conflictOnce     bool
}

func (w *fakeRemoteWriter) ApplyLocal(_ context.Context, _ tenancy.Scope, r RemoteApplyRequest) (RemoteApplyResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, r)
	if w.conflictOnce && !r.Force {
		w.conflictOnce = false
		return RemoteApplyResult{}, &RemoteConflict{CurrentRevision: w.conflictRevision}
	}
	if w.results == nil {
		w.results = map[string]RemoteApplyResult{}
	}
	if v, ok := w.results[r.Metadata.IdempotencyKey]; ok {
		return v, nil
	}
	remoteID := r.RemoteID
	if remoteID == "" {
		remoteID = "remote-product-1"
	}
	v := RemoteApplyResult{RemoteID: remoteID, Revision: fmt.Sprintf("rev-%d", w.effects+1)}
	w.results[r.Metadata.IdempotencyKey] = v
	w.effects++
	return v, nil
}

type fakeReader struct {
	pages map[string]RemotePage
	calls int
}

func (r *fakeReader) Pull(_ context.Context, _ tenancy.Scope, q PullRequest) (RemotePage, error) {
	r.calls++
	p, ok := r.pages[q.Cursor]
	if !ok {
		return RemotePage{}, errors.New("unexpected cursor")
	}
	return p, nil
}

type fakeLocal struct {
	mu         sync.Mutex
	snapshots  map[string]LocalSnapshot
	applied    map[string]LocalApplyResult
	calls      []LocalApplyRequest
	effects    int
	failOnCall int
}

func (l *fakeLocal) Snapshot(_ context.Context, _ tenancy.Scope, _ string, id string) (LocalSnapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.snapshots[id]
	if !ok {
		return LocalSnapshot{}, errors.New("snapshot missing")
	}
	return v, nil
}
func (l *fakeLocal) ApplyRemote(_ context.Context, _ tenancy.Scope, r LocalApplyRequest) (LocalApplyResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, r)
	if l.failOnCall > 0 && len(l.calls) == l.failOnCall {
		return LocalApplyResult{}, errors.New("synthetic local failure")
	}
	if l.applied == nil {
		l.applied = map[string]LocalApplyResult{}
	}
	if v, ok := l.applied[r.IdempotencyKey]; ok {
		return v, nil
	}
	id := r.LocalEntityID
	if id == "" {
		id = localID
		if r.RemoteID == "remote-product-2" {
			id = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0011"
		}
	}
	fp, _ := PayloadFingerprint(r.Payload)
	version := int64(1)
	if r.ExpectedLocalVersion > 0 {
		version = r.ExpectedLocalVersion + 1
	}
	v := LocalApplyResult{LocalEntityID: id, Version: version, Fingerprint: fp}
	l.applied[r.IdempotencyKey] = v
	l.effects++
	if l.snapshots == nil {
		l.snapshots = map[string]LocalSnapshot{}
	}
	l.snapshots[id] = LocalSnapshot{LocalEntityID: id, Version: version, Fingerprint: fp}
	return v, nil
}

func seededState(t *testing.T, repo *memoryRepo, mappings *memoryMappings, policy Policy, localVersion int64, remoteRevision string, payload json.RawMessage) EntityState {
	t.Helper()
	scope := mustScope(t)
	_, err := mappings.UpsertMapping(context.Background(), connectors.MappingUpsert{OrganizationID: testOrg, WorkspaceID: testWS, ConnectorAccountID: testAccount, EntityType: "product", LocalEntityID: localID, RemoteID: "remote-product-1", ExpectedVersion: 0})
	if err != nil {
		t.Fatal(err)
	}
	fp, err := PayloadFingerprint(payload)
	if err != nil {
		t.Fatal(err)
	}
	s := EntityState{PolicyID: policy.ID, LocalEntityID: localID, RemoteID: "remote-product-1", LastLocalVersion: localVersion, LastRemoteRevision: remoteRevision, LastSyncedFingerprint: fp, LastLocalEventID: "evt_seed", LastRemoteChangeID: "chg_seed", UpdatedAt: fixedNow}
	s, err = repo.SaveEntityState(context.Background(), scope, s, 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPushLocalCreatesMappingAndPreservesCausation(t *testing.T) {
	p := policyFixture("sync_policy_out", DirectionBidirectional, SourceLocal)
	repo := newMemoryRepo(p)
	maps := newMemoryMappings()
	remote := &fakeRemoteWriter{}
	engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
	m := localMutationFixture("evt_local_013", 1)
	got, err := engine.PushLocal(context.Background(), mustScope(t), p.ID, m, remote)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomeApplied || remote.effects != 1 {
		t.Fatalf("result=%+v effects=%d", got, remote.effects)
	}
	if len(remote.calls) != 1 {
		t.Fatal("expected one remote call")
	}
	call := remote.calls[0]
	if call.Metadata.CorrelationID != "corr_013" || call.Metadata.CausationID != m.EventID || call.Metadata.OriginEventID != m.EventID || call.Metadata.Source != policySource(p.ID) {
		t.Fatalf("metadata=%+v", call.Metadata)
	}
	if _, err := maps.MappingByLocal(context.Background(), testOrg, testWS, testAccount, "product", localID); err != nil {
		t.Fatal(err)
	}
}

func TestPushLocalCrashRetryUsesSameRemoteIdempotencyKey(t *testing.T) {
	p := policyFixture("sync_policy_crash", DirectionOutbound, SourceLocal)
	repo := newMemoryRepo(p)
	repo.failStateOnce = true
	maps := newMemoryMappings()
	remote := &fakeRemoteWriter{}
	engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
	m := localMutationFixture("evt_crash_013", 1)
	if _, err := engine.PushLocal(context.Background(), mustScope(t), p.ID, m, remote); err == nil {
		t.Fatal("expected persistence failure")
	}
	got, err := engine.PushLocal(context.Background(), mustScope(t), p.ID, m, remote)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomeApplied || remote.effects != 1 || len(remote.calls) != 2 {
		t.Fatalf("got=%+v effects=%d calls=%d", got, remote.effects, len(remote.calls))
	}
	if remote.calls[0].Metadata.IdempotencyKey != remote.calls[1].Metadata.IdempotencyKey {
		t.Fatal("idempotency key changed across retry")
	}
}

func TestPullRemoteCrashBeforeCheckpointReplaysSafely(t *testing.T) {
	p := policyFixture("sync_policy_pull", DirectionInbound, SourceRemote)
	repo := newMemoryRepo(p)
	maps := newMemoryMappings()
	engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
	changes := []RemoteMutation{remoteMutationFixture("chg_pull_1", "r1"), {ChangeID: "chg_pull_2", EntityType: "product", RemoteID: "remote-product-2", Revision: "r1", Operation: OperationUpsert, Payload: json.RawMessage(`{"code":"SKU-2"}`), OccurredAt: fixedNow}}
	reader := &fakeReader{pages: map[string]RemotePage{"": {Changes: changes, NextCursor: "cursor-2", HasMore: true}, "cursor-2": {NextCursor: "cursor-2"}}}
	local := &fakeLocal{snapshots: map[string]LocalSnapshot{}, failOnCall: 2}
	if _, err := engine.PullRemote(context.Background(), mustScope(t), p.ID, reader, local, 10); err == nil {
		t.Fatal("expected second change failure")
	}
	cp, _ := repo.Checkpoint(context.Background(), mustScope(t), p.ID)
	if cp.Cursor != "" {
		t.Fatalf("checkpoint advanced on partial page: %+v", cp)
	}
	local.failOnCall = 0
	results, err := engine.PullRemote(context.Background(), mustScope(t), p.ID, reader, local, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Outcome != OutcomeDuplicate || results[1].Outcome != OutcomeApplied {
		t.Fatalf("results=%+v", results)
	}
	if local.effects != 2 {
		t.Fatalf("local effects=%d want 2", local.effects)
	}
	cp, _ = repo.Checkpoint(context.Background(), mustScope(t), p.ID)
	if cp.Cursor != "cursor-2" {
		t.Fatalf("checkpoint=%+v", cp)
	}
}

func TestInboundOriginAndFingerprintEchoAreLoopSuppressed(t *testing.T) {
	p := policyFixture("sync_policy_loop", DirectionBidirectional, SourceLocal)
	repo := newMemoryRepo(p)
	maps := newMemoryMappings()
	engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
	basePayload := json.RawMessage(`{"code":"SKU-1","title":"same"}`)
	seededState(t, repo, maps, p, 1, "r1", basePayload)
	local := &fakeLocal{snapshots: map[string]LocalSnapshot{localID: {LocalEntityID: localID, Version: 1, Fingerprint: mustFP(t, basePayload)}}}
	explicit := RemoteMutation{ChangeID: "chg_loop_origin", EntityType: "product", RemoteID: "remote-product-1", Revision: "r2", Operation: OperationUpsert, Payload: json.RawMessage(`{"code":"different"}`), OccurredAt: fixedNow, Origin: Origin{Source: policySource(p.ID), EventID: "evt_origin"}}
	reader := &fakeReader{pages: map[string]RemotePage{"": {Changes: []RemoteMutation{explicit}, NextCursor: "c1"}, "c1": {Changes: []RemoteMutation{{ChangeID: "chg_loop_fp", EntityType: "product", RemoteID: "remote-product-1", Revision: "r3", Operation: OperationUpsert, Payload: basePayload, OccurredAt: fixedNow}}, NextCursor: "c2"}}}
	res, err := engine.PullRemote(context.Background(), mustScope(t), p.ID, reader, local, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Outcome != OutcomeLoopSuppressed || local.effects != 0 {
		t.Fatalf("explicit loop result=%+v effects=%d", res, local.effects)
	}
	res, err = engine.PullRemote(context.Background(), mustScope(t), p.ID, reader, local, 10)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Outcome != OutcomeLoopSuppressed || local.effects != 0 {
		t.Fatalf("fingerprint loop result=%+v effects=%d", res, local.effects)
	}
}

func TestInboundConflictPolicies(t *testing.T) {
	for _, tc := range []struct {
		name      string
		truth     SourceOfTruth
		want      Outcome
		wantErr   bool
		overwrite bool
	}{{"local", SourceLocal, OutcomeLocalWins, false, false}, {"remote", SourceRemote, OutcomeApplied, false, true}, {"manual", SourceManual, "", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			p := policyFixture("sync_policy_conflict_"+tc.name, DirectionBidirectional, tc.truth)
			repo := newMemoryRepo(p)
			maps := newMemoryMappings()
			oldPayload := json.RawMessage(`{"code":"old"}`)
			seededState(t, repo, maps, p, 1, "r1", oldPayload)
			local := &fakeLocal{snapshots: map[string]LocalSnapshot{localID: {LocalEntityID: localID, Version: 2, Fingerprint: mustFP(t, json.RawMessage(`{"code":"local-new"}`))}}}
			engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
			reader := &fakeReader{pages: map[string]RemotePage{"": {Changes: []RemoteMutation{remoteMutationFixture("chg_conflict_"+tc.name, "r2")}, NextCursor: "c"}}}
			res, err := engine.PullRemote(context.Background(), mustScope(t), p.ID, reader, local, 10)
			if tc.wantErr {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("err=%v", err)
				}
				cp, _ := repo.Checkpoint(context.Background(), mustScope(t), p.ID)
				if cp.Cursor != "" {
					t.Fatal("manual conflict advanced checkpoint")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res[0].Outcome != tc.want {
				t.Fatalf("result=%+v", res[0])
			}
			if tc.truth == SourceLocal && local.effects != 0 {
				t.Fatal("local-wins applied remote")
			}
			if tc.truth == SourceRemote {
				if local.effects != 1 || !local.calls[0].Overwrite {
					t.Fatalf("calls=%+v", local.calls)
				}
			}
		})
	}
}

func TestOutboundRemoteConflictLocalRetriesForceOthersStop(t *testing.T) {
	for _, tc := range []struct {
		name    string
		truth   SourceOfTruth
		wantErr bool
	}{{"local", SourceLocal, false}, {"remote", SourceRemote, true}, {"manual", SourceManual, true}} {
		t.Run(tc.name, func(t *testing.T) {
			p := policyFixture("sync_policy_outconf_"+tc.name, DirectionOutbound, tc.truth)
			repo := newMemoryRepo(p)
			maps := newMemoryMappings()
			seededState(t, repo, maps, p, 1, "r1", json.RawMessage(`{"code":"old"}`))
			remote := &fakeRemoteWriter{conflictOnce: true, conflictRevision: "r2"}
			engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
			m := localMutationFixture("evt_outconf_"+tc.name, 2)
			_, err := engine.PushLocal(context.Background(), mustScope(t), p.ID, m, remote)
			if tc.wantErr {
				if !errors.Is(err, ErrConflict) {
					t.Fatalf("err=%v", err)
				}
				if remote.effects != 0 {
					t.Fatal("conflict unexpectedly changed remote")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(remote.calls) != 2 || !remote.calls[1].Force || remote.calls[1].ExpectedRemoteRevision != "r2" || remote.effects != 1 {
				t.Fatalf("calls=%+v effects=%d", remote.calls, remote.effects)
			}
		})
	}
}

func TestPolicyScopedLoopMarkerStillAllowsCrossConnectorPropagation(t *testing.T) {
	pa := policyFixture("sync_policy_a", DirectionBidirectional, SourceRemote)
	pb := policyFixture("sync_policy_b", DirectionBidirectional, SourceLocal)
	repo := newMemoryRepo(pa, pb)
	maps := newMemoryMappings()
	remote := &fakeRemoteWriter{}
	engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
	m := localMutationFixture("evt_cross_013", 1)
	m.Source = policySource(pa.ID)
	got, err := engine.PushLocal(context.Background(), mustScope(t), pb.ID, m, remote)
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome != OutcomeApplied || remote.effects != 1 {
		t.Fatalf("got=%+v effects=%d", got, remote.effects)
	}
}

func TestPayloadFingerprintCanonicalAndReceiptCollision(t *testing.T) {
	a := json.RawMessage(`{"b":2,"a":1}`)
	b := json.RawMessage(`{"a":1,"b":2}`)
	fa := mustFP(t, a)
	fb := mustFP(t, b)
	if fa != fb {
		t.Fatalf("canonical fingerprints differ %s %s", fa, fb)
	}
	p := policyFixture("sync_policy_collision", DirectionOutbound, SourceLocal)
	repo := newMemoryRepo(p)
	maps := newMemoryMappings()
	engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
	m := localMutationFixture("evt_collision", 1)
	fp, err := LocalMutationFingerprint(m)
	if err != nil {
		t.Fatal(err)
	}
	repo.localReceipts[key(p.ID, m.EventID)] = Receipt{PolicyID: p.ID, ChangeID: m.EventID, Fingerprint: fp[:63] + "0", Outcome: OutcomeApplied, CreatedAt: fixedNow}
	if _, err := engine.PushLocal(context.Background(), mustScope(t), p.ID, m, &fakeRemoteWriter{}); !errors.Is(err, ErrReceiptCollision) {
		t.Fatalf("err=%v", err)
	}
}

func mustFP(t *testing.T, p json.RawMessage) string {
	t.Helper()
	v, err := PayloadFingerprint(p)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRemoteReceiptCollisionBindsRevisionAndOrigin(t *testing.T) {
	p := policyFixture("sync_policy_remote_collision", DirectionInbound, SourceRemote)
	repo := newMemoryRepo(p)
	maps := newMemoryMappings()
	engine, _ := newEngine(repo, maps, fixedClock{fixedNow})
	first := remoteMutationFixture("chg_reused_013", "r1")
	fp, err := RemoteMutationFingerprint(first)
	if err != nil {
		t.Fatal(err)
	}
	repo.remoteReceipts[key(p.ID, first.ChangeID)] = Receipt{PolicyID: p.ID, ChangeID: first.ChangeID, Fingerprint: fp, Outcome: OutcomeApplied, CreatedAt: fixedNow}
	changed := first
	changed.Revision = "r2"
	reader := &fakeReader{pages: map[string]RemotePage{"": {Changes: []RemoteMutation{changed}, NextCursor: "c"}}}
	_, err = engine.PullRemote(context.Background(), mustScope(t), p.ID, reader, &fakeLocal{snapshots: map[string]LocalSnapshot{}}, 10)
	if !errors.Is(err, ErrReceiptCollision) {
		t.Fatalf("err=%v", err)
	}
}

func TestDirectionAndDisabledPolicyFailClosed(t *testing.T) {
	inbound := policyFixture("sync_policy_in_only", DirectionInbound, SourceRemote)
	disabled := policyFixture("sync_policy_disabled", DirectionOutbound, SourceLocal)
	disabled.Enabled = false
	repo := newMemoryRepo(inbound, disabled)
	engine, _ := newEngine(repo, newMemoryMappings(), fixedClock{fixedNow})
	if _, err := engine.PushLocal(context.Background(), mustScope(t), inbound.ID, localMutationFixture("evt_direction", 1), &fakeRemoteWriter{}); !errors.Is(err, ErrDirectionDisabled) {
		t.Fatalf("inbound-only push err=%v", err)
	}
	if _, err := engine.PushLocal(context.Background(), mustScope(t), disabled.ID, localMutationFixture("evt_disabled", 1), &fakeRemoteWriter{}); !errors.Is(err, ErrDirectionDisabled) {
		t.Fatalf("disabled push err=%v", err)
	}
}
