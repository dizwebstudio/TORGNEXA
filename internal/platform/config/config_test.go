package config

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestLoadWithLookupDefaults(t *testing.T) {
	cfg, err := LoadWithLookup(ServiceAPI, mapLookup(nil))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}

	if cfg.Service != ServiceAPI || cfg.Environment != EnvironmentDevelopment {
		t.Fatalf("unexpected identity config: %+v", cfg)
	}
	if cfg.Log.Level != "info" || cfg.Log.Format != LogFormatJSON {
		t.Fatalf("unexpected log config: %+v", cfg.Log)
	}
	if cfg.HTTP.Address != "127.0.0.1:8080" {
		t.Fatalf("HTTP address = %q", cfg.HTTP.Address)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second || cfg.HTTP.ReadTimeout != 10*time.Second || cfg.HTTP.WriteTimeout != 10*time.Second || cfg.HTTP.IdleTimeout != 60*time.Second || cfg.HTTP.MaxHeaderBytes != 64<<10 {
		t.Fatalf("unexpected default HTTP config: %+v", cfg.HTTP)
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Fatalf("shutdown timeout = %s", cfg.ShutdownTimeout)
	}
	if cfg.Database.URL != "" || cfg.Database.MaxOpenConns != 20 || cfg.Database.MaxIdleConns != 10 || cfg.Database.ConnMaxLifetime != 30*time.Minute || cfg.Database.ConnMaxIdleTime != 5*time.Minute || cfg.Database.ConnectTimeout != 5*time.Second {
		t.Fatalf("unexpected database defaults: %+v", cfg.Database)
	}
	if cfg.ClickHouse.Endpoint != "http://127.0.0.1:8123" || cfg.ClickHouse.QueryTimeout != 5*time.Second {
		t.Fatalf("unexpected ClickHouse defaults: %+v", cfg.ClickHouse)
	}
}

