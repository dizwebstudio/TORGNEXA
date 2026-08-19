package legalparty

import (
	"testing"
	"time"
)

const (
	org = "018f0f50-1111-7111-8111-111111111111"
	ws  = "018f0f50-2222-7222-8222-222222222222"
	id1 = "018f0f50-3333-7333-8333-333333333333"
	id2 = "018f0f50-4444-7444-8444-444444444444"
)

func TestRussianIdentifiers(t *testing.T) {
	inn10 := makeINN10("770708389")
	if err := ValidateINNLegal(inn10); err != nil {
		t.Fatalf("INN10 %q: %v", inn10, err)
	}
	inn12 := makeINN12("5001007322")
	if err := ValidateINNIndividual(inn12); err != nil {
		t.Fatalf("INN12 %q: %v", inn12, err)
	}
	ogrn := makeOGRN("102770013219")
	if err := ValidateOGRN(ogrn); err != nil {
		t.Fatalf("OGRN %q: %v", ogrn, err)
	}
	ogrnip := makeOGRNIP("30450011600015")
	if err := ValidateOGRNIP(ogrnip); err != nil {
		t.Fatalf("OGRNIP %q: %v", ogrnip, err)
	}
	if err := ValidateKPP("773601001"); err != nil {
		t.Fatal(err)
	}
	if ValidateINNLegal(inn10[:9]+"0") == nil {
		t.Fatal("invalid checksum accepted")
	}
	if ValidateOGRN(ogrn[:12]+"0") == nil && ogrn[12] != '0' {
		t.Fatal("invalid OGRN checksum accepted")
	}
}

func TestLegalEntityAndIEValidate(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	le := LegalEntity{ID: ID(id1), OrganizationID: org, WorkspaceID: ws, Code: "acme-ru", LegalName: "ООО АКМЕ", ShortName: "АКМЕ", CountryCode: "RU", INN: makeINN10("770708389"), KPP: "773601001", OGRN: makeOGRN("102770013219"), Status: StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := le.Validate(); err != nil {
		t.Fatal(err)
	}
	ie := IndividualEntrepreneur{ID: ID(id2), OrganizationID: org, WorkspaceID: ws, Code: "ivanov", FullName: "Иванов Иван Иванович", CountryCode: "RU", INN: makeINN12("5001007322"), OGRNIP: makeOGRNIP("30450011600015"), Status: StatusDraft, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := ie.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDetectDuplicates(t *testing.T) {
	d, err := DetectDuplicates(PartyLegalEntity, ID(id1), ID(id2), "ООО Ромашка", "ООО  Ромашка", "123", "123", "456", "456")
	if err != nil {
		t.Fatal(err)
	}
	if d.ScoreBPS != 10000 || len(d.Signals) != 3 {
		t.Fatalf("candidate=%#v", d)
	}
	if _, err := DetectDuplicates(PartyLegalEntity, ID(id1), ID(id2), "A", "B", "1", "2", "3", "4"); err != ErrNotFound {
		t.Fatalf("err=%v", err)
	}
}

func TestContractAndAuthorityUTC(t *testing.T) {
	now := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	later := now.Add(24 * time.Hour)
	c := Contract{ID: ID(id1), OrganizationID: org, WorkspaceID: ws, CounterpartyID: ID(id2), Number: "42/2026", ContractType: "supply", ValidFrom: now, ValidUntil: &later, Status: ContractActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	a := AuthorityReference{ID: ID(id1), OrganizationID: org, WorkspaceID: ws, CounterpartyID: ID(id2), Type: AuthorityMChD, ReferenceNumber: "MCHD-42", IssuedAt: now, ExpiresAt: &later, Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
}

func makeINN10(base string) string {
	return base + string(byte('0'+checksumDigit(base, []int{2, 4, 10, 3, 5, 9, 4, 6, 8})))
}
func makeINN12(base10 string) string {
	d11 := checksumDigit(base10, []int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8})
	s := base10 + string(byte('0'+d11))
	d12 := checksumDigit(s, []int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8})
	return s + string(byte('0'+d12))
}
func makeOGRN(base12 string) string   { return base12 + string(byte('0'+modDecimal(base12, 11)%10)) }
func makeOGRNIP(base14 string) string { return base14 + string(byte('0'+modDecimal(base14, 13)%10)) }
