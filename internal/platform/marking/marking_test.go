package marking

import (
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func scope(t *testing.T) tenancy.Scope {
	s, e := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestReconciliationUsesRemoteAuthoritativeStatusAndStoresNoRawCode(t *testing.T) {
	s := NewStore()
	sc := scope(t)
	f := StatusFact{CodeFingerprint: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", GTIN: "04601234567890", RemoteStatus: "retired", SourceRef: "crpt:req-1", ObservedAt: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC)}
	if e := s.Append(sc, f); e != nil {
		t.Fatal(e)
	}
	r, e := s.Reconcile(sc, "rec-1", f.CodeFingerprint, "circulation", time.Now().UTC())
	if e != nil || r.Outcome != "drift" || r.RemoteStatus != "retired" {
		t.Fatalf("%+v %v", r, e)
	}
	if got := s.Facts(sc)[0]; got.CodeFingerprint == "" || got.SourceRef == "" {
		t.Fatal("missing minimized evidence")
	}
}
