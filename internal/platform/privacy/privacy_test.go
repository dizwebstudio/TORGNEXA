package privacy

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

const (
	testOrg = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	testWS  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
)

func TestClassRegistryPolicy(t *testing.T) {
	for _, class := range []DataClass{ClassPublic, ClassInternal, ClassConfidential, ClassPersonal, ClassSensitiveOperational, ClassSecret} {
		if !class.Valid() {
			t.Fatalf("class %q invalid", class)
		}
	}
	secret, _ := ClassSecret.Metadata()
	if secret.Logs != HandlingForbid || secret.Events != HandlingForbid || secret.Analytics != HandlingForbid || !secret.Secret {
		t.Fatalf("secret metadata = %#v", secret)
	}
	personal, _ := ClassPersonal.Metadata()
	if !personal.PII || personal.Logs != HandlingRedact {
		t.Fatalf("personal metadata = %#v", personal)
	}
}

func TestServicePurposeAndRetentionLifecycle(t *testing.T) {
	scope := mustScope(t)
	repo := newMemoryRepository()
	clock := fixedClock{value: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)}
	service, err := newService(repo, clock)
	if err != nil {
		t.Fatal(err)
	}
	purpose, err := service.RegisterPurpose(context.Background(), scope, PurposeSpec{Key: "order_fulfillment", Description: "Fulfil synthetic customer orders", LegalBasis: BasisContract, NoticeReference: "privacy-notice:v1", AllowedClasses: []DataClass{ClassSensitiveOperational, ClassPersonal}})
	if err != nil {
		t.Fatalf("RegisterPurpose() = %v", err)
	}
	if purpose.Version != 1 || !reflect.DeepEqual(purpose.AllowedClasses, []DataClass{ClassPersonal, ClassSensitiveOperational}) {
		t.Fatalf("purpose = %#v", purpose)
	}
	policy, err := service.SetRetention(context.Background(), scope, 0, RetentionSpec{PurposeKey: purpose.Key, DataClass: ClassPersonal, RetentionDays: 365, Disposition: DispositionAnonymize, LegalHoldOK: true})
	if err != nil {
		t.Fatalf("SetRetention(create) = %v", err)
	}
	clock.value = clock.value.Add(time.Hour)
	policy, err = service.SetRetention(context.Background(), scope, 1, RetentionSpec{PurposeKey: purpose.Key, DataClass: ClassPersonal, RetentionDays: 730, Disposition: DispositionDelete, LegalHoldOK: true})
	if err != nil || policy.Version != 2 || policy.RetentionDays != 730 {
		t.Fatalf("SetRetention(update) = %#v, %v", policy, err)
	}
	clock.value = clock.value.Add(time.Hour)
	_, err = service.RevisePurpose(context.Background(), scope, 1, PurposeSpec{Key: purpose.Key, Description: purpose.Description, LegalBasis: BasisContract, NoticeReference: "privacy-notice:v1", AllowedClasses: []DataClass{ClassSensitiveOperational}})
	if !errors.Is(err, ErrInvalidPurpose) {
		t.Fatalf("purpose removed active retention class: %v", err)
	}
	retiredPolicy, err := service.RetireRetention(context.Background(), scope, purpose.Key, ClassPersonal, 2)
	if err != nil || retiredPolicy.Status != StatusRetired || retiredPolicy.Version != 3 {
		t.Fatalf("RetireRetention() = %#v, %v", retiredPolicy, err)
	}
	clock.value = clock.value.Add(time.Hour)
	retired, err := service.RetirePurpose(context.Background(), scope, purpose.Key, 1)
	if err != nil || retired.Status != StatusRetired || retired.Version != 2 {
		t.Fatalf("RetirePurpose() = %#v, %v", retired, err)
	}
	if _, err := service.SetRetention(context.Background(), scope, 2, RetentionSpec{PurposeKey: purpose.Key, DataClass: ClassPersonal, RetentionDays: 30, Disposition: DispositionDelete}); !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("retired purpose SetRetention() error = %v", err)
	}
}

func TestPurposeRequiresNoticeForPIIAndConsentReference(t *testing.T) {
	scope := mustScope(t)
	repo := newMemoryRepository()
	service, _ := newService(repo, fixedClock{value: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)})
	_, err := service.RegisterPurpose(context.Background(), scope, PurposeSpec{Key: "support_case", Description: "Support a synthetic case", LegalBasis: BasisContract, AllowedClasses: []DataClass{ClassPersonal}})
	if !errors.Is(err, ErrInvalidPurpose) {
		t.Fatalf("missing notice error = %v", err)
	}
	_, err = service.RegisterPurpose(context.Background(), scope, PurposeSpec{Key: "marketing", Description: "Synthetic opt-in marketing", LegalBasis: BasisConsent, NoticeReference: "notice:v1", AllowedClasses: []DataClass{ClassPersonal}})
	if !errors.Is(err, ErrInvalidPurpose) {
		t.Fatalf("missing consent reference error = %v", err)
	}
}

