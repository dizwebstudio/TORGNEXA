package pim

import (
	"encoding/json"
	"testing"
	"time"
)

const org = "018f0000-0000-7000-8000-000000000001"
const ws = "018f0000-0000-7000-8000-000000000002"
const id1 = "018f0000-0000-7000-8000-000000000101"
const id2 = "018f0000-0000-7000-8000-000000000102"

func mustID(t *testing.T, v string) ID {
	t.Helper()
	id, e := ParseID(v)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func TestMergePreviewAuthorityAndConflict(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	target := MasterSnapshot{OrganizationID: org, WorkspaceID: ws, EntityType: EntityBrand, EntityID: mustID(t, id1), Source: "erp.master", Version: "4", Fields: map[string]string{"name": "Acme", "country": "DE"}}
	source := MasterSnapshot{OrganizationID: org, WorkspaceID: ws, EntityType: EntityBrand, EntityID: mustID(t, id2), Source: "import.feed", Version: "2", Fields: map[string]string{"name": "ACME", "country": "DE", "website": "https://example.test"}}
	aid := mustID(t, "018f0000-0000-7000-8000-000000000103")
	authority := FieldAuthority{ID: aid, OrganizationID: org, WorkspaceID: ws, EntityType: EntityBrand, FieldPath: "name", Source: "erp.master", Priority: 100, Version: 1, Active: true, CreatedAt: now, UpdatedAt: now}
	p, err := BuildMergePreview(target, source, []FieldAuthority{authority})
	if err != nil {
		t.Fatal(err)
	}
	if p.HasConflicts {
		t.Fatal("unexpected conflict")
	}
	if len(p.Fields) != 3 || p.Fields[1].FieldPath != "name" || p.Fields[1].Decision != MergeKeepTarget {
		t.Fatalf("preview=%#v", p)
	}
	again, _ := BuildMergePreview(target, source, []FieldAuthority{authority})
	if p.FingerprintSHA256 != again.FingerprintSHA256 || p.ID != again.ID {
		t.Fatal("preview not deterministic")
	}
	p2, err := BuildMergePreview(target, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p2.HasConflicts {
		t.Fatal("expected conflict")
	}
}
func TestTypedAttributeValueRejectsFloatDecimal(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	v := AttributeValue{OrganizationID: org, WorkspaceID: ws, ProductID: mustID(t, id1), AttributeID: mustID(t, id2), Value: json.RawMessage(`"12.345"`), Source: "import.feed", Version: 1, Active: true, CreatedAt: now, UpdatedAt: now}
	if err := v.Validate(ValueDecimal, false); err != nil {
		t.Fatal(err)
	}
	v.Value = json.RawMessage(`12.345`)
	if err := v.Validate(ValueDecimal, false); err == nil {
		t.Fatal("JSON number decimal must be rejected")
	}
}
func TestDuplicateCandidateIsExplainable(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	d := DuplicateCandidate{ID: mustID(t, "018f0000-0000-7000-8000-000000000105"), OrganizationID: org, WorkspaceID: ws, EntityType: EntityBrand, LeftID: mustID(t, id1), RightID: mustID(t, id2), ScoreBPS: 9100, Signals: []DuplicateSignal{{Kind: "normalized_name", Explanation: "normalized names match", WeightBPS: 9100}}, State: DuplicateOpen, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestMergePreviewIsTenantBound(t *testing.T) {
	target := MasterSnapshot{OrganizationID: org, WorkspaceID: ws, EntityType: EntityBrand, EntityID: mustID(t, id1), Source: "erp.master", Version: "1", Fields: map[string]string{"name": "Acme"}}
	source := MasterSnapshot{OrganizationID: "018f0000-0000-7000-8000-000000000011", WorkspaceID: ws, EntityType: EntityBrand, EntityID: mustID(t, id2), Source: "import.feed", Version: "1", Fields: map[string]string{"name": "ACME"}}
	if _, err := BuildMergePreview(target, source, nil); err == nil {
		t.Fatal("cross-tenant preview must be rejected")
	}
	source.OrganizationID = org
	p, err := BuildMergePreview(target, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if p.OrganizationID != org || p.WorkspaceID != ws {
		t.Fatal("preview lost tenant binding")
	}
}
