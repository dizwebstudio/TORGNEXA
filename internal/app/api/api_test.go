package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, HealthPath, nil)
	response := httptest.NewRecorder()
	newHealthHandler(testLogger()).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	var body healthResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := healthResponse{Status: "ok", Service: "api"}
	if body != want {
		t.Fatalf("body = %+v, want %+v", body, want)
	}
}

func TestHealthHeadHasNoBody(t *testing.T) {
	response := httptest.NewRecorder()
	newHealthHandler(testLogger()).ServeHTTP(response, httptest.NewRequest(http.MethodHead, HealthPath, nil))
	if response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD response status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestRouterRejectsUnsupportedMethodAndUnknownPath(t *testing.T) {
	tests := []struct {
		method string
		path   string
		status int
	}{
		{method: http.MethodPost, path: HealthPath, status: http.StatusMethodNotAllowed},
		{method: http.MethodGet, path: "/api/v1/unknown", status: http.StatusNotFound},
	}
	for _, tt := range tests {
		response := httptest.NewRecorder()
		newHealthHandler(testLogger()).ServeHTTP(response, httptest.NewRequest(tt.method, tt.path, nil))
		if response.Code != tt.status {
			t.Fatalf("%s %s status = %d, want %d", tt.method, tt.path, response.Code, tt.status)
		}
		if response.Header().Get("Content-Type") != "application/problem+json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("unsafe problem response headers: %#v", response.Header())
		}
		var problem problemResponse
		if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
			t.Fatalf("decode problem: %v", err)
		}
		if problem.Status != tt.status || problem.Type != "about:blank" {
			t.Fatalf("problem = %+v", problem)
		}
	}
}

func TestHandlerRecoversWithoutExposingPanic(t *testing.T) {
	const sensitivePanic = "secret-panic-value"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := recoverPanics(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(sensitivePanic)
	}))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	if strings.Contains(response.Body.String(), sensitivePanic) {
		t.Fatalf("panic value leaked: %q", response.Body.String())
	}
	if strings.Contains(output.String(), sensitivePanic) || !strings.Contains(output.String(), "http.handler_panic") {
		t.Fatalf("unsafe or missing panic event: %q", output.String())
	}
}

func TestHandlerAbortsWithoutAppendingProblemAfterCommit(t *testing.T) {
	const sensitivePanic = "secret-post-commit-panic"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	handler := recoverPanics(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("accepted"))
		panic(sensitivePanic)
	}))
	response := httptest.NewRecorder()
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/panic", nil))
	}()
	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered = %#v, want http.ErrAbortHandler", recovered)
	}
	if response.Code != http.StatusAccepted || response.Body.String() != "accepted" {
		t.Fatalf("committed response was corrupted: status=%d body=%q", response.Code, response.Body.String())
	}
	if strings.Contains(output.String(), sensitivePanic) || !strings.Contains(output.String(), "http.handler_panic") {
		t.Fatalf("unsafe or missing panic event: %q", output.String())
	}
}

func TestServerDiagnosticsDoNotExposeRawText(t *testing.T) {
	const sensitiveDiagnostic = "secret-server-diagnostic"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	writer := serverLogWriter{logger: logger}
	if _, err := writer.Write([]byte(sensitiveDiagnostic)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if strings.Contains(output.String(), sensitiveDiagnostic) {
		t.Fatalf("server diagnostic leaked: %q", output.String())
	}
}

func TestRunStopsGracefully(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config.Config{
		ShutdownTimeout: time.Second,
		HTTP: config.HTTP{
			ReadHeaderTimeout: time.Second,
			ReadTimeout:       time.Second,
			WriteTimeout:      time.Second,
			IdleTimeout:       time.Second,
			MaxHeaderBytes:    4096,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	listener := newBlockingListener()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, logger, listener, newHealthHandler(logger)) }()
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not stop within the shutdown timeout")
	}
}

func TestServeDrainsActiveRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	listener := newConnectionListener()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(time.Second), testLogger(), listener, handler) }()

	client, server := net.Pipe()
	defer client.Close()
	listener.connections <- server
	request, err := http.NewRequest(http.MethodGet, "http://test/request", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if err := request.Write(client); err != nil {
		t.Fatalf("write request: %v", err)
	}
	responseDone := make(chan struct{})
	go func() {
		defer close(responseDone)
		response, readErr := http.ReadResponse(bufio.NewReader(client), request)
		if readErr == nil {
			_ = response.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		t.Fatalf("serve returned before active request completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not finish after request drained")
	}
	<-responseDone
}

func TestServeForceClosesAfterShutdownTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	handlerDone := make(chan struct{})
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		defer close(handlerDone)
		close(started)
		<-release
	})
	listener := newConnectionListener()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, testConfig(20*time.Millisecond), testLogger(), listener, handler) }()

	client, server := net.Pipe()
	listener.connections <- server
	request, err := http.NewRequest(http.MethodGet, "http://test/request", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if err := request.Write(client); err != nil {
		t.Fatalf("write request: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, client) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || err.Error() != "http_shutdown_failed" {
			t.Fatalf("serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not enforce shutdown timeout")
	}
	close(release)
	_ = client.Close()
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("force-closed handler did not exit")
	}
}

func TestServeReturnsListenerFailure(t *testing.T) {
	logger := testLogger()
	err := serve(context.Background(), testConfig(time.Second), logger, errorListener{}, newHealthHandler(logger))
	if err == nil || err.Error() != "http_serve_failed" {
		t.Fatalf("serve() error = %v", err)
	}
}

func testConfig(shutdownTimeout time.Duration) config.Config {
	return config.Config{
		ShutdownTimeout: shutdownTimeout,
		HTTP: config.HTTP{
			ReadHeaderTimeout: time.Second,
			ReadTimeout:       time.Second,
			WriteTimeout:      time.Second,
			IdleTimeout:       time.Second,
			MaxHeaderBytes:    4096,
		},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener {
	return &blockingListener{closed: make(chan struct{})}
}

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *blockingListener) Addr() net.Addr {
	return testAddr("test-listener")
}

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

type errorListener struct{}

func (errorListener) Accept() (net.Conn, error) { return nil, errors.New("listener failed") }
func (errorListener) Close() error              { return nil }
func (errorListener) Addr() net.Addr            { return testAddr("error-listener") }

type connectionListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func newConnectionListener() *connectionListener {
	return &connectionListener{
		connections: make(chan net.Conn, 1),
		closed:      make(chan struct{}),
	}
}

func (l *connectionListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *connectionListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *connectionListener) Addr() net.Addr { return testAddr("connection-listener") }

func TestOpenAPIHealthPathMatchesRouter(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	contractPath := filepath.Join(filepath.Dir(filename), "..", "..", "..", "contracts", "openapi", "torgnexa-v1.yaml")
	// #nosec G304 -- contractPath is derived from this test file and a fixed repository-relative contract path.
	contract, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatalf("read OpenAPI contract: %v", err)
	}
	text := string(contract)
	if !strings.Contains(text, "  - url: /api/v1") {
		t.Fatalf("OpenAPI contract does not map to %s", HealthPath)
	}
	healthOperation := yamlBlock(t, text, "  /health:")
	for _, expected := range []string{"    get:", "      security: []", "        '200':", "Cache-Control:", "const: no-store", "X-Content-Type-Options:", "const: nosniff", "application/json:", "$ref: '#/components/schemas/HealthResponse'"} {
		if !strings.Contains(healthOperation, expected) {
			t.Fatalf("OpenAPI health operation is missing %q", expected)
		}
	}
	healthSchema := yamlBlock(t, text, "    HealthResponse:")
	for _, expected := range []string{"required: [status, service]", "status: {type: string, const: ok}", "service: {type: string, const: api}"} {
		if !strings.Contains(healthSchema, expected) {
			t.Fatalf("OpenAPI health contract is missing %q", expected)
		}
	}
	if strings.Contains(healthSchema, "version:") {
		t.Fatal("OpenAPI health response exposes a version")
	}
}

func yamlBlock(t *testing.T, document, marker string) string {
	t.Helper()
	lines := strings.Split(document, "\n")
	start := -1
	baseIndent := len(marker) - len(strings.TrimLeft(marker, " "))
	for i, line := range lines {
		if line == marker {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("YAML block %q not found", marker)
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		indent := len(lines[i]) - len(strings.TrimLeft(lines[i], " "))
		if indent <= baseIndent {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}
