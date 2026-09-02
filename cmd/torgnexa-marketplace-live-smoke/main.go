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
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	ozon "github.com/torgnexa/torgnexa/connectors/marketplaces/ozon"
	wildberries "github.com/torgnexa/torgnexa/connectors/marketplaces/wildberries"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

const (
	smokeSecretReference = "sec:v1:0123456789abcdef0123456789abcdef"
	smokeAccountID       = "marketplace-live-smoke"
	smokeOrganizationID  = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001"
	smokeWorkspaceID     = "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"
	writeAcknowledgement = "I_UNDERSTAND_THIS_IS_NON_PRODUCTION"
	maxSmokeBodyBytes    = 12 << 20
)

var (
	sha40Pattern        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	categoryCodePattern = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	accountRefPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$`)
)

type config struct {
	Connector       string
	Environment     string
	Target          string
	AccountRef      string
	ReleaseCommit   string
	Output          string
	Scope           string
	Locale          string
	Jurisdiction    string
	CategoryCode    string
	WBWarehouseID   string
	WBVariantID     string
	OzonWarehouseID string
	OzonOfferID     string
	RunID           string
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

type httpTransport struct {
	client       *http.Client
	allowedHosts map[string]struct{}
}

func newHTTPTransport() httpTransport {
	return httpTransport{
		client: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		allowedHosts: map[string]struct{}{
			"content-api.wildberries.ru":     {},
			"marketplace-api.wildberries.ru": {},
			"api-seller.ozon.ru":             {},
		},
	}
}

func (transport httpTransport) call(ctx context.Context, method, host, path string, query url.Values, body []byte, headers map[string]string) (int, []byte, string, int64, error) {
	if _, ok := transport.allowedHosts[host]; !ok || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\x00\r\n") {
		return 0, nil, "", 0, errors.New("remote host or path is not allowlisted")
	}
	remoteURL := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	request, err := http.NewRequestWithContext(ctx, method, remoteURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return 0, nil, "", 0, errors.New("remote request could not be created")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "torgnexa-marketplace-live-smoke/1")
	if len(body) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := transport.client.Do(request)
	if err != nil {
		return 0, nil, "", 0, err
	}
	defer response.Body.Close()
	limited := io.LimitReader(response.Body, maxSmokeBodyBytes+1)
	responseBody, err := io.ReadAll(limited)
	if err != nil || len(responseBody) > maxSmokeBodyBytes {
		return 0, nil, "", 0, errors.New("remote response exceeded smoke bound")
	}
	return response.StatusCode, responseBody, response.Header.Get("X-Request-ID"), retryAfterMillis(response.Header.Get("Retry-After")), nil
}

func retryAfterMillis(value string) int64 {
	seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || seconds < 0 || seconds > 24*60*60 {
		return 0
	}
	return seconds * 1000
}

type wildberriesTransport struct{ httpTransport }

func (transport wildberriesTransport) Do(ctx context.Context, request wildberries.Request) (wildberries.Response, error) {
	query := url.Values{}
	for _, parameter := range request.Query {
		query.Set(parameter.Name, parameter.Value)
	}
	status, body, requestID, retryAfter, err := transport.call(ctx, request.Method, request.Host, request.Path, query, request.Body, map[string]string{"Authorization": string(request.Token), "X-Idempotency-Key": request.IdempotencyKey})
	return wildberries.Response{StatusCode: status, Body: body, RequestID: requestID, RetryAfterMS: retryAfter}, err
}

type ozonTransport struct{ httpTransport }

func (transport ozonTransport) Do(ctx context.Context, request ozon.Request) (ozon.Response, error) {
	query := url.Values{}
	for _, parameter := range request.Query {
		query.Set(parameter.Name, parameter.Value)
	}
	status, body, requestID, retryAfter, err := transport.call(ctx, request.Method, request.Host, request.Path, query, request.Body, map[string]string{"Client-Id": string(request.ClientID), "Api-Key": string(request.APIKey), "X-Idempotency-Key": request.IdempotencyKey})
	return ozon.Response{StatusCode: status, Body: body, RequestID: requestID, RetryAfterMS: retryAfter}, err
}

func main() {
	var connector, output string
	flag.StringVar(&connector, "connector", os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR"), "wildberries or ozon")
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
	if evidence.Repository == "" {
		evidence.Repository = "external-release-runner"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var smokeErr *smokeFailure
	switch cfg.Connector {
	case "wildberries":
		smokeErr = runWildberries(ctx, cfg, &evidence)
	case "ozon":
		smokeErr = runOzon(ctx, cfg, &evidence)
	default:
		smokeErr = &smokeFailure{CheckID: "configuration", ErrorCode: "connector_not_supported"}
	}
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
		Connector:       strings.TrimSpace(connector),
		Environment:     strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ENVIRONMENT")),
		Target:          strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_TARGET")),
		AccountRef:      strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ACCOUNT_REF")),
		ReleaseCommit:   strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_RELEASE_COMMIT")),
		Output:          strings.TrimSpace(output),
		Scope:           strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_SCOPE")),
		Locale:          valueOr(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_LOCALE"), "ru-RU"),
		Jurisdiction:    valueOr(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_JURISDICTION"), "RU"),
		CategoryCode:    strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_CATEGORY_CODE")),
		WBWarehouseID:   strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_WB_WAREHOUSE_ID")),
		WBVariantID:     strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_WB_VARIANT_ID")),
		OzonWarehouseID: strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_OZON_WAREHOUSE_ID")),
		OzonOfferID:     strings.TrimSpace(os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_OZON_OFFER_ID")),
	}
	if cfg.Connector != "wildberries" && cfg.Connector != "ozon" {
		return config{}, errors.New("TORGNEXA_MARKETPLACE_SMOKE_CONNECTOR must be wildberries or ozon")
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
	if cfg.Connector == "ozon" && cfg.Scope == "qualification" && os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES") != writeAcknowledgement {
		return config{}, errors.New("Ozon qualification requires TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES=" + writeAcknowledgement)
	}
	if cfg.Connector == "wildberries" && os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_ALLOW_WRITES") != "" {
		return config{}, errors.New("Wildberries smoke does not permit writes")
	}
	return cfg, nil
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

func runWildberries(ctx context.Context, cfg config, evidence *smokeEvidence) *smokeFailure {
	token := os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_WB_TOKEN")
	if token == "" {
		return &smokeFailure{CheckID: "configuration", ErrorCode: "wildberries_token_missing"}
	}
	connector := wildberries.New(wildberriesTransport{newHTTPTransport()}, nil)
	account := smokeAccount("wildberries")
	runtime := secretRuntime{value: token}
	for attempt := 0; attempt < 2; attempt++ {
		health, err := connector.Health(ctx, account, runtime)
		if err != nil {
			return connectorFailure("health", err)
		}
		if health.Status != sdk.HealthHealthy {
			return &smokeFailure{CheckID: "health", ErrorCode: valueOr(health.ReasonCode, "not_healthy")}
		}
	}
	pass(evidence, "health")

	products, err := connector.ReadProducts(ctx, account, runtime, sdk.PageRequest{Limit: 10})
	if err != nil {
		return connectorFailure("products_read", err)
	}
	variant, ok := selectWildberriesProduct(products.Items, cfg.WBVariantID)
	if !ok {
		return &smokeFailure{CheckID: "products_read", ErrorCode: "test_product_variant_missing"}
	}
	pass(evidence, "products_read")

	locations, err := connector.ListInventoryLocations(ctx, account, runtime)
	if err != nil {
		return connectorFailure("inventory_locations_read", err)
	}
	location, ok := selectLocation(locations, cfg.WBWarehouseID)
	if !ok {
		return &smokeFailure{CheckID: "inventory_locations_read", ErrorCode: "test_warehouse_missing"}
	}
	pass(evidence, "inventory_locations_read")
	if _, err = connector.ReadInventory(ctx, account, runtime, sdk.InventoryQuery{LocationRemoteID: location.RemoteID, VariantRemoteIDs: []string{variant.RemoteID}}); err != nil {
		return connectorFailure("inventory_read", err)
	}
	pass(evidence, "inventory_read")
	if _, err = connector.ReadOrders(ctx, account, runtime, sdk.PageRequest{Limit: 10}); err != nil {
		return connectorFailure("orders_read", err)
	}
	pass(evidence, "orders_read")
	taxonomy, err := connector.ReadMarketplaceListingTaxonomy(ctx, account, runtime, sdk.MarketplaceListingTaxonomyRequest{Locale: cfg.Locale, Jurisdiction: cfg.Jurisdiction, CategoryCode: cfg.CategoryCode})
	if err != nil {
		return connectorFailure("taxonomy_read", err)
	}
	setTaxonomy(evidence, taxonomy.Source, taxonomy.Fingerprint)
	pass(evidence, "taxonomy_read")
	evidence.Write = writeEvidenceState{Attempted: false, ReadAfterWrite: false, Restored: true}
	return nil
}

func runOzon(ctx context.Context, cfg config, evidence *smokeEvidence) *smokeFailure {
	clientID := os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_OZON_CLIENT_ID")
	apiKey := os.Getenv("TORGNEXA_MARKETPLACE_SMOKE_OZON_API_KEY")
	if clientID == "" || apiKey == "" {
		return &smokeFailure{CheckID: "configuration", ErrorCode: "ozon_credentials_missing"}
	}
	connector := ozon.New(ozonTransport{newHTTPTransport()}, nil)
	account := smokeAccount("ozon")
	runtime := secretRuntime{value: clientID + "\n" + apiKey}
	for attempt := 0; attempt < 2; attempt++ {
		health, err := connector.Health(ctx, account, runtime)
		if err != nil {
			return connectorFailure("health", err)
		}
		if health.Status != sdk.HealthHealthy {
			return &smokeFailure{CheckID: "health", ErrorCode: valueOr(health.ReasonCode, "not_healthy")}
		}
	}
	pass(evidence, "health")

	products, err := connector.ReadProducts(ctx, account, runtime, sdk.PageRequest{Limit: 10})
	if err != nil {
		return connectorFailure("products_read", err)
	}
	product, variant, ok := selectOzonProduct(products.Items, cfg.OzonOfferID)
	if !ok {
		return &smokeFailure{CheckID: "products_read", ErrorCode: "test_product_offer_missing"}
	}
	pass(evidence, "products_read")

	locations, err := connector.ListInventoryLocations(ctx, account, runtime)
	if err != nil {
		return connectorFailure("inventory_locations_read", err)
	}
	location, ok := selectLocation(locations, cfg.OzonWarehouseID)
	if !ok {
		return &smokeFailure{CheckID: "inventory_locations_read", ErrorCode: "test_warehouse_missing"}
	}
	pass(evidence, "inventory_locations_read")

	readInventory := func() ([]sdk.RemoteInventory, *smokeFailure) {
		items, readErr := connector.ReadInventory(ctx, account, runtime, sdk.InventoryQuery{LocationRemoteID: location.RemoteID, VariantRemoteIDs: []string{variant.RemoteID}})
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
	if _, err = connector.ReadOrders(ctx, account, runtime, sdk.PageRequest{Limit: 10}); err != nil {
		return connectorFailure("orders_read", err)
	}
	pass(evidence, "orders_read")
	taxonomy, err := connector.ReadMarketplaceListingTaxonomy(ctx, account, runtime, sdk.MarketplaceListingTaxonomyRequest{Locale: cfg.Locale, Jurisdiction: cfg.Jurisdiction, CategoryCode: cfg.CategoryCode})
	if err != nil {
		return connectorFailure("taxonomy_read", err)
	}
	setTaxonomy(evidence, taxonomy.Source, taxonomy.Fingerprint)
	pass(evidence, "taxonomy_read")

	if cfg.Scope == "read" {
		evidence.Write = writeEvidenceState{Attempted: false, ReadAfterWrite: false, Restored: true}
		return nil
	}
	if initial[0].Quantity >= 2_000_000_000 {
		return &smokeFailure{CheckID: "inventory_write", ErrorCode: "test_quantity_overflow"}
	}
	testQuantity := initial[0].Quantity + 1
	evidence.Write.Attempted = true
	writeKey := smokeKey(cfg, "stock-write")
	_, writeErr := connector.WriteInventory(ctx, account, runtime, sdk.InventoryWriteRequest{ProductRemoteID: product.RemoteID, VariantRemoteID: variant.RemoteID, LocationRemoteID: location.RemoteID, Quantity: testQuantity, IdempotencyKey: writeKey})
	if writeErr != nil {
		return reconcileOzonAfterWriteError(ctx, cfg, connector, account, runtime, product.RemoteID, location.RemoteID, variant.RemoteID, initial[0].Quantity, testQuantity, readInventory, evidence, writeErr)
	}
	observed, observedValid, failure := waitOzonQuantity(ctx, readInventory, testQuantity)
	if failure != nil {
		return restoreOzonStock(ctx, cfg, connector, account, runtime, product.RemoteID, location.RemoteID, variant.RemoteID, initial[0].Quantity, observed, observedValid, readInventory, evidence, failure)
	}
	evidence.Write.ReadAfterWrite = true
	restoreKey := smokeKey(cfg, "stock-restore")
	if _, err = connector.WriteInventory(ctx, account, runtime, sdk.InventoryWriteRequest{ProductRemoteID: product.RemoteID, VariantRemoteID: variant.RemoteID, LocationRemoteID: location.RemoteID, Quantity: initial[0].Quantity, IdempotencyKey: restoreKey}); err != nil {
		return &smokeFailure{CheckID: "cleanup", ErrorCode: normalizedErrorCode(err)}
	}
	if _, _, failure = waitOzonQuantity(ctx, readInventory, initial[0].Quantity); failure != nil {
		return failure
	}
	evidence.Write.Restored = true
	pass(evidence, "inventory_write")
	pass(evidence, "read_after_write")
	pass(evidence, "cleanup")
	return nil
}

func selectWildberriesProduct(products []sdk.RemoteProduct, wantedVariant string) (sdk.RemoteVariant, bool) {
	for _, product := range products {
		for _, variant := range product.Variants {
			if wantedVariant == "" || variant.RemoteID == wantedVariant {
				return variant, true
			}
		}
	}
	return sdk.RemoteVariant{}, false
}

func selectOzonProduct(products []sdk.RemoteProduct, wantedOffer string) (sdk.RemoteProduct, sdk.RemoteVariant, bool) {
	for _, product := range products {
		for _, variant := range product.Variants {
			if wantedOffer == "" || variant.RemoteID == wantedOffer {
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

func waitOzonQuantity(ctx context.Context, read func() ([]sdk.RemoteInventory, *smokeFailure), wanted int64) (int64, bool, *smokeFailure) {
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

func restoreOzonStock(ctx context.Context, cfg config, connector *ozon.Connector, account sdk.Account, runtime sdk.Runtime, productID, locationID, offerID string, original, observed int64, observedValid bool, read func() ([]sdk.RemoteInventory, *smokeFailure), evidence *smokeEvidence, failure *smokeFailure) *smokeFailure {
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
	if _, err := connector.WriteInventory(ctx, account, runtime, sdk.InventoryWriteRequest{ProductRemoteID: productID, VariantRemoteID: offerID, LocationRemoteID: locationID, Quantity: original, IdempotencyKey: smokeKey(cfg, "stock-restore")}); err != nil {
		evidence.Write.Restored = false
		return &smokeFailure{CheckID: "cleanup", ErrorCode: normalizedErrorCode(err)}
	}
	if _, restoredValid, restoreFailure := waitOzonQuantity(ctx, read, original); restoreFailure != nil || !restoredValid {
		evidence.Write.Restored = false
		return &smokeFailure{CheckID: "cleanup", ErrorCode: "restore_not_confirmed"}
	}
	evidence.Write.Restored = true
	return failure
}

func reconcileOzonAfterWriteError(ctx context.Context, cfg config, connector *ozon.Connector, account sdk.Account, runtime sdk.Runtime, productID, locationID, offerID string, original, testQuantity int64, read func() ([]sdk.RemoteInventory, *smokeFailure), evidence *smokeEvidence, writeErr error) *smokeFailure {
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
	if _, restoreErr := connector.WriteInventory(ctx, account, runtime, sdk.InventoryWriteRequest{ProductRemoteID: productID, VariantRemoteID: offerID, LocationRemoteID: locationID, Quantity: original, IdempotencyKey: smokeKey(cfg, "stock-restore")}); restoreErr != nil {
		return &smokeFailure{CheckID: "cleanup", ErrorCode: normalizedErrorCode(restoreErr)}
	}
	if _, restoredValid, restoreFailure := waitOzonQuantity(ctx, read, original); restoreFailure != nil || !restoredValid {
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
