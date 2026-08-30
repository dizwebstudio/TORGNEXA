// Package config loads and validates process configuration from the environment.
package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const envPrefix = "TORGNEXA_"

// Service identifies one of the independently runnable TORGNEXA processes.
type Service string

const (
	ServiceAPI       Service = "api"
	ServiceWorker    Service = "worker"
	ServiceScheduler Service = "scheduler"
	ServiceMCP       Service = "mcp"
)

// Environment identifies a deployment environment without encoding provider details.
type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentTest        Environment = "test"
	EnvironmentProduction  Environment = "production"
)

// LogFormat identifies the structured logging wire format.
type LogFormat string

const (
	LogFormatJSON LogFormat = "json"
	LogFormatText LogFormat = "text"
)

// Log contains process logging configuration.
type Log struct {
	Level     string
	Format    LogFormat
	AddSource bool
}

// HTTP contains API listener and resource-limit configuration.
type HTTP struct {
	Address           string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
}

// Security contains mandatory HTTP edge policy used by the API composition root.
type Security struct {
	TrustedProxyCIDRs []string
	AdminCIDRs        []string
	AllowedOrigins    []string
	MaxRequestBytes   int64
	MaxUploadBytes    int64
	RatePerMinute     int
	HSTSSeconds       int64
}

// Database contains bounded PostgreSQL connection-pool configuration. URL is
// secret-bearing configuration and must never be copied into logs or errors.
type Database struct {
	URL             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	ConnectTimeout  time.Duration
}

// ClickHouse contains the bounded analytical HTTP query configuration.
type ClickHouse struct {
	Endpoint     string
	Username     string
	Password     string
	QueryTimeout time.Duration
}

// ObjectStorage contains the S3-compatible quarantine storage configuration.
// Credentials are secret-bearing and must never be logged.
type ObjectStorage struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Timeout   time.Duration
}

// OIDC contains the API's remote token-validation and tenant-claim policy.
// URLs and claim names are configuration, while bearer tokens are never stored.
type OIDC struct {
	Issuer                  string
	UserInfoURL             string
	ClientID                string
	OrganizationClaim       string
	WorkspaceClaim          string
	DevelopmentOrganization string
	DevelopmentWorkspace    string
	RequestTimeout          time.Duration
	ManagedIssuerHosts      []string
}

// Secrets configures the Community encrypted provider. MasterKey is decoded
// from process configuration and must never be logged or persisted.
type Secrets struct {
	KeyID     string
	MasterKey []byte
}

// Worker contains bounded background-runtime configuration. Kafka and worker
// process settings live here rather than leaking broker/client details into
// domain packages.
// Notifications contains optional production delivery adapters. Destination
// addresses/chat IDs remain tenant-scoped encrypted secret references in PostgreSQL;
// these fields contain process-level transport credentials only.
type Notifications struct {
	SMTPAddress     string
	SMTPFrom        string
	SMTPUsername    string
	SMTPPassword    string
	SMTPServerName  string
	SMTPImplicitTLS bool
	ChatEndpoint    string
	Timeout         time.Duration
}

type Worker struct {
	KafkaBrokers           []string
	KafkaConsumerGroup     string
	KafkaTopics            []string
	PollInterval           time.Duration
	DispatchBatch          int
	Lease                  time.Duration
	ReconciliationEnabled  bool
	UploadsEnabled         bool
	ClamAVNetwork          string
	ClamAVAddress          string
	ClamAVEngineVersion    string
	ClamAVSignatureVersion string
	ClamAVTimeout          time.Duration
}

// Config is the validated configuration shared by a single process.
type Config struct {
	Service         Service
	Environment     Environment
	Log             Log
	ShutdownTimeout time.Duration
	HTTP            HTTP
	Security        Security
	Database        Database
	ClickHouse      ClickHouse
	ObjectStorage   ObjectStorage
	OIDC            OIDC
	Secrets         Secrets
	Worker          Worker
	Notifications   Notifications
}

// Load reads configuration for service from the process environment.
func Load(service Service) (Config, error) {
	return LoadWithLookup(service, os.LookupEnv)
}

