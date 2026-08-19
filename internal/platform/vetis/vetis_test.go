package vetis

import (
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"testing"
	"time"
)

func TestTenantIsolation(t *testing.T) {
	a, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW")
	b, _ := tenancy.ParseScope("01ARZ3NDEKTSV4RRFFQ69G5FAX", "01ARZ3NDEKTSV4RRFFQ69G5FAY")
	s := NewStore()
	_ = s.Append(a, Evidence{RemoteID: "d", Kind: "vetd", RemoteStatus: "completed", SourceRequestRef: "req", ObservedAt: time.Now().UTC()})
	if len(s.List(a)) != 1 || len(s.List(b)) != 0 {
		t.Fatal("tenant leak")
	}
}
