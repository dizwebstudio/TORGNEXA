// Command torgnexa-marketplace-live-smoke runs a bounded credentialed smoke
// against a dedicated non-production marketplace account. Provider secrets
// are read only from the process environment and are exposed to adapters only
// through the SDK SecretAccessor callback. The command emits redacted evidence
// and never writes provider payloads or credentials to disk.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/builtinruntime"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	smokeSecretReference = "sec:v1:0123456789abcdef0123456789abcdef"
	smokeAccountID       = "marketplace-live-smoke"
	smokeOrganizationID  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	smokeWorkspaceID     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	writeAcknowledgement = "I_UNDERSTAND_THIS_IS_NON_PRODUCTION"
)

var (
	sha40Pattern        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	categoryCodePattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	accountRefPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
)

// goldenPathFlow contains only opaque, redacted references. It is populated
// when the smoke is one artifact of a complete production golden path.
type goldenPathFlow struct {
	FlowRef        string `json:"flow_ref"`
	OrderRef       string `json:"order_ref"`
	ReservationRef string `json:"reservation_ref"`
	ShipmentRef    string `json:"shipment_ref"`
	ReturnRef      string `json:"return_ref"`
	RefundRef      string `json:"refund_ref"`
	SettlementRef  string `json:"settlement_ref"`
	MarkingRef     string `json:"marking_ref"`
	EDORef         string `json:"edo_ref"`
}

type config struct {
	Connector      string
	Environment    string
	Target         string
	AccountRef     string
	ReleaseCommit  string
	Output         string
	Scope          string
	Locale         string
	Jurisdiction   string
	CategoryCode   string
	WarehouseID    string
	VariantID      string
	Secret         string
	WriteInventory bool
	RunID          string
	Flow           *goldenPathFlow
}

type checkEvidence struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	EvidenceRef string `json:"evidence_ref"`
	ErrorCode   string `json:"error_code,omitempty"`
}

type taxonomyEvidence struct {
	Status      string `json:"status"`
	Fingerprint string `json:"fingerprint"`
	Source      string `json:"source"`
}

type writeEvidenceState struct {
	Attempted      bool `json:"attempted"`
	ReadAfterWrite bool `json:"read_after_write"`
	Restored       bool `json:"restored"`
}

type smokeEvidence struct {
	SchemaVersion  int                `json:"schema_version"`
	Status         string             `json:"status"`
	Scope          string             `json:"scope"`
	Environment    string             `json:"environment"`
	Target         string             `json:"target"`
	Repository     string             `json:"repository"`
	ReleaseCommit  string             `json:"release_commit"`
	ConnectorID    string             `json:"connector_id"`
	AccountRef     string             `json:"account_ref"`
	QualifiedAt    string             `json:"qualified_at"`
	CredentialMode string             `json:"credential_mode"`
	Taxonomy       taxonomyEvidence   `json:"taxonomy"`
	Checks         []checkEvidence    `json:"checks"`
	Write          writeEvidenceState `json:"write"`
	Flow           *goldenPathFlow    `json:"flow,omitempty"`
	Failure        *failureEvidence   `json:"failure,omitempty"`
}

type failureEvidence struct {
	CheckID   string `json:"check_id"`
	ErrorCode string `json:"error_code"`
}

type smokeFailure struct {
	CheckID   string
	ErrorCode string
}

func (failure *smokeFailure) Error() string {
	if failure == nil {
		return "marketplace live smoke failed"
	}
	return failure.CheckID + ": " + failure.ErrorCode
}

type secretRuntime struct {
	value string
}

func (runtime secretRuntime) Secrets() sdk.SecretAccessor {
	return secretAccessor{value: runtime.value}
}

type secretAccessor struct {
	value string
}

func (accessor secretAccessor) UseSecret(ctx context.Context, reference sdk.SecretReference, callback func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if reference != smokeSecretReference || accessor.value == "" || callback == nil {
		return errors.New("live smoke secret access denied")
	}
	value := []byte(accessor.value)
	defer clear(value)
	return callback(value)
}