// LoadWithLookup reads configuration using lookup. It is intended for deterministic
// tests and configuration adapters that expose environment-compatible values.
func LoadWithLookup(service Service, lookup func(string) (string, bool)) (Config, error) {
	if !service.valid() {
		return Config{}, fmt.Errorf("unsupported service %q", service)
	}
	if lookup == nil {
		return Config{}, fmt.Errorf("configuration lookup is required")
	}

	cfg := Config{
		Service:     service,
		Environment: EnvironmentDevelopment,
		Log: Log{
			Level:  "info",
			Format: LogFormatJSON,
		},
		ShutdownTimeout: 15 * time.Second,
		HTTP: HTTP{
			Address:           "127.0.0.1:8080",
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    64 << 10,
		},
		Security: Security{
			MaxRequestBytes: 32 << 20,
			MaxUploadBytes:  16 << 20,
			RatePerMinute:   600,
			HSTSSeconds:     31536000,
		},
		Database: Database{
			MaxOpenConns:    20,
			MaxIdleConns:    10,
			ConnMaxLifetime: 30 * time.Minute,
			ConnMaxIdleTime: 5 * time.Minute,
			ConnectTimeout:  5 * time.Second,
		},
		ClickHouse: ClickHouse{Endpoint: "http://127.0.0.1:8123", QueryTimeout: 5 * time.Second},
		ObjectStorage: ObjectStorage{
			Region: "garage", Timeout: 30 * time.Second,
		},
		OIDC: OIDC{
			ClientID:          "torgnexa-web",
			OrganizationClaim: "organization_id",
			WorkspaceClaim:    "workspace_id",
			RequestTimeout:    3 * time.Second,
		},
		Secrets:       Secrets{KeyID: "community-v1"},
		Notifications: Notifications{Timeout: 10 * time.Second},
		Worker: Worker{
			KafkaBrokers:       []string{"127.0.0.1:9092"},
			KafkaConsumerGroup: "torgnexa.webhooks.v1",
			KafkaTopics: []string{
				"billing.subscription.events.v1",
				"commerce.catalog.events.v1",
				"commerce.claim.events.v1",
				"commerce.fulfillment.events.v1",
				"commerce.inventory.events.v1",
				"commerce.orders.events.v1",
				"commerce.pim.events.v1",
				"commerce.pricing.events.v1",
				"commerce.returns.events.v1",
				"commerce.social.events.v1",
				"compliance.document.events.v1",
				"compliance.product.events.v1",
				"enterprise.legal_party.events.v1",
				"finance.fx_rate.events.v1",
				"finance.fx_rate.events.v2",
				"finance.settlement_entry.events.v1",
				"governance.approval.events.v1",
				"governance.entitlement.events.v1",
				"party.counterparty.events.v1",
				"platform.notifications.events.v1",
				"privacy.request.events.v1",
				"security.upload.events.v1",
				"warehouse.stock.events.v1",
			},
			PollInterval:           500 * time.Millisecond,
			DispatchBatch:          32,
			Lease:                  90 * time.Second,
			ReconciliationEnabled:  true,
			UploadsEnabled:         false,
			ClamAVNetwork:          "tcp",
			ClamAVAddress:          "127.0.0.1:3310",
			ClamAVEngineVersion:    "runtime",
			ClamAVSignatureVersion: "runtime",
			ClamAVTimeout:          30 * time.Second,
		},
	}

	var err error
	if cfg.Environment, err = readEnvironment(lookup, "ENV", cfg.Environment); err != nil {
		return Config{}, err
	}
	if cfg.Log.Level, err = readLogLevel(lookup, "LOG_LEVEL", cfg.Log.Level); err != nil {
		return Config{}, err
	}
	if cfg.Log.Format, err = readLogFormat(lookup, "LOG_FORMAT", cfg.Log.Format); err != nil {
		return Config{}, err
	}
	if cfg.Log.AddSource, err = readBool(lookup, "LOG_ADD_SOURCE", cfg.Log.AddSource); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = readDuration(lookup, "SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout, time.Second, 2*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.Database.URL, _, err = readDatabaseURL(lookup); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxOpenConns, err = readInt(lookup, "DB_MAX_OPEN_CONNS", cfg.Database.MaxOpenConns, 1, 1000); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxIdleConns, err = readInt(lookup, "DB_MAX_IDLE_CONNS", cfg.Database.MaxIdleConns, 0, cfg.Database.MaxOpenConns); err != nil {
		return Config{}, err
	}
	if cfg.Database.ConnMaxLifetime, err = readDuration(lookup, "DB_CONN_MAX_LIFETIME", cfg.Database.ConnMaxLifetime, time.Minute, 24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.Database.ConnMaxIdleTime, err = readDuration(lookup, "DB_CONN_MAX_IDLE_TIME", cfg.Database.ConnMaxIdleTime, time.Second, time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.Database.ConnectTimeout, err = readDuration(lookup, "DB_CONNECT_TIMEOUT", cfg.Database.ConnectTimeout, 100*time.Millisecond, time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.ClickHouse.Endpoint, err = readClickHouseEndpoint(lookup, cfg.ClickHouse.Endpoint); err != nil {
		return Config{}, err
	}
	if cfg.ClickHouse.Username, _, err = readRawOptional(lookup, "CLICKHOUSE_USERNAME", 256); err != nil {
		return Config{}, err
	}
	if cfg.ClickHouse.Password, _, err = readRawOptional(lookup, "CLICKHOUSE_PASSWORD", 4096); err != nil {
		return Config{}, err
	}
	if cfg.ClickHouse.QueryTimeout, err = readDuration(lookup, "CLICKHOUSE_QUERY_TIMEOUT", cfg.ClickHouse.QueryTimeout, 100*time.Millisecond, 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.ObjectStorage.Endpoint, _, err = readExternalOptional(lookup, "S3_ENDPOINT", 2048); err != nil {
		return Config{}, err
	}
	if cfg.ObjectStorage.Bucket, _, err = readExternalOptional(lookup, "S3_BUCKET", 128); err != nil {
		return Config{}, err
	}
	if value, ok, readErr := readExternalOptional(lookup, "S3_REGION", 64); readErr != nil {
		return Config{}, readErr
	} else if ok {
		cfg.ObjectStorage.Region = value
	}
	if cfg.ObjectStorage.AccessKey, _, err = readExternalRawOptional(lookup, "S3_ACCESS_KEY", 512); err != nil {
		return Config{}, err
	}
	if cfg.ObjectStorage.SecretKey, _, err = readExternalRawOptional(lookup, "S3_SECRET_KEY", 4096); err != nil {
		return Config{}, err
	}
	if cfg.ObjectStorage.Timeout, err = readDuration(lookup, "S3_REQUEST_TIMEOUT", cfg.ObjectStorage.Timeout, time.Second, 2*time.Minute); err != nil {
		return Config{}, err
	}
	if err := validateObjectStorage(cfg.ObjectStorage); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.Issuer, _, err = readOptional(lookup, "OIDC_ISSUER"); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.UserInfoURL, _, err = readOptional(lookup, "OIDC_USERINFO_URL"); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.ClientID, err = readSafeString(lookup, "OIDC_CLIENT_ID", cfg.OIDC.ClientID, 128); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.OrganizationClaim, err = readSafeString(lookup, "OIDC_ORGANIZATION_CLAIM", cfg.OIDC.OrganizationClaim, 128); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.WorkspaceClaim, err = readSafeString(lookup, "OIDC_WORKSPACE_CLAIM", cfg.OIDC.WorkspaceClaim, 128); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.DevelopmentOrganization, _, err = readOptional(lookup, "OIDC_DEVELOPMENT_ORGANIZATION_ID"); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.DevelopmentWorkspace, _, err = readOptional(lookup, "OIDC_DEVELOPMENT_WORKSPACE_ID"); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.RequestTimeout, err = readDuration(lookup, "OIDC_REQUEST_TIMEOUT", cfg.OIDC.RequestTimeout, 100*time.Millisecond, 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.OIDC.ManagedIssuerHosts, err = readDefaultDenyCSV(lookup, "OIDC_MANAGED_ISSUER_HOSTS", cfg.OIDC.ManagedIssuerHosts); err != nil {
		return Config{}, err
	}
	if cfg.Secrets.KeyID, err = readSafeString(lookup, "SECRETS_KEY_ID", cfg.Secrets.KeyID, 128); err != nil {
		return Config{}, err
	}
	if raw, present, readErr := readOptional(lookup, "SECRETS_MASTER_KEY"); readErr != nil {
		return Config{}, readErr
	} else if present {
		cfg.Secrets.MasterKey, err = base64.StdEncoding.DecodeString(raw)
		if err != nil || len(cfg.Secrets.MasterKey) != 32 {
			return Config{}, fmt.Errorf("TORGNEXA_SECRETS_MASTER_KEY must be base64-encoded 32 bytes")
		}
	}

	if cfg.Notifications.SMTPAddress, _, err = readExternalOptionalOrEmpty(lookup, "NOTIFICATION_SMTP_ADDRESS", 255); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.SMTPFrom, _, err = readExternalOptionalOrEmpty(lookup, "NOTIFICATION_SMTP_FROM", 320); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.SMTPUsername, _, err = readRawOptional(lookup, "NOTIFICATION_SMTP_USERNAME", 512); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.SMTPPassword, _, err = readRawOptional(lookup, "NOTIFICATION_SMTP_PASSWORD", 4096); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.SMTPServerName, _, err = readExternalOptionalOrEmpty(lookup, "NOTIFICATION_SMTP_SERVER_NAME", 255); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.SMTPImplicitTLS, err = readBool(lookup, "NOTIFICATION_SMTP_IMPLICIT_TLS", cfg.Notifications.SMTPImplicitTLS); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.ChatEndpoint, _, err = readExternalOptionalOrEmpty(lookup, "NOTIFICATION_CHAT_ENDPOINT", 2048); err != nil {
		return Config{}, err
	}
	if cfg.Notifications.Timeout, err = readDuration(lookup, "NOTIFICATION_DELIVERY_TIMEOUT", cfg.Notifications.Timeout, time.Second, time.Minute); err != nil {
		return Config{}, err
	}

	if service == ServiceWorker {
		// Upload workers must enforce the same operator-owned byte limit as the API
		// admission path even though worker processes do not expose HTTP.
		if cfg.Security.MaxUploadBytes, err = readInt64(lookup, "SECURITY_MAX_UPLOAD_BYTES", cfg.Security.MaxUploadBytes, 1<<10, 1<<30); err != nil {
			return Config{}, err
		}
		if cfg.Worker.KafkaBrokers, err = readCSV(lookup, "KAFKA_BROKERS", cfg.Worker.KafkaBrokers); err != nil {
			return Config{}, err
		}
		if cfg.Worker.KafkaConsumerGroup, err = readSafeString(lookup, "KAFKA_CONSUMER_GROUP", cfg.Worker.KafkaConsumerGroup, 128); err != nil {
			return Config{}, err
		}
		if cfg.Worker.KafkaTopics, err = readDefaultDenyCSV(lookup, "KAFKA_TOPICS", cfg.Worker.KafkaTopics); err != nil {
			return Config{}, err
		}
		if cfg.Worker.PollInterval, err = readDuration(lookup, "WORKER_POLL_INTERVAL", cfg.Worker.PollInterval, 50*time.Millisecond, 30*time.Second); err != nil {
			return Config{}, err
		}
		if cfg.Worker.DispatchBatch, err = readInt(lookup, "WORKER_DISPATCH_BATCH", cfg.Worker.DispatchBatch, 1, 1000); err != nil {
			return Config{}, err
		}
		if cfg.Worker.Lease, err = readDuration(lookup, "WORKER_LEASE", cfg.Worker.Lease, 10*time.Second, 10*time.Minute); err != nil {
			return Config{}, err
		}
		if cfg.Worker.ReconciliationEnabled, err = readBool(lookup, "WORKER_RECONCILIATION_ENABLED", cfg.Worker.ReconciliationEnabled); err != nil {
			return Config{}, err
		}
		if cfg.Worker.UploadsEnabled, err = readBool(lookup, "WORKER_UPLOADS_ENABLED", cfg.Worker.UploadsEnabled); err != nil {
			return Config{}, err
		}
		if cfg.Worker.ClamAVNetwork, err = readSafeString(lookup, "CLAMAV_NETWORK", cfg.Worker.ClamAVNetwork, 16); err != nil {
			return Config{}, err
		}
		if cfg.Worker.ClamAVAddress, err = readSafeString(lookup, "CLAMAV_ADDRESS", cfg.Worker.ClamAVAddress, 255); err != nil {
			return Config{}, err
		}
		if cfg.Worker.ClamAVEngineVersion, err = readSafeString(lookup, "CLAMAV_ENGINE_VERSION", cfg.Worker.ClamAVEngineVersion, 128); err != nil {
			return Config{}, err
		}
		if cfg.Worker.ClamAVSignatureVersion, err = readSafeString(lookup, "CLAMAV_SIGNATURE_VERSION", cfg.Worker.ClamAVSignatureVersion, 128); err != nil {
			return Config{}, err
		}
		if cfg.Worker.ClamAVTimeout, err = readDuration(lookup, "CLAMAV_TIMEOUT", cfg.Worker.ClamAVTimeout, time.Second, 2*time.Minute); err != nil {
			return Config{}, err
		}
		if len(cfg.Worker.KafkaBrokers) == 0 {
			return Config{}, fmt.Errorf("%sKAFKA_BROKERS must contain at least one broker", envPrefix)
		}
		if cfg.Worker.UploadsEnabled && cfg.ObjectStorage.Endpoint == "" {
			return Config{}, fmt.Errorf("S3 configuration is required when %sWORKER_UPLOADS_ENABLED=true", envPrefix)
		}
	}

	if service != ServiceAPI && service != ServiceMCP {
		return cfg, nil
	}
	address, addressConfigured, err := readOptional(lookup, "HTTP_ADDR")
	if err != nil {
		return Config{}, err
	}
	if addressConfigured {
		cfg.HTTP.Address = address
	}
	if cfg.Environment == EnvironmentProduction && !addressConfigured {
		return Config{}, fmt.Errorf("%sHTTP_ADDR must be set explicitly in production", envPrefix)
	}
	if err := validateTCPAddress(cfg.HTTP.Address); err != nil {
		return Config{}, fmt.Errorf("%sHTTP_ADDR: %w", envPrefix, err)
	}
	if cfg.HTTP.ReadHeaderTimeout, err = readDuration(lookup, "HTTP_READ_HEADER_TIMEOUT", cfg.HTTP.ReadHeaderTimeout, 100*time.Millisecond, 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.ReadTimeout, err = readDuration(lookup, "HTTP_READ_TIMEOUT", cfg.HTTP.ReadTimeout, time.Second, 2*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.WriteTimeout, err = readDuration(lookup, "HTTP_WRITE_TIMEOUT", cfg.HTTP.WriteTimeout, time.Second, 2*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.IdleTimeout, err = readDuration(lookup, "HTTP_IDLE_TIMEOUT", cfg.HTTP.IdleTimeout, time.Second, 10*time.Minute); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.MaxHeaderBytes, err = readInt(lookup, "HTTP_MAX_HEADER_BYTES", cfg.HTTP.MaxHeaderBytes, 4<<10, 1<<20); err != nil {
		return Config{}, err
	}
	if cfg.Security.TrustedProxyCIDRs, err = readCSV(lookup, "SECURITY_TRUSTED_PROXY_CIDRS", cfg.Security.TrustedProxyCIDRs); err != nil {
		return Config{}, err
	}
	if cfg.Security.AdminCIDRs, err = readCSV(lookup, "SECURITY_ADMIN_CIDRS", cfg.Security.AdminCIDRs); err != nil {
		return Config{}, err
	}
	if cfg.Security.AllowedOrigins, err = readCSV(lookup, "SECURITY_ALLOWED_ORIGINS", cfg.Security.AllowedOrigins); err != nil {
		return Config{}, err
	}
	if cfg.Security.MaxRequestBytes, err = readInt64(lookup, "SECURITY_MAX_REQUEST_BYTES", cfg.Security.MaxRequestBytes, 1<<10, 1<<30); err != nil {
		return Config{}, err
	}
	if cfg.Security.MaxUploadBytes, err = readInt64(lookup, "SECURITY_MAX_UPLOAD_BYTES", cfg.Security.MaxUploadBytes, 1<<10, cfg.Security.MaxRequestBytes); err != nil {
		return Config{}, err
	}
	if cfg.Security.RatePerMinute, err = readInt(lookup, "SECURITY_RATE_PER_MINUTE", cfg.Security.RatePerMinute, 1, 1_000_000); err != nil {
		return Config{}, err
	}
	if cfg.Security.HSTSSeconds, err = readInt64(lookup, "SECURITY_HSTS_SECONDS", cfg.Security.HSTSSeconds, 31536000, 63072000); err != nil {
		return Config{}, err
	}
	if cfg.Security.MaxUploadBytes > cfg.Security.MaxRequestBytes {
		return Config{}, fmt.Errorf("%sSECURITY_MAX_UPLOAD_BYTES must not exceed %sSECURITY_MAX_REQUEST_BYTES", envPrefix, envPrefix)
	}
	if cfg.Environment == EnvironmentProduction {
		if len(cfg.Security.TrustedProxyCIDRs) == 0 {
			return Config{}, fmt.Errorf("%sSECURITY_TRUSTED_PROXY_CIDRS must be set explicitly in production", envPrefix)
		}
	}

	return cfg, nil
}

func readDatabaseURL(lookup func(string) (string, bool)) (string, bool, error) {
	raw, ok := lookup("DATABASE_URL")
	if !ok {
		return "", false, nil
	}
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 4096 {
		return "", false, fmt.Errorf("DATABASE_URL must be non-empty and at most 4096 characters")
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return "", false, fmt.Errorf("DATABASE_URL contains forbidden characters")
		}
	}
	return value, true, nil
}

func readClickHouseEndpoint(lookup func(string) (string, bool), fallback string) (string, error) {
	raw, ok := lookup("CLICKHOUSE_DSN")
	if !ok {
		return fallback, nil
	}
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf("CLICKHOUSE_DSN must be an HTTP(S) origin without credentials, query, fragment, or path")
	}
	if len(value) > 2048 || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", fmt.Errorf("CLICKHOUSE_DSN contains forbidden characters")
	}
	return strings.TrimRight(value, "/"), nil
}

func readExternalOptional(lookup func(string) (string, bool), key string, max int) (string, bool, error) {
	raw, ok := lookup(key)
	if !ok {
		return "", false, nil
	}
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > max || strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return "", false, fmt.Errorf("%s contains invalid characters", key)
	}
	return value, true, nil
}

func readExternalOptionalOrEmpty(lookup func(string) (string, bool), key string, max int) (string, bool, error) {
	raw, ok := lookup(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return "", false, nil
	}
	return readExternalOptional(lookup, key, max)
}

func readExternalRawOptional(lookup func(string) (string, bool), key string, max int) (string, bool, error) {
	raw, ok := lookup(key)
	if !ok {
		return "", false, nil
	}
	if raw == "" || len(raw) > max || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", false, fmt.Errorf("%s contains invalid characters", key)
	}
	return raw, true, nil
}

func validateObjectStorage(storage ObjectStorage) error {
	configured := storage.Endpoint != "" || storage.Bucket != "" || storage.AccessKey != "" || storage.SecretKey != ""
	if !configured {
		return nil
	}
	parsed, err := url.Parse(storage.Endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return fmt.Errorf("S3_ENDPOINT must be an HTTP(S) origin without credentials, query, fragment, or path")
	}
	if storage.Bucket == "" || storage.Region == "" || storage.AccessKey == "" || storage.SecretKey == "" {
		return fmt.Errorf("S3_ENDPOINT, S3_BUCKET, S3_REGION, S3_ACCESS_KEY and S3_SECRET_KEY must be configured together")
	}
	for _, value := range []string{storage.Bucket, storage.Region} {
		if strings.IndexFunc(value, func(r rune) bool { return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '.' && r != '-' }) >= 0 {
			return fmt.Errorf("S3_BUCKET and S3_REGION must use lowercase DNS-safe characters")
		}
	}
	return nil
}

func readRawOptional(lookup func(string) (string, bool), key string, max int) (string, bool, error) {
	raw, ok := lookup(key)
	if !ok {
		return "", false, nil
	}
	if raw == "" {
		return "", false, nil
	}
	if len(raw) > max || strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", false, fmt.Errorf("%s contains invalid characters", key)
	}
	return raw, true, nil
}

func (s Service) valid() bool {
	switch s {
	case ServiceAPI, ServiceWorker, ServiceScheduler, ServiceMCP:
		return true
	default:
		return false
	}
}

func readEnvironment(lookup func(string) (string, bool), name string, fallback Environment) (Environment, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	if len(raw) > 32 {
		return "", fmt.Errorf("%s%s must be at most 32 characters", envPrefix, name)
	}
	for i, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || (i > 0 && (r == '-' || r == '_')) {
			continue
		}
		return "", fmt.Errorf("%s%s must use lowercase letters, digits, hyphens, or underscores", envPrefix, name)
	}
	return Environment(raw), nil
}

func readLogLevel(lookup func(string) (string, bool), name, fallback string) (string, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	switch raw {
	case "debug", "info", "warn", "error":
		return raw, nil
	default:
		return "", fmt.Errorf("%s%s must be debug, info, warn, or error", envPrefix, name)
	}
}

func readLogFormat(lookup func(string) (string, bool), name string, fallback LogFormat) (LogFormat, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	value := LogFormat(raw)
	switch value {
	case LogFormatJSON, LogFormatText:
		return value, nil
	default:
		return "", fmt.Errorf("%s%s must be json or text", envPrefix, name)
	}
}

func readBool(lookup func(string) (string, bool), name string, fallback bool) (bool, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	value, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		return false, fmt.Errorf("%s%s must be a boolean", envPrefix, name)
	}
	return value, nil
}