func TestRetentionRequiresAllowedClass(t *testing.T) {
	scope := mustScope(t)
	repo := newMemoryRepository()
	service, _ := newService(repo, fixedClock{value: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)})
	_, err := service.RegisterPurpose(context.Background(), scope, PurposeSpec{Key: "inventory", Description: "Inventory operations", LegalBasis: BasisLegitimateInterest, AllowedClasses: []DataClass{ClassInternal}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SetRetention(context.Background(), scope, 0, RetentionSpec{PurposeKey: "inventory", DataClass: ClassPersonal, RetentionDays: 30, Disposition: DispositionDelete})
	if !errors.Is(err, ErrInvalidRetention) {
		t.Fatalf("disallowed class error = %v", err)
	}
}

func TestRedactionHelpersDoNotLeakPII(t *testing.T) {
	fixtures := []struct{ key, value string }{
		{"customer_email", "synthetic.person@example.invalid"},
		{"full_name", "Synthetic Person"},
		{"phone_number", "+31 6 12345678"},
		{"client_ip", "203.0.113.42"},
		{"unknown", "synthetic.person@example.invalid"},
		{"Authorization", "Bearer synthetic-token"},
	}
	for _, fixture := range fixtures {
		redacted := RedactString(fixture.key, fixture.value)
		if redacted == fixture.value {
			t.Errorf("PII/secret leaked for %q", fixture.key)
		}
	}
	if got := RedactString("order_id", "order-42"); got != "order-42" {
		t.Fatalf("safe value redacted = %q", got)
	}
}

func mustScope(t *testing.T) tenancy.Scope {
	t.Helper()
	scope, err := tenancy.ParseScope(testOrg, testWS)
	if err != nil {
		t.Fatal(err)
	}
	return scope
}

type fixedClock struct{ value time.Time }

func (clock fixedClock) Now() time.Time { return clock.value }

type memoryRepository struct {
	purposes  map[string]Purpose
	retention map[string]RetentionPolicy
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{purposes: map[string]Purpose{}, retention: map[string]RetentionPolicy{}}
}
func (repo *memoryRepository) CreatePurpose(_ context.Context, _ tenancy.Scope, value Purpose) error {
	if _, ok := repo.purposes[value.Key]; ok {
		return ErrConflict
	}
	repo.purposes[value.Key] = value
	return nil
}
func (repo *memoryRepository) Purpose(_ context.Context, _ tenancy.Scope, key string) (Purpose, error) {
	value, ok := repo.purposes[key]
	if !ok {
		return Purpose{}, ErrNotFound
	}
	return value, nil
}
func (repo *memoryRepository) UpdatePurpose(_ context.Context, _ tenancy.Scope, value Purpose, expected uint64) error {
	current, ok := repo.purposes[value.Key]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expected {
		return ErrConflict
	}
	repo.purposes[value.Key] = value
	return nil
}
func retentionKey(key string, class DataClass) string { return key + "\x00" + string(class) }
func (repo *memoryRepository) ActiveRetentionClasses(_ context.Context, _ tenancy.Scope, key string) ([]DataClass, error) {
	var classes []DataClass
	for _, value := range repo.retention {
		if value.PurposeKey == key && value.Status == StatusActive {
			classes = append(classes, value.DataClass)
		}
	}
	return classes, nil
}
func (repo *memoryRepository) CreateRetention(_ context.Context, _ tenancy.Scope, value RetentionPolicy) error {
	key := retentionKey(value.PurposeKey, value.DataClass)
	if _, ok := repo.retention[key]; ok {
		return ErrConflict
	}
	repo.retention[key] = value
	return nil
}
func (repo *memoryRepository) Retention(_ context.Context, _ tenancy.Scope, key string, class DataClass) (RetentionPolicy, error) {
	value, ok := repo.retention[retentionKey(key, class)]
	if !ok {
		return RetentionPolicy{}, ErrNotFound
	}
	return value, nil
}
func (repo *memoryRepository) UpdateRetention(_ context.Context, _ tenancy.Scope, value RetentionPolicy, expected uint64) error {
	key := retentionKey(value.PurposeKey, value.DataClass)
	current, ok := repo.retention[key]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expected {
		return ErrConflict
	}
	repo.retention[key] = value
	return nil
}
