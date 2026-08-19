package retention

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/audit"
	"github.com/torgnexa/torgnexa/internal/platform/privacy"
)

var fixedNow = time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return fixedNow }

type fakeAuditor struct {
	mu      sync.Mutex
	actions []string
}

func (a *fakeAuditor) Capture(_ context.Context, _ tenancy.Scope, e audit.Entry) (audit.Record, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actions = append(a.actions, e.Action)
	return audit.Record{}, nil
}

type fakeStore struct {
	name      string
	class     StoreClass
	supported map[Action]bool
	mu        sync.Mutex
	calls     []Step
	failAt    int
	pages     []StepResult
}

func (s *fakeStore) Name() string           { return s.name }
func (s *fakeStore) Class() StoreClass      { return s.class }
func (s *fakeStore) Supports(a Action) bool { return s.supported[a] }
func (s *fakeStore) Step(_ context.Context, _ tenancy.Scope, step Step) (StepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, step)
	n := len(s.calls)
	if s.failAt == n {
		return StepResult{}, errors.New("temporary store failure")
	}
	idx := n - 1
	if s.failAt > 0 && n > s.failAt {
		idx--
	}
	if idx >= len(s.pages) {
		return StepResult{Done: true, Digest: EvidenceDigest(s.name, "done")}, nil
	}
	return s.pages[idx], nil
}

type memRepo struct {
	mu       sync.Mutex
	requests map[string]Request
	jobs     map[string]Job
	targets  map[string]map[string]Target
	holds    map[string]LegalHold
	evidence []Evidence
}

func newMemRepo() *memRepo {
	return &memRepo{requests: map[string]Request{}, jobs: map[string]Job{}, targets: map[string]map[string]Target{}, holds: map[string]LegalHold{}}
}
func (r *memRepo) CreateWorkflow(_ context.Context, _ tenancy.Scope, req *Request, j Job, targets []Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.jobs[j.ID]; ok {
		return ErrConflict
	}
	if req != nil {
		if _, ok := r.requests[req.ID]; ok {
			return ErrConflict
		}
		r.requests[req.ID] = *req
	}
	r.jobs[j.ID] = j
	r.targets[j.ID] = map[string]Target{}
	for _, t := range targets {
		r.targets[j.ID][t.Store] = t
	}
	return nil
}
func (r *memRepo) Job(_ context.Context, _ tenancy.Scope, id string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return v, nil
}
func (r *memRepo) Targets(_ context.Context, _ tenancy.Scope, id string) ([]Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.targets[id]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]Target, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Store < out[j].Store })
	return out, nil
}
func (r *memRepo) UpdateJob(_ context.Context, _ tenancy.Scope, j Job, expected uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.jobs[j.ID]
	if !ok {
		return ErrNotFound
	}
	if old.Version != expected || j.Version != expected+1 {
		return ErrConflict
	}
	r.jobs[j.ID] = j
	return nil
}
func (r *memRepo) CommitTargetPage(_ context.Context, _ tenancy.Scope, tg Target, expected uint64, e Evidence) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.targets[tg.JobID][tg.Store]
	if !ok {
		return ErrNotFound
	}
	if old.Version != expected || tg.Version != expected+1 {
		return ErrConflict
	}
	if ValidateEvidence(e) != nil || e.JobID != tg.JobID || e.Store != tg.Store {
		return ErrInvalid
	}
	r.targets[tg.JobID][tg.Store] = tg
	r.evidence = append(r.evidence, e)
	return nil
}
func (r *memRepo) UpdateRequest(_ context.Context, _ tenancy.Scope, req Request, expected uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	old, ok := r.requests[req.ID]
	if !ok {
		return ErrNotFound
	}
	if old.Version != expected || req.Version != expected+1 {
		return ErrConflict
	}
	r.requests[req.ID] = req
	return nil
}
func (r *memRepo) Request(_ context.Context, _ tenancy.Scope, id string) (Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.requests[id]
	if !ok {
		return Request{}, ErrNotFound
	}
	return v, nil
}
func (r *memRepo) PlaceHold(_ context.Context, _ tenancy.Scope, h LegalHold) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.holds[h.ID]; ok {
		return ErrConflict
	}
	r.holds[h.ID] = h
	return nil
}
func (r *memRepo) ActiveHolds(_ context.Context, _ tenancy.Scope, now time.Time) ([]LegalHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []LegalHold{}
	for _, h := range r.holds {
		if h.Active(now) {
			out = append(out, h)
		}
	}
	return out, nil
}
func (r *memRepo) ReleaseHold(_ context.Context, _ tenancy.Scope, id string, expected uint64, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	h, ok := r.holds[id]
	if !ok {
		return ErrNotFound
	}
	if h.Version != expected || h.ReleasedAt != nil {
		return ErrConflict
	}
	h.Version++
	h.ReleasedAt = &at
	r.holds[id] = h
	return nil
}