func TestLoadWithLookupOverrides(t *testing.T) {
	values := map[string]string{
		"TORGNEXA_ENV":                          "staging-eu_1",
		"TORGNEXA_LOG_LEVEL":                    "warn",
		"TORGNEXA_LOG_FORMAT":                   "text",
		"TORGNEXA_LOG_ADD_SOURCE":               "true",
		"TORGNEXA_SHUTDOWN_TIMEOUT":             "45s",
		"TORGNEXA_HTTP_ADDR":                    ":9090",
		"TORGNEXA_HTTP_READ_HEADER_TIMEOUT":     "2s",
		"TORGNEXA_HTTP_READ_TIMEOUT":            "20s",
		"TORGNEXA_HTTP_WRITE_TIMEOUT":           "25s",
		"TORGNEXA_HTTP_IDLE_TIMEOUT":            "2m",
		"TORGNEXA_HTTP_MAX_HEADER_BYTES":        "32768",
		"TORGNEXA_SECURITY_TRUSTED_PROXY_CIDRS": "10.0.0.0/8,192.168.1.0/24",
		"TORGNEXA_SECURITY_ADMIN_CIDRS":         "10.1.0.0/16",
		"TORGNEXA_SECURITY_ALLOWED_ORIGINS":     "https://console.example.test",
		"TORGNEXA_SECURITY_MAX_REQUEST_BYTES":   "1048576",
		"TORGNEXA_SECURITY_MAX_UPLOAD_BYTES":    "524288",
		"TORGNEXA_SECURITY_RATE_PER_MINUTE":     "120",
		"TORGNEXA_SECURITY_HSTS_SECONDS":        "63072000",
		"DATABASE_URL":                          "postgres://app:secret@db:5432/torgnexa?sslmode=require",
		"TORGNEXA_DB_MAX_OPEN_CONNS":            "40",
		"TORGNEXA_DB_MAX_IDLE_CONNS":            "12",
		"TORGNEXA_DB_CONN_MAX_LIFETIME":         "45m",
		"TORGNEXA_DB_CONN_MAX_IDLE_TIME":        "3m",
		"TORGNEXA_DB_CONNECT_TIMEOUT":           "7s",
		"CLICKHOUSE_DSN":                        "https://clickhouse.example.test",
		"CLICKHOUSE_USERNAME":                   "reports",
		"CLICKHOUSE_PASSWORD":                   "secret",
		"TORGNEXA_CLICKHOUSE_QUERY_TIMEOUT":     "4s",
		"S3_ENDPOINT":                           "https://objects.example.test",
		"S3_BUCKET":                             "tenant-files",
		"S3_REGION":                             "ru-central-1",
		"S3_ACCESS_KEY":                         "access-key",
		"S3_SECRET_KEY":                         "secret-key",
		"TORGNEXA_S3_REQUEST_TIMEOUT":           "12s",
		"TORGNEXA_OIDC_MANAGED_ISSUER_HOSTS":    "login.example.test,id.example.test",
	}

	cfg, err := LoadWithLookup(ServiceAPI, mapLookup(values))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
	if cfg.Environment != "staging-eu_1" || cfg.Log.Level != "warn" || cfg.Log.Format != LogFormatText || !cfg.Log.AddSource {
		t.Fatalf("unexpected common config: %+v", cfg)
	}
	if cfg.ShutdownTimeout != 45*time.Second {
		t.Fatalf("shutdown timeout = %s", cfg.ShutdownTimeout)
	}
	if cfg.HTTP.Address != ":9090" || cfg.HTTP.ReadHeaderTimeout != 2*time.Second || cfg.HTTP.ReadTimeout != 20*time.Second || cfg.HTTP.WriteTimeout != 25*time.Second || cfg.HTTP.IdleTimeout != 2*time.Minute || cfg.HTTP.MaxHeaderBytes != 32768 {
		t.Fatalf("unexpected HTTP config: %+v", cfg.HTTP)
	}
	if len(cfg.Security.TrustedProxyCIDRs) != 2 || cfg.Security.AdminCIDRs[0] != "10.1.0.0/16" || cfg.Security.AllowedOrigins[0] != "https://console.example.test" || cfg.Security.MaxRequestBytes != 1048576 || cfg.Security.MaxUploadBytes != 524288 || cfg.Security.RatePerMinute != 120 || cfg.Security.HSTSSeconds != 63072000 {
		t.Fatalf("unexpected security config: %+v", cfg.Security)
	}
	if cfg.Database.URL == "" || cfg.Database.MaxOpenConns != 40 || cfg.Database.MaxIdleConns != 12 || cfg.Database.ConnMaxLifetime != 45*time.Minute || cfg.Database.ConnMaxIdleTime != 3*time.Minute || cfg.Database.ConnectTimeout != 7*time.Second {
		t.Fatalf("unexpected database config: %+v", cfg.Database)
	}
	if cfg.ClickHouse.Endpoint != "https://clickhouse.example.test" || cfg.ClickHouse.Username != "reports" || cfg.ClickHouse.Password != "secret" || cfg.ClickHouse.QueryTimeout != 4*time.Second {
		t.Fatalf("unexpected ClickHouse config: %+v", cfg.ClickHouse)
	}
	if cfg.ObjectStorage.Endpoint != "https://objects.example.test" || cfg.ObjectStorage.Bucket != "tenant-files" || cfg.ObjectStorage.Region != "ru-central-1" || cfg.ObjectStorage.AccessKey != "access-key" || cfg.ObjectStorage.SecretKey != "secret-key" || cfg.ObjectStorage.Timeout != 12*time.Second {
		t.Fatalf("unexpected object-storage config: %+v", cfg.ObjectStorage)
	}
	if len(cfg.OIDC.ManagedIssuerHosts) != 2 || cfg.OIDC.ManagedIssuerHosts[0] != "login.example.test" {
		t.Fatalf("unexpected managed OIDC issuer hosts: %+v", cfg.OIDC.ManagedIssuerHosts)
	}
}

func TestLoadWithLookupRejectsPartialObjectStorageConfiguration(t *testing.T) {
	_, err := LoadWithLookup(ServiceAPI, mapLookup(map[string]string{"S3_ENDPOINT": "https://objects.example.test", "S3_BUCKET": "tenant-files"}))
	if err == nil || !strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
}