func readDuration(lookup func(string) (string, bool), name string, fallback, min, max time.Duration) (time.Duration, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	value, parseErr := time.ParseDuration(raw)
	if parseErr != nil || value < min || value > max {
		return 0, fmt.Errorf("%s%s must be a duration between %s and %s", envPrefix, name, min, max)
	}
	return value, nil
}

func readInt(lookup func(string) (string, bool), name string, fallback, min, max int) (int, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	value, parseErr := strconv.Atoi(raw)
	if parseErr != nil || value < min || value > max {
		return 0, fmt.Errorf("%s%s must be an integer between %d and %d", envPrefix, name, min, max)
	}
	return value, nil
}

func readOptional(lookup func(string) (string, bool), name string) (string, bool, error) {
	key := envPrefix + name
	raw, ok := lookup(key)
	if !ok {
		return "", false, nil
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false, fmt.Errorf("%s must not be empty", key)
	}
	return value, true, nil
}

func readSafeString(lookup func(string) (string, bool), name, fallback string, max int) (string, error) {
	value, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	if len(value) > max || strings.IndexFunc(value, unicode.IsSpace) >= 0 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%s%s contains invalid characters", envPrefix, name)
	}
	return value, nil
}

func readInt64(lookup func(string) (string, bool), name string, fallback, min, max int64) (int64, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return fallback, err
	}
	value, parseErr := strconv.ParseInt(raw, 10, 64)
	if parseErr != nil || value < min || value > max {
		return 0, fmt.Errorf("%s%s must be an integer between %d and %d", envPrefix, name, min, max)
	}
	return value, nil
}

func readCSV(lookup func(string) (string, bool), name string, fallback []string) ([]string, error) {
	raw, ok, err := readOptional(lookup, name)
	if err != nil || !ok {
		return append([]string(nil), fallback...), err
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" || len(value) > 512 {
			return nil, fmt.Errorf("%s%s contains an invalid empty or oversized value", envPrefix, name)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out, nil
}

func readDefaultDenyCSV(lookup func(string) (string, bool), name string, fallback []string) ([]string, error) {
	if raw, present := lookup(envPrefix + name); present && strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	return readCSV(lookup, name, fallback)
}

func validateTCPAddress(address string) error {
	if len(address) > 255 {
		return fmt.Errorf("TCP address is too long")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("must be a host:port TCP address")
	}
	if strings.IndexFunc(host, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("host contains invalid characters")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}