func scope(t *testing.T) tenancy.Scope {
	t.Helper()
	s, err := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func service(t *testing.T, repo *memRepo, stores ...Store) (*Service, *fakeAuditor) {
	t.Helper()
	a := &fakeAuditor{}
	s, err := newService(repo, stores, a, fixedClock{})
	if err != nil {
		t.Fatal(err)
	}
	return s, a
}

func TestSubjectDeletionPropagatesAcrossAuthoritativeDerivedAndObjectStores(t *testing.T) {
	repo := newMemRepo()
	mk := func(name string, class StoreClass) *fakeStore {
		return &fakeStore{name: name, class: class, supported: map[Action]bool{ActionDelete: true}, pages: []StepResult{{Done: true, Processed: 2, Digest: EvidenceDigest(name, "2")}}}
	}
	primary, clickhouse, objects := mk("postgres", StoreAuthoritative), mk("clickhouse", StoreDerived), mk("object-store", StoreObject)
	svc, a := service(t, repo, primary, clickhouse, objects)
	sc := scope(t)
	_, err := svc.CreateSubjectRequest(context.Background(), sc, SubjectRequestSpec{RequestID: "req-061-1", JobID: "job-061-1", Type: RequestDeletion, Subject: SubjectRef{Kind: "customer", OpaqueID: "customer_opaque_42"}})
	if err != nil {
		t.Fatal(err)
	}
	job, err := svc.Advance(context.Background(), sc, "job-061-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("status=%s", job.Status)
	}
	for _, st := range []*fakeStore{primary, clickhouse, objects} {
		if len(st.calls) != 1 {
			t.Fatalf("%s calls=%d", st.name, len(st.calls))
		}
	}
	req, _ := repo.Request(context.Background(), sc, "req-061-1")
	if req.Status != StatusCompleted {
		t.Fatalf("request=%s", req.Status)
	}
	if len(repo.evidence) != 3 {
		t.Fatalf("evidence=%d", len(repo.evidence))
	}
	if len(a.actions) < 2 || a.actions[0] != "privacy.workflow.created" || a.actions[len(a.actions)-1] != "privacy.workflow.completed" {
		t.Fatalf("audit=%v", a.actions)
	}
}

func TestResumeDoesNotAdvanceCursorAfterStoreFailure(t *testing.T) {
	repo := newMemRepo()
	st := &fakeStore{name: "postgres", class: StoreAuthoritative, supported: map[Action]bool{ActionExport: true}, failAt: 2, pages: []StepResult{{NextCursor: "c1", Processed: 10, Digest: EvidenceDigest("p1")}, {Done: true, Processed: 3, Digest: EvidenceDigest("p2"), ArtifactRef: "released:export-1"}}}
	svc, _ := service(t, repo, st)
	sc := scope(t)
	_, err := svc.CreateSubjectRequest(context.Background(), sc, SubjectRequestSpec{RequestID: "req-061-2", JobID: "job-061-2", Type: RequestExport, Subject: SubjectRef{Kind: "customer", OpaqueID: "opaque-2"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Advance(context.Background(), sc, "job-061-2", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Advance(context.Background(), sc, "job-061-2", 1); err == nil {
		t.Fatal("expected failure")
	}
	targets, _ := repo.Targets(context.Background(), sc, "job-061-2")
	if targets[0].Cursor != "c1" || targets[0].Processed != 10 {
		t.Fatalf("target after failure=%+v", targets[0])
	}
	job, err := svc.Advance(context.Background(), sc, "job-061-2", 3)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusCompleted {
		t.Fatalf("status=%s", job.Status)
	}
	if len(st.calls) != 3 || st.calls[1].Cursor != "c1" || st.calls[2].Cursor != "c1" {
		t.Fatalf("calls=%+v", st.calls)
	}
}

func TestLegalHoldBlocksDeletionUntilReleased(t *testing.T) {
	repo := newMemRepo()
	st := &fakeStore{name: "postgres", class: StoreAuthoritative, supported: map[Action]bool{ActionDelete: true}, pages: []StepResult{{Done: true, Digest: EvidenceDigest("delete")}}}
	svc, _ := service(t, repo, st)
	sc := scope(t)
	subject := SubjectRef{Kind: "customer", OpaqueID: "opaque-3"}
	if err := svc.PlaceLegalHold(context.Background(), sc, LegalHold{ID: "hold-061-1", SelectorKind: HoldSubject, Subject: subject, ReasonRef: "case:legal-17"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateSubjectRequest(context.Background(), sc, SubjectRequestSpec{RequestID: "req-061-3", JobID: "job-061-3", Type: RequestDeletion, Subject: subject}); err != nil {
		t.Fatal(err)
	}
	job, err := svc.Advance(context.Background(), sc, "job-061-3", 5)
	if !errors.Is(err, ErrLegalHold) || job.Status != StatusBlocked {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if len(st.calls) != 0 {
		t.Fatal("store must not be called while held")
	}
	if err := svc.ReleaseLegalHold(context.Background(), sc, "hold-061-1", 1); err != nil {
		t.Fatal(err)
	}
	job, err = svc.Advance(context.Background(), sc, "job-061-3", 5)
	if err != nil || job.Status != StatusCompleted {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestRetentionExpiryUsesPolicyAndHonorsHoldPermission(t *testing.T) {
	repo := newMemRepo()
	st := &fakeStore{name: "postgres", class: StoreAuthoritative, supported: map[Action]bool{ActionAnonymize: true}, pages: []StepResult{{Done: true, Digest: EvidenceDigest("anon")}}}
	svc, _ := service(t, repo, st)
	sc := scope(t)
	policy := privacy.RetentionPolicy{OrganizationID: sc.OrganizationID(), WorkspaceID: sc.WorkspaceID(), PurposeKey: "support.history", DataClass: privacy.ClassPersonal, RetentionDays: 30, Disposition: privacy.DispositionAnonymize, LegalHoldOK: false, Status: privacy.StatusActive, Version: 1, CreatedAt: fixedNow.Add(-time.Hour), UpdatedAt: fixedNow.Add(-time.Hour)}
	if err := svc.PlaceLegalHold(context.Background(), sc, LegalHold{ID: "hold-061-2", SelectorKind: HoldPurposeClass, PurposeKey: policy.PurposeKey, DataClass: policy.DataClass, ReasonRef: "case:18"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRetentionExpiry(context.Background(), sc, "job-061-4", policy); err != nil {
		t.Fatal(err)
	}
	job, err := svc.Advance(context.Background(), sc, "job-061-4", 3)
	if err != nil || job.Status != StatusCompleted {
		t.Fatalf("job=%+v err=%v", job, err)
	}
}

func TestTenantDeletionRequiresAuthoritativeTarget(t *testing.T) {
	repo := newMemRepo()
	derived := &fakeStore{name: "clickhouse", class: StoreDerived, supported: map[Action]bool{ActionTenantDelete: true}}
	svc, _ := service(t, repo, derived)
	if _, err := svc.CreateTenantDeletion(context.Background(), scope(t), "job-061-5"); !errors.Is(err, ErrNoTargets) {
		t.Fatalf("err=%v", err)
	}
}

func TestCorrectionRequiresReleasedArtifactReference(t *testing.T) {
	repo := newMemRepo()
	st := &fakeStore{name: "postgres", class: StoreAuthoritative, supported: map[Action]bool{ActionCorrect: true}, pages: []StepResult{{Done: true, Digest: EvidenceDigest("correct")}}}
	svc, _ := service(t, repo, st)
	sc := scope(t)
	_, err := svc.CreateSubjectRequest(context.Background(), sc, SubjectRequestSpec{RequestID: "req-061-4", JobID: "job-061-6", Type: RequestCorrection, Subject: SubjectRef{Kind: "customer", OpaqueID: "opaque-6"}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
	_, err = svc.CreateSubjectRequest(context.Background(), sc, SubjectRequestSpec{RequestID: "req-061-4", JobID: "job-061-6", Type: RequestCorrection, Subject: SubjectRef{Kind: "customer", OpaqueID: "opaque-6"}, CorrectionArtifactRef: "released:correction-061"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManualReviewRetentionNeverMutatesStores(t *testing.T) {
	repo := newMemRepo()
	st := &fakeStore{name: "postgres", class: StoreAuthoritative, supported: map[Action]bool{ActionDelete: true}}
	svc, _ := service(t, repo, st)
	sc := scope(t)
	policy := privacy.RetentionPolicy{OrganizationID: sc.OrganizationID(), WorkspaceID: sc.WorkspaceID(), PurposeKey: "regulated.records", DataClass: privacy.ClassPersonal, RetentionDays: 365, Disposition: privacy.DispositionManualReview, LegalHoldOK: true, Status: privacy.StatusActive, Version: 1, CreatedAt: fixedNow.Add(-time.Hour), UpdatedAt: fixedNow.Add(-time.Hour)}
	job, err := svc.CreateRetentionExpiry(context.Background(), sc, "job-061-manual", policy)
	if err != nil || job.Status != StatusBlocked || job.Action != ActionManualReview {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	job, err = svc.Advance(context.Background(), sc, job.ID, 1)
	if !errors.Is(err, ErrManualReview) || job.Status != StatusBlocked {
		t.Fatalf("job=%+v err=%v", job, err)
	}
	if len(st.calls) != 0 {
		t.Fatal("manual review must not mutate any store")
	}
}

func TestExportCannotCompleteWithoutArtifactEvidence(t *testing.T) {
	repo := newMemRepo()
	st := &fakeStore{name: "postgres", class: StoreAuthoritative, supported: map[Action]bool{ActionExport: true}, pages: []StepResult{{Done: true, Processed: 1, Digest: EvidenceDigest("export")}}}
	svc, _ := service(t, repo, st)
	sc := scope(t)
	_, err := svc.CreateSubjectRequest(context.Background(), sc, SubjectRequestSpec{RequestID: "req-061-export", JobID: "job-061-export", Type: RequestExport, Subject: SubjectRef{Kind: "customer", OpaqueID: "opaque-export"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Advance(context.Background(), sc, "job-061-export", 1); !errors.Is(err, ErrUnsafeResult) {
		t.Fatalf("err=%v", err)
	}
	targets, _ := repo.Targets(context.Background(), sc, "job-061-export")
	if targets[0].Version != 1 || targets[0].Status != TargetPending {
		t.Fatalf("target advanced on unsafe result: %+v", targets[0])
	}
}
