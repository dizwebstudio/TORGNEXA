package api

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

type openAPIOperation struct {
	method string
	path   string
}

func TestProductionRouteRegistryIsAcceptedBySecurityComposition(t *testing.T) {
	principal := Principal{Issuer: "https://id.example.test", Subject: "route-parity"}
	_, err := NewProductionHandler(
		testSecurityLogger(),
		edgeTestConfig(),
		securityedge.NewLimiter(),
		authnStub{principal: principal},
		tenantStub{scope: validTestScope(t)},
		authzStub{},
		newProductionRoutes(productionRouteDependencies{}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFormerlyReservedContractRoutesAreProductionAdapters(t *testing.T) {
	routes := newReservedContractRoutes(productionRouteDependencies{})
	if len(routes) != 9 {
		t.Fatalf("got %d production adapters, want 9", len(routes))
	}
	for _, route := range routes {
		request := httptest.NewRequest(route.Method, route.Path, nil)
		response := httptest.NewRecorder()
		route.Handler.ServeHTTP(response, request)
		if response.Code == http.StatusNotImplemented {
			t.Errorf("%s %s is still a 501 placeholder", route.Method, route.Path)
		}
	}
}

func TestProductionRoutesCoverOpenAPI(t *testing.T) {
	operations, pathCount := readOpenAPIOperations(t, "../../../contracts/openapi/torgnexa-v1.yaml")
	if pathCount != 142 || len(operations) != 171 {
		t.Fatalf("unexpected OpenAPI surface: got %d paths and %d operations", pathCount, len(operations))
	}

	routes := newProductionRoutes(productionRouteDependencies{})
	for _, operation := range operations {
		if operation.method == "GET" && operation.path == "/health" {
			continue // NewProductionHandler always installs the public health route.
		}
		if !productionRouteCovers(routes, operation) {
			t.Errorf("OpenAPI operation has no production route: %s %s", operation.method, operation.path)
		}
	}

	for _, route := range routes {
		if route.Method == "HEAD" {
			continue
		}
		covered := false
		for _, operation := range operations {
			if routeCoversOperation(route, operation) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("production route is absent from OpenAPI: %s %s", route.Method, route.Path)
		}
	}
}

func readOpenAPIOperations(t *testing.T, path string) ([]openAPIOperation, int) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	methods := map[string]bool{"get": true, "post": true, "put": true, "patch": true, "delete": true, "head": true, "options": true, "trace": true}
	var operations []openAPIOperation
	currentPath := ""
	pathCount := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  /") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(trimmed, ":") {
			currentPath = strings.TrimSuffix(trimmed, ":")
			pathCount++
			continue
		}
		if currentPath == "" || !strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "      ") || !strings.HasSuffix(trimmed, ":") {
			continue
		}
		method := strings.TrimSuffix(trimmed, ":")
		if methods[method] {
			operations = append(operations, openAPIOperation{method: strings.ToUpper(method), path: currentPath})
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(operations) == 0 {
		t.Fatal(fmt.Errorf("no OpenAPI operations found in %s", path))
	}
	return operations, pathCount
}

func productionRouteCovers(routes []ProtectedRoute, operation openAPIOperation) bool {
	for _, route := range routes {
		if routeCoversOperation(route, operation) {
			return true
		}
	}
	return false
}

func routeCoversOperation(route ProtectedRoute, operation openAPIOperation) bool {
	if route.Method != operation.method {
		return false
	}
	fullPath := "/api/v1" + operation.path
	if !route.PathPrefix {
		return route.Path == fullPath
	}
	staticPrefix := fullPath
	if parameter := strings.IndexByte(staticPrefix, '{'); parameter >= 0 {
		staticPrefix = staticPrefix[:parameter]
	}
	return strings.HasPrefix(staticPrefix, route.Path)
}