func TestLoadWithLookupAllowsEmptyManagedIssuerAllowlist(t *testing.T) {
	cfg, err := LoadWithLookup(ServiceAPI, mapLookup(map[string]string{"OIDC_MANAGED_ISSUER_HOSTS": ""}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.OIDC.ManagedIssuerHosts) != 0 {
		t.Fatalf("managed issuer allowlist = %+v, want default deny", cfg.OIDC.ManagedIssuerHosts)
	}
}

func TestLoadWithLookupRequiresExplicitProductionAPIAddress(t *testing.T) {
	_, err := LoadWithLookup(ServiceAPI, mapLookup(map[string]string{
		"TORGNEXA_ENV": "production",
	}))
	if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR must be set explicitly") {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}

	cfg, err := LoadWithLookup(ServiceAPI, mapLookup(map[string]string{
		"TORGNEXA_ENV":                          "production",
		"TORGNEXA_HTTP_ADDR":                    ":8080",
		"TORGNEXA_SECURITY_TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err != nil {
		t.Fatalf("LoadWithLookup() explicit production address error = %v", err)
	}
	if cfg.HTTP.Address != ":8080" {
		t.Fatalf("HTTP address = %q", cfg.HTTP.Address)
	}
}

func TestLoadWithLookupRequiresExplicitProductionSecurityEdge(t *testing.T) {
	_, err := LoadWithLookup(ServiceAPI, mapLookup(map[string]string{
		"TORGNEXA_ENV":       "production",
		"TORGNEXA_HTTP_ADDR": ":8080",
	}))
	if err == nil || !strings.Contains(err.Error(), "SECURITY_TRUSTED_PROXY_CIDRS") {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
}

func TestLoadWithLookupMCPUsesHTTPConfiguration(t *testing.T) {
	cfg, err := LoadWithLookup(ServiceMCP, mapLookup(map[string]string{
		"TORGNEXA_HTTP_ADDR":         ":9191",
		"TORGNEXA_HTTP_READ_TIMEOUT": "12s",
	}))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
	if cfg.HTTP.Address != ":9191" || cfg.HTTP.ReadTimeout != 12*time.Second {
		t.Fatalf("unexpected MCP HTTP config: %+v", cfg.HTTP)
	}
}

func TestLoadWithLookupRequiresExplicitProductionMCPAddress(t *testing.T) {
	_, err := LoadWithLookup(ServiceMCP, mapLookup(map[string]string{"TORGNEXA_ENV": "production"}))
	if err == nil || !strings.Contains(err.Error(), "HTTP_ADDR must be set explicitly") {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
}

func TestLoadWithLookupIgnoresAPIOnlyValuesForBackgroundServices(t *testing.T) {
	cfg, err := LoadWithLookup(ServiceWorker, mapLookup(map[string]string{
		"TORGNEXA_HTTP_ADDR": "not-an-address",
	}))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
	if cfg.Service != ServiceWorker {
		t.Fatalf("service = %q", cfg.Service)
	}
}

func TestWorkerDefaultKafkaTopicsCoverEventCatalog(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/events/event-catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Events []struct {
			EventType string `json:"event_type"`
		} `json:"events"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	wantSet := map[string]struct{}{}
	for _, event := range catalog.Events {
		parts := strings.Split(event.EventType, ".")
		if len(parts) != 4 || !strings.HasPrefix(parts[3], "v") {
			t.Fatalf("unexpected event type %q", event.EventType)
		}
		wantSet[parts[0]+"."+parts[1]+".events."+parts[3]] = struct{}{}
	}
	cfg, err := LoadWithLookup(ServiceWorker, mapLookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	got := append([]string(nil), cfg.Worker.KafkaTopics...)
	want := make([]string, 0, len(wantSet))
	for topic := range wantSet {
		want = append(want, topic)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("worker Kafka topics drifted from event catalog\ngot:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLoadWithLookupWorkerRuntimeConfiguration(t *testing.T) {
	cfg, err := LoadWithLookup(ServiceWorker, mapLookup(map[string]string{
		"TORGNEXA_KAFKA_BROKERS":                 "kafka-1:9092,kafka-2:9092",
		"TORGNEXA_KAFKA_CONSUMER_GROUP":          "torgnexa.worker.test",
		"TORGNEXA_KAFKA_TOPICS":                  "commerce.orders.events.v1,security.upload.events.v1",
		"TORGNEXA_WORKER_POLL_INTERVAL":          "750ms",
		"TORGNEXA_WORKER_DISPATCH_BATCH":         "64",
		"TORGNEXA_WORKER_LEASE":                  "2m",
		"TORGNEXA_WORKER_RECONCILIATION_ENABLED": "false",
		"TORGNEXA_WORKER_UPLOADS_ENABLED":        "false",
		"TORGNEXA_CLAMAV_ADDRESS":                "clamav:3310",
		"TORGNEXA_SECURITY_MAX_UPLOAD_BYTES":     "33554432",
	}))
	if err != nil {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
	if len(cfg.Worker.KafkaBrokers) != 2 || cfg.Worker.KafkaBrokers[1] != "kafka-2:9092" {
		t.Fatalf("Kafka brokers = %#v", cfg.Worker.KafkaBrokers)
	}
	if cfg.Worker.KafkaConsumerGroup != "torgnexa.worker.test" || len(cfg.Worker.KafkaTopics) != 2 {
		t.Fatalf("Kafka config = %+v", cfg.Worker)
	}
	if cfg.Worker.PollInterval != 750*time.Millisecond || cfg.Worker.DispatchBatch != 64 || cfg.Worker.Lease != 2*time.Minute {
		t.Fatalf("worker bounds = %+v", cfg.Worker)
	}
	if cfg.Worker.ReconciliationEnabled || cfg.Worker.UploadsEnabled || cfg.Worker.ClamAVAddress != "clamav:3310" {
		t.Fatalf("worker flags = %+v", cfg.Worker)
	}
	if cfg.Security.MaxUploadBytes != 33554432 {
		t.Fatalf("max upload bytes = %d", cfg.Security.MaxUploadBytes)
	}
}

func TestLoadWithLookupWorkerUploadsRequireObjectStorage(t *testing.T) {
	_, err := LoadWithLookup(ServiceWorker, mapLookup(map[string]string{
		"TORGNEXA_WORKER_UPLOADS_ENABLED": "true",
	}))
	if err == nil || !strings.Contains(err.Error(), "S3 configuration is required") {
		t.Fatalf("LoadWithLookup() error = %v", err)
	}
}

func TestLoadWithLookupRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		service Service
		values  map[string]string
		want    string
	}{
		{name: "service", service: "unknown", want: "unsupported service"},
		{name: "empty", service: ServiceAPI, values: map[string]string{"TORGNEXA_LOG_LEVEL": " "}, want: "must not be empty"},
		{name: "environment", service: ServiceAPI, values: map[string]string{"TORGNEXA_ENV": "Production"}, want: "must use lowercase"},
		{name: "log level", service: ServiceAPI, values: map[string]string{"TORGNEXA_LOG_LEVEL": "trace"}, want: "must be debug"},
		{name: "log format", service: ServiceAPI, values: map[string]string{"TORGNEXA_LOG_FORMAT": "console"}, want: "must be json"},
		{name: "source", service: ServiceAPI, values: map[string]string{"TORGNEXA_LOG_ADD_SOURCE": "sometimes"}, want: "must be a boolean"},
		{name: "shutdown", service: ServiceAPI, values: map[string]string{"TORGNEXA_SHUTDOWN_TIMEOUT": "500ms"}, want: "between"},
		{name: "address", service: ServiceAPI, values: map[string]string{"TORGNEXA_HTTP_ADDR": "localhost"}, want: "host:port"},
		{name: "port", service: ServiceAPI, values: map[string]string{"TORGNEXA_HTTP_ADDR": "localhost:0"}, want: "between 1 and 65535"},
		{name: "header timeout", service: ServiceAPI, values: map[string]string{"TORGNEXA_HTTP_READ_HEADER_TIMEOUT": "31s"}, want: "between"},
		{name: "read timeout", service: ServiceAPI, values: map[string]string{"TORGNEXA_HTTP_READ_TIMEOUT": "500ms"}, want: "between"},
		{name: "write timeout", service: ServiceAPI, values: map[string]string{"TORGNEXA_HTTP_WRITE_TIMEOUT": "3m"}, want: "between"},
		{name: "idle timeout", service: ServiceAPI, values: map[string]string{"TORGNEXA_HTTP_IDLE_TIMEOUT": "11m"}, want: "between"},
		{name: "headers", service: ServiceAPI, values: map[string]string{"TORGNEXA_HTTP_MAX_HEADER_BYTES": "1024"}, want: "between"},
		{name: "security request", service: ServiceAPI, values: map[string]string{"TORGNEXA_SECURITY_MAX_REQUEST_BYTES": "512"}, want: "between"},
		{name: "security upload", service: ServiceAPI, values: map[string]string{"TORGNEXA_SECURITY_MAX_REQUEST_BYTES": "4096", "TORGNEXA_SECURITY_MAX_UPLOAD_BYTES": "8192"}, want: "between"},
		{name: "security hsts", service: ServiceAPI, values: map[string]string{"TORGNEXA_SECURITY_HSTS_SECONDS": "10"}, want: "between"},
		{name: "database URL whitespace", service: ServiceAPI, values: map[string]string{"DATABASE_URL": "postgres://user:secret@db:5432/db bad"}, want: "forbidden"},
		{name: "database open", service: ServiceAPI, values: map[string]string{"TORGNEXA_DB_MAX_OPEN_CONNS": "0"}, want: "between"},
		{name: "database idle", service: ServiceAPI, values: map[string]string{"TORGNEXA_DB_MAX_OPEN_CONNS": "2", "TORGNEXA_DB_MAX_IDLE_CONNS": "3"}, want: "between"},
		{name: "database connect timeout", service: ServiceAPI, values: map[string]string{"TORGNEXA_DB_CONNECT_TIMEOUT": "10ms"}, want: "between"},
		{name: "clickhouse credentials in URL", service: ServiceAPI, values: map[string]string{"CLICKHOUSE_DSN": "http://user:secret@clickhouse:8123"}, want: "without credentials"},
		{name: "clickhouse path", service: ServiceAPI, values: map[string]string{"CLICKHOUSE_DSN": "http://clickhouse:8123/query"}, want: "without credentials"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadWithLookup(tt.service, mapLookup(tt.values))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("LoadWithLookup() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestLoadWithLookupRequiresLookup(t *testing.T) {
	_, err := LoadWithLookup(ServiceAPI, nil)
	if err == nil {
		t.Fatal("LoadWithLookup() error = nil")
	}
}

func TestLoadWithLookupDoesNotEchoInvalidValues(t *testing.T) {
	const invalid = "Bearer-secret-that-must-not-appear"
	_, err := LoadWithLookup(ServiceAPI, mapLookup(map[string]string{
		"TORGNEXA_HTTP_ADDR": invalid,
	}))
	if err == nil {
		t.Fatal("LoadWithLookup() error = nil")
	}
	if strings.Contains(err.Error(), invalid) {
		t.Fatalf("configuration error leaked input value: %v", err)
	}
}

func TestLoadWithLookupDoesNotEchoInvalidDatabaseURL(t *testing.T) {
	const invalid = "postgres://user:database-secret@db:5432/db bad"
	_, err := LoadWithLookup(ServiceAPI, mapLookup(map[string]string{"DATABASE_URL": invalid}))
	if err == nil {
		t.Fatal("LoadWithLookup() error = nil")
	}
	if strings.Contains(err.Error(), invalid) || strings.Contains(err.Error(), "database-secret") {
		t.Fatalf("configuration error leaked database URL: %v", err)
	}
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