func main() {
	var connector, output string
	flag.StringVar(&connector, "connector", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR"), "admitted marketplace connector ID")
	flag.StringVar(&output, "output", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_OUTPUT"), "absolute redacted evidence path")
	flag.Parse()

	cfg, err := loadConfig(connector, output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marketplace live smoke:", err)
		os.Exit(2)
	}
	cfg.RunID = strconv.FormatInt(time.Now().UTC().UnixNano(), 10)

	evidence := smokeEvidence{
		SchemaVersion:  1,
		Status:         "FAIL",
		Scope:          cfg.Scope,
		Environment:    cfg.Environment,
		Target:         cfg.Target,
		Repository:     os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_REPOSITORY"),
		ReleaseCommit:  cfg.ReleaseCommit,
		ConnectorID:    cfg.Connector,
		AccountRef:     cfg.AccountRef,
		QualifiedAt:    time.Now().UTC().Format(time.RFC3339),
		CredentialMode: "env_only_secret_accessor",
		Taxonomy:       taxonomyEvidence{Status: "NOT_RUN"},
		Checks:         []checkEvidence{},
	}
	if cfg.Flow != nil {
		evidence.SchemaVersion = 2
		evidence.Flow = cfg.Flow
	}
	if evidence.Repository == "" {
		evidence.Repository = "external-release-runner"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	smokeErr := runMarketplace(ctx, cfg, &evidence, builtinruntime.New())
	if smokeErr != nil {
		evidence.Failure = &failureEvidence{CheckID: smokeErr.CheckID, ErrorCode: smokeErr.ErrorCode}
	} else {
		evidence.Status = "PASS"
	}
	if writeErr := writeEvidence(cfg.Output, evidence); writeErr != nil {
		fmt.Fprintln(os.Stderr, "marketplace live smoke: cannot write evidence:", writeErr)
		os.Exit(1)
	}
	if smokeErr != nil {
		fmt.Fprintf(os.Stderr, "Marketplace %s live smoke: FAIL (%s: %s)\nevidence: %s\n", cfg.Connector, smokeErr.CheckID, smokeErr.ErrorCode, cfg.Output)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "Marketplace %s live smoke: PASS (%s scope)\nevidence: %s\n", cfg.Connector, cfg.Scope, cfg.Output)
}

func loadConfig(connector, output string) (config, error) {
	cfg := config{
		Connector:     strings.TrimSpace(connector),
		Environment:   strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ENVIRONMENT")),
		Target:        strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_TARGET")),
		AccountRef:    strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ACCOUNT_REF")),
		ReleaseCommit: strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_RELEASE_COMMIT")),
		Output:        strings.TrimSpace(output),
		Scope:         strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_SCOPE")),
		Locale:        valueOr(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_LOCALE"), "ru-RU"),
		Jurisdiction:  valueOr(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_JURISDICTION"), "RU"),
		CategoryCode:  strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_CATEGORY_CODE")),
		WarehouseID:   strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_WAREHOUSE_ID")),
		VariantID:     strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_VARIANT_ID")),
		Secret:        os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_SECRET"),
	}
	profile, ok := builtinruntime.MarketplaceSmokeProfileFor(cfg.Connector)
	if !ok {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR is not admitted for marketplace smoke")
	}
	if cfg.Environment != "non-production" {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_ENVIRONMENT must be non-production")
	}
	if cfg.Target != "dedicated-non-production" {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_TARGET must be dedicated-non-production")
	}
	if !accountRefPattern.MatchString(cfg.AccountRef) {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_ACCOUNT_REF must be a safe non-secret label")
	}
	if !sha40Pattern.MatchString(cfg.ReleaseCommit) {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_RELEASE_COMMIT must be a lowercase 40-hex release SHA")
	}
	if cfg.Output == "" || !filepath.IsAbs(cfg.Output) {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_OUTPUT must be an absolute path")
	}
	if cfg.Scope == "" {
		cfg.Scope = "qualification"
	}
	if cfg.Scope != "read" && cfg.Scope != "qualification" {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_SCOPE must be read or qualification")
	}
	if len(cfg.Locale) < 2 || len(cfg.Locale) > 16 || len(cfg.Jurisdiction) != 2 || cfg.Jurisdiction != strings.ToUpper(cfg.Jurisdiction) || !categoryCodePattern.MatchString(cfg.CategoryCode) {
		return config{}, errors.New("locale, jurisdiction and category code are required for taxonomy qualification")
	}
	cfg.WriteInventory = cfg.Scope == "qualification" && profile.InventoryWrite
	if cfg.WriteInventory && os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES") != writeAcknowledgement {
		return config{}, errors.New("inventory qualification requires TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES=" + writeAcknowledgement)
	}
	if !cfg.WriteInventory && os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES") != "" {
		return config{}, errors.New("write acknowledgement is not permitted for this smoke profile")
	}
	flow, err := loadGoldenPathFlow()
	if err != nil {
		return config{}, err
	}
	cfg.Flow = flow
	return cfg, nil
}

func loadGoldenPathFlow() (*goldenPathFlow, error) {
	values := []struct {
		name  string
		value string
	}{
		{"TORGNEXA_MARKETPLACE_SMOKE_FLOW_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_FLOW_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_ORDER_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ORDER_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_RESERVATION_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_RESERVATION_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_SHIPMENT_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_SHIPMENT_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_RETURN_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_RETURN_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_REFUND_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_REFUND_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_SETTLEMENT_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_SETTLEMENT_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_MARKING_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_MARKING_REF")},
		{"TORGNEXA_MARKETPLACE_SMOKE_EDO_REF", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_EDO_REF")},
	}
	anySet := false
	for _, value := range values {
		if strings.TrimSpace(value.value) != "" {
			anySet = true
			break
		}
	}
	if !anySet {
		return nil, nil
	}
	flow := &goldenPathFlow{}
	fields := []*string{
		&flow.FlowRef, &flow.OrderRef, &flow.ReservationRef, &flow.ShipmentRef,
		&flow.ReturnRef, &flow.RefundRef, &flow.SettlementRef, &flow.MarkingRef,
		&flow.EDORef,
	}
	for index := range values {
		value := strings.TrimSpace(values[index].value)
		if value == "" || accountRefPattern.FindString(value) != value {
			return nil, errors.New(values[index].name + " must be a safe non-secret golden path reference")
		}
		*fields[index] = value
	}
	return flow, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func smokeAccount(connectorID string) sdk.Account {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return sdk.Account{ID: smokeAccountID, OrganizationID: smokeOrganizationID, WorkspaceID: smokeWorkspaceID, ConnectorID: connectorID, Family: sdk.FamilyMarketplace, Status: sdk.AccountActive, SecretReference: smokeSecretReference, Version: 1, Health: sdk.Health{Status: sdk.HealthUnknown}, CreatedAt: created, UpdatedAt: created}
}

func runMarketplace(ctx context.Context, cfg config, evidence *smokeEvidence, registry *builtinruntime.Registry) *smokeFailure {
	if cfg.Secret == "" {
		return &smokeFailure{CheckID: "configuration", ErrorCode: "secret_missing"}
	}
	if registry == nil {
		return &smokeFailure{CheckID: "configuration", ErrorCode: "runtime_unavailable"}
	}
	account := smokeAccount(cfg.Connector)
	runtime := secretRuntime{value: cfg.Secret}
	for attempt := 0; attempt < 2; attempt++ {
		health, err := registry.Health(ctx, account, runtime, nil)
		if err != nil {
			return connectorFailure("health", err)
		}
		if health.Status != sdk.HealthHealthy {
			return &smokeFailure{CheckID: "health", ErrorCode: valueOr(health.ReasonCode, "not_healthy")}
		}
	}
	pass(evidence, "health")

	productsReader, err := registry.MarketplaceProductReader(account, runtime)
	if err != nil {
		return connectorFailure("products_read", err)
	}
	products, err := productsReader.ReadProducts(ctx, account, runtime, sdk.PageRequest{Limit: 10})
	if err != nil {
		return connectorFailure("products_read", err)
	}
	product, variant, ok := selectProduct(products.Items, cfg.VariantID)
	if !ok {
		return &smokeFailure{CheckID: "products_read", ErrorCode: "test_product_variant_missing"}
	}
	pass(evidence, "products_read")

	inventoryReader, err := registry.InventoryReader(account, runtime, nil)
	if err != nil {
		return connectorFailure("inventory_locations_read", err)
	}
	locations, err := inventoryReader.ListInventoryLocations(ctx, account, runtime)
	if err != nil {
		return connectorFailure("inventory_locations_read", err)
	}
	location, ok := selectLocation(locations, cfg.WarehouseID)
	if !ok {
		return &smokeFailure{CheckID: "inventory_locations_read", ErrorCode: "test_warehouse_missing"}
	}
	pass(evidence, "inventory_locations_read")

	readInventory := func() ([]sdk.RemoteInventory, *smokeFailure) {
		items, readErr := inventoryReader.ReadInventory(ctx, account, runtime, sdk.InventoryQuery{LocationRemoteID: location.RemoteID, VariantRemoteIDs: []string{variant.RemoteID}})
		if readErr != nil {
			return nil, connectorFailure("inventory_read", readErr)
		}
		if len(items) != 1 {
			return nil, &smokeFailure{CheckID: "inventory_read", ErrorCode: "stock_observation_missing"}
		}
		return items, nil
	}
	initial, failure := readInventory()
	if failure != nil {
		return failure
	}
	pass(evidence, "inventory_read")

	ordersReader, err := registry.OrderReader(ctx, account, runtime, nil)
	if err != nil {
		return connectorFailure("orders_read", err)
	}
	if _, err = ordersReader.Read(ctx, sdk.PageRequest{Limit: 10}); err != nil {
		return connectorFailure("orders_read", err)
	}
	pass(evidence, "orders_read")

	taxonomyReader, err := registry.MarketplaceListingTaxonomyReader(account, runtime)
	if err != nil {
		return connectorFailure("taxonomy_read", err)
	}
	taxonomy, err := taxonomyReader.ReadMarketplaceListingTaxonomy(ctx, account, runtime, sdk.MarketplaceListingTaxonomyRequest{Locale: cfg.Locale, Jurisdiction: cfg.Jurisdiction, CategoryCode: cfg.CategoryCode})
	if err != nil {
		return connectorFailure("taxonomy_read", err)
	}
	setTaxonomy(evidence, taxonomy.Source, taxonomy.Fingerprint)
	pass(evidence, "taxonomy_read")

	if !cfg.WriteInventory {
		evidence.Write = writeEvidenceState{Attempted: false, ReadAfterWrite: false, Restored: true}
		return nil
	}
	if initial[0].Quantity >= 2_000_000_000 {
		return &smokeFailure{CheckID: "inventory_write", ErrorCode: "test_quantity_overflow"}
	}
	writer, err := registry.InventoryWriter(account, runtime, nil)
	if err != nil {
		return connectorFailure("inventory_write", err)
	}
	testQuantity := initial[0].Quantity + 1
	evidence.Write.Attempted = true
	writeKey := smokeKey(cfg, "stock-write")
	writeRequest := sdk.InventoryWriteRequest{ProductRemoteID: product.RemoteID, VariantRemoteID: variant.RemoteID, LocationRemoteID: location.RemoteID, Quantity: testQuantity, IdempotencyKey: writeKey}
	_, writeErr := writer.WriteInventory(ctx, account, runtime, writeRequest)
	if writeErr != nil {
		return reconcileAfterWriteError(ctx, cfg, writer, account, runtime, writeRequest, initial[0].Quantity, testQuantity, readInventory, evidence, writeErr)
	}
	observed, observedValid, failure := waitInventoryQuantity(ctx, readInventory, testQuantity)
	if failure != nil {
		return restoreInventory(ctx, cfg, writer, account, runtime, writeRequest, initial[0].Quantity, observed, observedValid, readInventory, evidence, failure)
	}
	evidence.Write.ReadAfterWrite = true
	restoreRequest := writeRequest
	restoreRequest.Quantity = initial[0].Quantity
	restoreRequest.IdempotencyKey = smokeKey(cfg, "stock-restore")
	if _, err = writer.WriteInventory(ctx, account, runtime, restoreRequest); err != nil {
		return &smokeFailure{CheckID: "cleanup", ErrorCode: normalizedErrorCode(err)}
	}
	if _, _, failure = waitInventoryQuantity(ctx, readInventory, initial[0].Quantity); failure != nil {
		return failure
	}
	evidence.Write.Restored = true
	pass(evidence, "inventory_write")
	pass(evidence, "read_after_write")
	pass(evidence, "cleanup")
	return nil
}

func selectProduct(products []sdk.RemoteProduct, wantedVariant string) (sdk.RemoteProduct, sdk.RemoteVariant, bool) {
	for _, product := range products {
		for _, variant := range product.Variants {
			if wantedVariant == "" || variant.RemoteID == wantedVariant {
				return product, variant, true
			}
		}
	}
	return sdk.RemoteProduct{}, sdk.RemoteVariant{}, false
}

func selectLocation(locations []sdk.RemoteLocation, wanted string) (sdk.RemoteLocation, bool) {
	for _, location := range locations {
		if wanted == "" || location.RemoteID == wanted {
			return location, true
		}
	}
	return sdk.RemoteLocation{}, false
}

func waitInventoryQuantity(ctx context.Context, read func() ([]sdk.RemoteInventory, *smokeFailure), wanted int64) (int64, bool, *smokeFailure) {
	var observed int64
	var observedValid bool
	for attempt := 0; attempt < 6; attempt++ {
		items, failure := read()
		if failure != nil {
			return observed, observedValid, failure
		}
		observed = items[0].Quantity
		observedValid = true
		if observed == wanted {
			return observed, observedValid, nil
		}
		if attempt < 5 {
			select {
			case <-ctx.Done():
				return observed, observedValid, &smokeFailure{CheckID: "read_after_write", ErrorCode: "timeout"}
			case <-time.After(time.Second):
			}
		}
	}
	return observed, observedValid, &smokeFailure{CheckID: "read_after_write", ErrorCode: "quantity_mismatch"}
}

func restoreInventory(ctx context.Context, cfg config, writer sdk.InventoryWriter, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest, original, observed int64, observedValid bool, read func() ([]sdk.RemoteInventory, *smokeFailure), evidence *smokeEvidence, failure *smokeFailure) *smokeFailure {
	if !observedValid {
		evidence.Write.Restored = false
		return failure
	}
	if observed == original {
		evidence.Write.Restored = true
		return failure
	}
	if observed != original+1 {
		evidence.Write.Restored = false
		return &smokeFailure{CheckID: "cleanup", ErrorCode: "unexpected_remote_quantity"}
	}
	request.Quantity = original
	request.IdempotencyKey = smokeKey(cfg, "stock-restore")
	if _, err := writer.WriteInventory(ctx, account, runtime, request); err != nil {
		evidence.Write.Restored = false
		return &smokeFailure{CheckID: "cleanup", ErrorCode: normalizedErrorCode(err)}
	}
	if _, restoredValid, restoreFailure := waitInventoryQuantity(ctx, read, original); restoreFailure != nil || !restoredValid {
		evidence.Write.Restored = false
		return &smokeFailure{CheckID: "cleanup", ErrorCode: "restore_not_confirmed"}
	}
	evidence.Write.Restored = true
	return failure
}

func reconcileAfterWriteError(ctx context.Context, cfg config, writer sdk.InventoryWriter, account sdk.Account, runtime sdk.Runtime, request sdk.InventoryWriteRequest, original, testQuantity int64, read func() ([]sdk.RemoteInventory, *smokeFailure), evidence *smokeEvidence, writeErr error) *smokeFailure {
	items, readFailure := read()
	if readFailure != nil {
		return &smokeFailure{CheckID: "inventory_write", ErrorCode: "write_outcome_unknown"}
	}
	observed := items[0].Quantity
	if observed == original {
		evidence.Write.ReadAfterWrite = true
		evidence.Write.Restored = true
		return &smokeFailure{CheckID: "inventory_write", ErrorCode: normalizedErrorCode(writeErr)}
	}
	if observed != testQuantity {
		return &smokeFailure{CheckID: "cleanup", ErrorCode: "unexpected_remote_quantity"}
	}
	request.Quantity = original
	request.IdempotencyKey = smokeKey(cfg, "stock-restore")
	if _, restoreErr := writer.WriteInventory(ctx, account, runtime, request); restoreErr != nil {
		return &smokeFailure{CheckID: "cleanup", ErrorCode: normalizedErrorCode(restoreErr)}
	}
	if _, restoredValid, restoreFailure := waitInventoryQuantity(ctx, read, original); restoreFailure != nil || !restoredValid {
		evidence.Write.Restored = false
		return &smokeFailure{CheckID: "cleanup", ErrorCode: "restore_not_confirmed"}
	}
	evidence.Write.ReadAfterWrite = true
	evidence.Write.Restored = true
	return &smokeFailure{CheckID: "inventory_write", ErrorCode: normalizedErrorCode(writeErr)}
}

func connectorFailure(checkID string, err error) *smokeFailure {
	return &smokeFailure{CheckID: checkID, ErrorCode: normalizedErrorCode(err)}
}

func normalizedErrorCode(err error) string {
	var remote *sdk.RemoteError
	if errors.As(err, &remote) && remote != nil {
		return remote.Code
	}
	return "connector_error"
}

func smokeKey(cfg config, purpose string) string {
	digest := sha256.Sum256([]byte(cfg.ReleaseCommit + "\x00" + cfg.AccountRef + "\x00" + cfg.RunID + "\x00" + purpose))
	return "marketplace-live-smoke-" + purpose + "-" + hex.EncodeToString(digest[:8])
}

func pass(evidence *smokeEvidence, id string) {
	evidence.Checks = append(evidence.Checks, checkEvidence{ID: id, Status: "PASS", EvidenceRef: "marketplace-live-smoke/" + id})
}

func setTaxonomy(evidence *smokeEvidence, source, fingerprint string) {
	evidence.Taxonomy = taxonomyEvidence{Status: "PASS", Fingerprint: fingerprint, Source: source}
}

func writeEvidence(path string, evidence smokeEvidence) error {
	if evidence.Checks == nil {
		evidence.Checks = []checkEvidence{}
	}
	encoded, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".marketplace-live-smoke-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}
