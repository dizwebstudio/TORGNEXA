package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

const generatorVersion = "torgnexa-sdkgen/v1"

type parameter struct {
	Name     string
	Location string
	Type     string
	Required bool
}

type operation struct {
	Method      string
	Path        string
	OperationID string
	Parameters  []parameter
	HasBody     bool
}

type spec struct {
	OpenAPIVersion string
	APIVersion     string
	ServerPath     string
	Operations     []operation
	SHA256         string
}

type generatedFile struct {
	Path string
	Data []byte
}

var (
	pathLine        = regexp.MustCompile(`^  (/.+):\s*$`)
	methodLine      = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	operationIDLine = regexp.MustCompile(`^\s{6}operationId:\s*([A-Za-z][A-Za-z0-9]*)\s*$`)
	// The `required:` key is itself optional in the source spec: an omitted
	// key means "not required" per OpenAPI 3.1, so it must not be mandatory
	// in the regex either. When the group doesn't participate in the match,
	// strconv.ParseBool("") below returns its false zero value, which is
	// exactly the correct default.
	inlineParameterLine          = regexp.MustCompile(`^- \{name: ([A-Za-z0-9_-]+), in: (query|path|header)(?:, required: (true|false))?, schema: \{type: (string|integer|boolean)(?:,|\})`)
	inlineSchemaRefParameterLine = regexp.MustCompile(`^- \{name: ([A-Za-z0-9_-]+), in: (query|path|header)(?:, required: (true|false))?, schema: \{\$ref: '#/components/schemas/([A-Za-z0-9_-]+)'\}\}$`)
	inlineParametersLine         = regexp.MustCompile(`^parameters: \[\{name: ([A-Za-z0-9_-]+), in: (query|path|header)(?:, required: (true|false))?, schema: \{type: (string|integer|boolean)(?:,|\})`)
	refParameterLine             = regexp.MustCompile(`^- \{\$ref: '#/components/parameters/([A-Za-z0-9_]+)'\}$`)
	inlineRefParameterLine       = regexp.MustCompile(`^parameters: \[\{\$ref: '#/components/parameters/([A-Za-z0-9_]+)'\}\]$`)
)

func main() {
	var root string
	var check bool
	flag.StringVar(&root, "root", ".", "repository root")
	flag.BoolVar(&check, "check", false, "verify generated SDKs are up to date")
	flag.Parse()

	if err := run(root, check); err != nil {
		fmt.Fprintln(os.Stderr, "sdkgen:", err)
		os.Exit(1)
	}
}

func run(root string, check bool) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	sourcePath := filepath.Join(absolute, "contracts", "openapi", "torgnexa-v1.yaml")
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read OpenAPI: %w", err)
	}
	parsed, err := parseSpec(data)
	if err != nil {
		return err
	}
	files, err := generate(parsed)
	if err != nil {
		return err
	}
	if check {
		var drift []string
		for _, file := range files {
			existing, readErr := os.ReadFile(filepath.Join(absolute, filepath.FromSlash(file.Path)))
			if readErr != nil || !bytes.Equal(existing, file.Data) {
				drift = append(drift, file.Path)
			}
		}
		if len(drift) > 0 {
			return fmt.Errorf("generated SDK drift: %s (run make sdk-generate)", strings.Join(drift, ", "))
		}
		fmt.Printf("Generated SDKs are current: %d operations, OpenAPI %s, source sha256 %s\n", len(parsed.Operations), parsed.APIVersion, parsed.SHA256)
		return nil
	}
	for _, file := range files {
		path := filepath.Join(absolute, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, file.Data, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("Generated %d SDK artifacts from %d OpenAPI operations\n", len(files), len(parsed.Operations))
	return nil
}

// resolveParameterRef expands a `$ref: '#/components/parameters/NAME'` into
// the concrete parameter it denotes. Both the multi-line list-item form
// (`- {$ref: ...}`) and the single-line inline-array form
// (`parameters: [{$ref: ...}]`) resolve through this one place so the two
// shapes can never silently diverge again.
func resolveParameterRef(name string) (parameter, error) {
	switch name {
	case "Cursor":
		return parameter{Name: "cursor", Location: "query", Type: "string", Required: false}, nil
	case "IdempotencyKey":
		return parameter{Name: "Idempotency-Key", Location: "header", Type: "string", Required: true}, nil
	default:
		return parameter{}, fmt.Errorf("unsupported parameter reference %q", name)
	}
}

func parseSpec(data []byte) (spec, error) {
	text := string(data)
	for _, forbidden := range []string{"internal/", "database/sql", "pgx", "gorm", "sqlc", "postgresql://", "postgres://"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			return spec{}, fmt.Errorf("public OpenAPI contains forbidden internal-model token %q", forbidden)
		}
	}
	lines := strings.Split(text, "\n")
	result := spec{}
	hash := sha256.Sum256(data)
	result.SHA256 = hex.EncodeToString(hash[:])
	inInfo := false
	inServers := false
	inPaths := false
	var currentPath string
	var pathParameters []parameter
	var current *operation
	addParameter := func(target *[]parameter, value parameter) {
		for _, existing := range *target {
			if existing.Name == value.Name && existing.Location == value.Location {
				return
			}
		}
		*target = append(*target, value)
	}
	parseSchemaRefParameter := func(trimmed string) (parameter, bool, error) {
		match := inlineSchemaRefParameterLine.FindStringSubmatch(trimmed)
		if match == nil {
			return parameter{}, false, nil
		}
		var typ string
		switch match[4] {
		case "SortableID":
			typ = "string"
		default:
			return parameter{}, true, fmt.Errorf("unsupported parameter schema reference %q", match[4])
		}
		required, _ := strconv.ParseBool(match[3])
		return parameter{Name: match[1], Location: match[2], Type: typ, Required: required}, true, nil
	}
	flush := func() error {
		if current == nil {
			return nil
		}
		if current.OperationID == "" {
			return fmt.Errorf("%s %s has no operationId", current.Method, current.Path)
		}
		result.Operations = append(result.Operations, *current)
		current = nil
		return nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "openapi:") {
			result.OpenAPIVersion = strings.TrimSpace(strings.TrimPrefix(line, "openapi:"))
			continue
		}
		if line == "info:" {
			inInfo = true
			inServers = false
			continue
		}
		if line == "servers:" {
			inInfo = false
			inServers = true
			continue
		}
		if line == "paths:" {
			inInfo = false
			inServers = false
			inPaths = true
			continue
		}
		if line == "components:" {
			if err := flush(); err != nil {
				return spec{}, err
			}
			inPaths = false
			continue
		}
		if inInfo && strings.HasPrefix(line, "  version:") {
			result.APIVersion = strings.TrimSpace(strings.TrimPrefix(line, "  version:"))
		}
		if inServers && strings.HasPrefix(trimmed, "- url:") {
			result.ServerPath = strings.TrimSpace(strings.TrimPrefix(trimmed, "- url:"))
		}
		if !inPaths {
			continue
		}
		if match := pathLine.FindStringSubmatch(line); match != nil {
			if err := flush(); err != nil {
				return spec{}, err
			}
			currentPath = match[1]
			pathParameters = nil
			continue
		}
		if match := methodLine.FindStringSubmatch(line); match != nil {
			if err := flush(); err != nil {
				return spec{}, err
			}
			if currentPath == "" {
				return spec{}, errors.New("HTTP method without path")
			}
			current = &operation{Method: strings.ToUpper(match[1]), Path: currentPath, Parameters: append([]parameter(nil), pathParameters...)}
			continue
		}
		if current == nil {
			match := inlineParameterLine.FindStringSubmatch(trimmed)
			if match == nil {
				match = inlineParametersLine.FindStringSubmatch(trimmed)
			}
			if match != nil {
				required, _ := strconv.ParseBool(match[3])
				addParameter(&pathParameters, parameter{Name: match[1], Location: match[2], Type: match[4], Required: required})
				continue
			}
			if parameter, ok, err := parseSchemaRefParameter(trimmed); err != nil {
				return spec{}, err
			} else if ok {
				addParameter(&pathParameters, parameter)
			}
			continue
		}
		if match := operationIDLine.FindStringSubmatch(line); match != nil {
			current.OperationID = match[1]
			continue
		}
		if strings.HasPrefix(trimmed, "requestBody:") {
			current.HasBody = true
			continue
		}
		if match := inlineParameterLine.FindStringSubmatch(trimmed); match != nil {
			required, _ := strconv.ParseBool(match[3])
			addParameter(&current.Parameters, parameter{Name: match[1], Location: match[2], Type: match[4], Required: required})
			continue
		}
		if parameter, ok, err := parseSchemaRefParameter(trimmed); err != nil {
			return spec{}, err
		} else if ok {
			addParameter(&current.Parameters, parameter)
			continue
		}
		if match := inlineParametersLine.FindStringSubmatch(trimmed); match != nil {
			required, _ := strconv.ParseBool(match[3])
			addParameter(&current.Parameters, parameter{Name: match[1], Location: match[2], Type: match[4], Required: required})
			continue
		}
		if match := refParameterLine.FindStringSubmatch(trimmed); match != nil {
			resolved, err := resolveParameterRef(match[1])
			if err != nil {
				return spec{}, err
			}
			addParameter(&current.Parameters, resolved)
			continue
		}
		if match := inlineRefParameterLine.FindStringSubmatch(trimmed); match != nil {
			resolved, err := resolveParameterRef(match[1])
			if err != nil {
				return spec{}, err
			}
			addParameter(&current.Parameters, resolved)
		}
	}
	if err := flush(); err != nil {
		return spec{}, err
	}
	if result.OpenAPIVersion == "" || result.APIVersion == "" || result.ServerPath == "" {
		return spec{}, errors.New("OpenAPI version, info.version and server path are required")
	}
	if len(result.Operations) == 0 {
		return spec{}, errors.New("no OpenAPI operations found")
	}
	seen := map[string]bool{}
	for _, op := range result.Operations {
		if seen[op.OperationID] {
			return spec{}, fmt.Errorf("duplicate operationId %q", op.OperationID)
		}
		seen[op.OperationID] = true
		for _, p := range op.Parameters {
			if p.Location == "path" && !strings.Contains(op.Path, "{"+p.Name+"}") {
				return spec{}, fmt.Errorf("%s parameter %q is not present in path", op.OperationID, p.Name)
			}
		}
	}
	sort.Slice(result.Operations, func(i, j int) bool { return result.Operations[i].OperationID < result.Operations[j].OperationID })
	return result, nil
}

func generate(s spec) ([]generatedFile, error) {
	goClient, err := renderGo(s)
	if err != nil {
		return nil, err
	}
	tsRuntime, tsTypes, err := renderTypeScript(s)
	if err != nil {
		return nil, err
	}
	pythonClient, err := renderPython(s)
	if err != nil {
		return nil, err
	}
	manifest, err := renderManifest(s)
	if err != nil {
		return nil, err
	}
	files := []generatedFile{
		{Path: "sdk/go/torgnexa/client.gen.go", Data: goClient},
		{Path: "sdk/typescript/src/client.gen.mjs", Data: tsRuntime},
		{Path: "sdk/typescript/src/client.gen.d.mts", Data: tsTypes},
		{Path: "sdk/python/torgnexa_sdk/client_gen.py", Data: pythonClient},
		{Path: "contracts/sdk/generated-manifest.json", Data: manifest},
	}
	for _, file := range files {
		lower := strings.ToLower(string(file.Data))
		for _, forbidden := range []string{"internal/", "database/sql", "pgx", "gorm", "sqlc"} {
			if strings.Contains(lower, forbidden) {
				return nil, fmt.Errorf("generated artifact %s contains forbidden internal-model token %q", file.Path, forbidden)
			}
		}
	}
	return files, nil
}

func renderGo(s spec) ([]byte, error) {
	funcs := template.FuncMap{
		"pascal":       pascal,
		"goType":       goType,
		"hasRequest":   func(op operation) bool { return op.HasBody || len(op.Parameters) > 0 },
		"pathParams":   func(op operation) []parameter { return paramsAt(op, "path") },
		"queryParams":  func(op operation) []parameter { return paramsAt(op, "query") },
		"headerParams": func(op operation) []parameter { return paramsAt(op, "header") },
		"quote":        strconv.Quote,
	}
	const source = `// Code generated by {{.Generator}}; DO NOT EDIT.
// Source: contracts/openapi/torgnexa-v1.yaml sha256={{.Spec.SHA256}}

package torgnexa

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strconv"
    "strings"
)

const (
    APIVersion = {{quote .Spec.APIVersion}}
    OpenAPIVersion = {{quote .Spec.OpenAPIVersion}}
    DefaultServerPath = {{quote .Spec.ServerPath}}
    GeneratorVersion = {{quote .Generator}}
)

type Client struct {
    baseURL string
    bearerToken string
    httpClient *http.Client
    userAgent string
}

type Config struct {
    BaseURL string
    BearerToken string
    HTTPClient *http.Client
    UserAgent string
}

type Response struct {
    StatusCode int
    Header http.Header
    Body json.RawMessage
}

type APIError struct {
    StatusCode int
    Body json.RawMessage
}

func (e *APIError) Error() string { return fmt.Sprintf("torgnexa API returned HTTP %d", e.StatusCode) }

func NewClient(config Config) (*Client, error) {
    base := strings.TrimSpace(config.BaseURL)
    if base == "" { return nil, errors.New("base URL is required") }
    parsed, err := url.Parse(base)
    if err != nil { return nil, fmt.Errorf("parse base URL: %w", err) }
    if parsed.Scheme != "http" && parsed.Scheme != "https" { return nil, errors.New("base URL must use http or https") }
    if parsed.Host == "" { return nil, errors.New("base URL host is required") }
    parsed.RawQuery = ""
    parsed.Fragment = ""
    client := config.HTTPClient
    if client == nil { client = http.DefaultClient }
    userAgent := strings.TrimSpace(config.UserAgent)
    if userAgent == "" { userAgent = "torgnexa-go-sdk/" + APIVersion }
    return &Client{baseURL: strings.TrimRight(parsed.String(), "/"), bearerToken: config.BearerToken, httpClient: client, userAgent: userAgent}, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, requestHeaders http.Header, body any) (*Response, error) {
    endpoint := c.baseURL + path
    if len(query) != 0 { endpoint += "?" + query.Encode() }
    var reader io.Reader
    if body != nil {
        encoded, err := json.Marshal(body)
        if err != nil { return nil, fmt.Errorf("encode request body: %w", err) }
        reader = bytes.NewReader(encoded)
    }
    req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
    if err != nil { return nil, err }
    req.Header.Set("Accept", "application/json")
    req.Header.Set("User-Agent", c.userAgent)
    for name, values := range requestHeaders {
        for _, value := range values { req.Header.Add(name, value) }
    }
    if body != nil { req.Header.Set("Content-Type", "application/json") }
    if strings.TrimSpace(c.bearerToken) != "" { req.Header.Set("Authorization", "Bearer "+c.bearerToken) }
    raw, err := c.httpClient.Do(req)
    if err != nil { return nil, err }
    defer raw.Body.Close()
    payload, err := io.ReadAll(io.LimitReader(raw.Body, 16<<20))
    if err != nil { return nil, err }
    response := &Response{StatusCode: raw.StatusCode, Header: raw.Header.Clone(), Body: json.RawMessage(payload)}
    if raw.StatusCode < 200 || raw.StatusCode >= 300 { return response, &APIError{StatusCode: raw.StatusCode, Body: response.Body} }
    return response, nil
}
{{range .Spec.Operations}}
{{if hasRequest .}}type {{pascal .OperationID}}Request struct {
{{range .Parameters}}    {{pascal .Name}} {{goType .}}
{{end}}{{if .HasBody}}    Body any
{{end}}}
{{end}}
func (c *Client) {{pascal .OperationID}}(ctx context.Context{{if hasRequest .}}, input {{pascal .OperationID}}Request{{end}}) (*Response, error) {
    path := {{quote .Path}}
{{range pathParams .}}    if strings.TrimSpace(input.{{pascal .Name}}) == "" { return nil, errors.New({{quote (printf "%s is required" .Name)}}) }
    path = strings.ReplaceAll(path, {{quote (printf "{%s}" .Name)}}, url.PathEscape(input.{{pascal .Name}}))
{{end}}    query := url.Values{}
{{range queryParams .}}{{if .Required}}{{if eq .Type "integer"}}    if input.{{pascal .Name}} == 0 { return nil, errors.New({{quote (printf "%s is required" .Name)}}) }
{{else if eq .Type "string"}}    if strings.TrimSpace(input.{{pascal .Name}}) == "" { return nil, errors.New({{quote (printf "%s is required" .Name)}}) }
{{end}}{{end}}{{if eq .Type "integer"}}    if input.{{pascal .Name}} != 0 { query.Set({{quote .Name}}, strconv.Itoa(input.{{pascal .Name}})) }
{{else if eq .Type "boolean"}}    if input.{{pascal .Name}} { query.Set({{quote .Name}}, "true") }
{{else}}    if input.{{pascal .Name}} != "" { query.Set({{quote .Name}}, input.{{pascal .Name}}) }
{{end}}{{end}}    requestHeaders := http.Header{}
{{range headerParams .}}{{if .Required}}    if strings.TrimSpace(input.{{pascal .Name}}) == "" { return nil, errors.New({{quote (printf "%s is required" .Name)}}) }
{{end}}    if input.{{pascal .Name}} != "" { requestHeaders.Set({{quote .Name}}, input.{{pascal .Name}}) }
{{end}}    return c.do(ctx, {{quote .Method}}, path, query, requestHeaders, {{if .HasBody}}input.Body{{else}}nil{{end}})
}
{{end}}
`
	payload := struct {
		Generator string
		Spec      spec
	}{generatorVersion, s}
	var out bytes.Buffer
	tmpl, err := template.New("go").Funcs(funcs).Parse(source)
	if err != nil {
		return nil, err
	}
	if err := tmpl.Execute(&out, payload); err != nil {
		return nil, err
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated Go SDK: %w\n%s", err, out.String())
	}
	return formatted, nil
}

func renderTypeScript(s spec) ([]byte, []byte, error) {
	var runtime bytes.Buffer
	fmt.Fprintf(&runtime, "// Code generated by %s; DO NOT EDIT.\n// Source: contracts/openapi/torgnexa-v1.yaml sha256=%s\n\n", generatorVersion, s.SHA256)
	fmt.Fprintf(&runtime, "export const API_VERSION = %q;\nexport const OPENAPI_VERSION = %q;\nexport const DEFAULT_SERVER_PATH = %q;\nexport const GENERATOR_VERSION = %q;\n\n", s.APIVersion, s.OpenAPIVersion, s.ServerPath, generatorVersion)
	runtime.WriteString(`export class APIError extends Error {
  constructor(statusCode, body) {
    super(` + "`TORGNEXA API returned HTTP ${statusCode}`" + `);
    this.name = "APIError";
    this.statusCode = statusCode;
    this.body = body;
  }
}

export class TorgnexaClient {
  constructor(config) {
    if (!config || typeof config.baseURL !== "string" || config.baseURL.trim() === "") throw new TypeError("baseURL is required");
    const parsed = new URL(config.baseURL);
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") throw new TypeError("baseURL must use http or https");
    parsed.search = "";
    parsed.hash = "";
    this.baseURL = parsed.toString().replace(/\/$/, "");
    this.bearerToken = config.bearerToken || "";
    this.fetchImpl = config.fetch || globalThis.fetch;
    if (typeof this.fetchImpl !== "function") throw new TypeError("fetch implementation is required");
    this.userAgent = config.userAgent || ("torgnexa-typescript-sdk/" + API_VERSION);
  }

  async _request(method, path, query, body, options = {}) {
    const endpoint = new URL(this.baseURL + path);
    for (const [key, value] of Object.entries(query || {})) {
      if (value !== undefined && value !== null && value !== "" && value !== 0 && value !== false) endpoint.searchParams.set(key, String(value));
    }
    const headers = { Accept: "application/json", ...(options.headers || {}) };
    if (body !== undefined && body !== null) headers["Content-Type"] = "application/json";
    if (this.bearerToken) headers.Authorization = "Bearer " + this.bearerToken;
    const raw = await this.fetchImpl(endpoint, { method, headers, body: body === undefined || body === null ? undefined : JSON.stringify(body), signal: options.signal });
    let payload = null;
    const contentType = (raw.headers.get("content-type") || "").split(";", 1)[0].trim().toLowerCase();
    const binary = contentType !== "" && !contentType.startsWith("text/") && !contentType.includes("json") && !contentType.includes("xml");
    if (binary) {
      payload = await raw.arrayBuffer();
    } else {
      const text = await raw.text();
      if (text !== "") { try { payload = JSON.parse(text); } catch { payload = text; } }
    }
    const response = { statusCode: raw.status, headers: raw.headers, body: payload };
    if (raw.status < 200 || raw.status >= 300) throw new APIError(raw.status, payload);
    return response;
  }
`)
	for _, op := range s.Operations {
		name := lowerFirst(pascal(op.OperationID))
		needsInput := op.HasBody || len(op.Parameters) > 0
		if needsInput {
			fmt.Fprintf(&runtime, "\n  async %s(input = {}, options = {}) {\n", name)
		} else {
			fmt.Fprintf(&runtime, "\n  async %s(options = {}) {\n", name)
		}
		fmt.Fprintf(&runtime, "    let path = %q;\n", op.Path)
		if needsInput {
			for _, p := range paramsAt(op, "path") {
				field := lowerFirst(pascal(p.Name))
				fmt.Fprintf(&runtime, "    if (typeof input.%s !== \"string\" || input.%s.trim() === \"\") throw new TypeError(%q);\n", field, field, p.Name+" is required")
				fmt.Fprintf(&runtime, "    path = path.replace(%q, encodeURIComponent(input.%s));\n", "{"+p.Name+"}", field)
			}
			runtime.WriteString("    const query = {};\n")
			for _, p := range paramsAt(op, "query") {
				field := lowerFirst(pascal(p.Name))
				if p.Required && p.Type == "string" {
					fmt.Fprintf(&runtime, "    if (typeof input.%s !== \"string\" || input.%s.trim() === \"\") throw new TypeError(%q);\n", field, field, p.Name+" is required")
				}
				fmt.Fprintf(&runtime, "    if (input.%s !== undefined) query[%q] = input.%s;\n", field, p.Name, field)
			}
			runtime.WriteString("    const requestHeaders = { ...(options.headers || {}) };\n")
			for _, p := range paramsAt(op, "header") {
				field := lowerFirst(pascal(p.Name))
				if p.Required {
					fmt.Fprintf(&runtime, "    if (typeof input.%s !== \"string\" || input.%s.trim() === \"\") throw new TypeError(%q);\n", field, field, p.Name+" is required")
				}
				fmt.Fprintf(&runtime, "    if (input.%s !== undefined) requestHeaders[%q] = input.%s;\n", field, p.Name, field)
			}
			body := "undefined"
			if op.HasBody {
				body = "input.body"
			}
			fmt.Fprintf(&runtime, "    return this._request(%q, path, query, %s, { ...options, headers: requestHeaders });\n", op.Method, body)
		} else {
			fmt.Fprintf(&runtime, "    return this._request(%q, path, {}, undefined, options);\n", op.Method)
		}
		runtime.WriteString("  }\n")
	}
	runtime.WriteString("}\n")

	var types bytes.Buffer
	fmt.Fprintf(&types, "// Code generated by %s; DO NOT EDIT.\n// Source: contracts/openapi/torgnexa-v1.yaml sha256=%s\n\n", generatorVersion, s.SHA256)
	types.WriteString(`export const API_VERSION: string;
export const OPENAPI_VERSION: string;
export const DEFAULT_SERVER_PATH: string;
export const GENERATOR_VERSION: string;
export interface APIResponse<T = unknown> { statusCode: number; headers: Headers; body: T; }
export interface RequestOptions { signal?: AbortSignal; headers?: Record<string, string>; }
export interface ClientConfig { baseURL: string; bearerToken?: string; userAgent?: string; fetch?: typeof fetch; }
export class APIError extends Error { statusCode: number; body: unknown; constructor(statusCode: number, body: unknown); }
`)
	for _, op := range s.Operations {
		if op.HasBody || len(op.Parameters) > 0 {
			fmt.Fprintf(&types, "export interface %sRequest {\n", pascal(op.OperationID))
			for _, p := range op.Parameters {
				optional := "?"
				if p.Required {
					optional = ""
				}
				fmt.Fprintf(&types, "  %s%s: %s;\n", lowerFirst(pascal(p.Name)), optional, tsType(p.Type))
			}
			if op.HasBody {
				types.WriteString("  body: unknown;\n")
			}
			types.WriteString("}\n")
		}
	}
	types.WriteString("export class TorgnexaClient {\n  constructor(config: ClientConfig);\n")
	for _, op := range s.Operations {
		name := lowerFirst(pascal(op.OperationID))
		if op.HasBody || len(op.Parameters) > 0 {
			fmt.Fprintf(&types, "  %s(input: %sRequest, options?: RequestOptions): Promise<APIResponse>;\n", name, pascal(op.OperationID))
		} else {
			fmt.Fprintf(&types, "  %s(options?: RequestOptions): Promise<APIResponse>;\n", name)
		}
	}
	types.WriteString("}\n")
	return runtime.Bytes(), types.Bytes(), nil
}

func renderPython(s spec) ([]byte, error) {
	var out bytes.Buffer
	fmt.Fprintf(&out, "# Code generated by %s; DO NOT EDIT.\n# Source: contracts/openapi/torgnexa-v1.yaml sha256=%s\n\n", generatorVersion, s.SHA256)
	out.WriteString("from __future__ import annotations\n\nimport json\nimport urllib.error\nimport urllib.parse\nimport urllib.request\nfrom dataclasses import dataclass\nfrom typing import Any, Callable, Mapping\n\n")
	fmt.Fprintf(&out, "API_VERSION = %q\nOPENAPI_VERSION = %q\nDEFAULT_SERVER_PATH = %q\nGENERATOR_VERSION = %q\n\n", s.APIVersion, s.OpenAPIVersion, s.ServerPath, generatorVersion)
	out.WriteString(`@dataclass(frozen=True)
class APIResponse:
    status_code: int
    headers: Mapping[str, str]
    body: Any


class APIError(RuntimeError):
    def __init__(self, status_code: int, body: Any):
        super().__init__(f"TORGNEXA API returned HTTP {status_code}")
        self.status_code = status_code
        self.body = body


Transport = Callable[[urllib.request.Request], tuple[int, Mapping[str, str], bytes]]


def _default_transport(request: urllib.request.Request) -> tuple[int, Mapping[str, str], bytes]:
    try:
        with urllib.request.urlopen(request) as response:
            return response.status, dict(response.headers.items()), response.read(16 << 20)
    except urllib.error.HTTPError as error:
        return error.code, dict(error.headers.items()), error.read(16 << 20)


class TorgnexaClient:
    def __init__(self, base_url: str, *, bearer_token: str = "", user_agent: str = "", transport: Transport | None = None):
        parsed = urllib.parse.urlsplit(base_url.strip())
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise ValueError("base_url must be an absolute http(s) URL")
        self._base_url = urllib.parse.urlunsplit((parsed.scheme, parsed.netloc, parsed.path.rstrip("/"), "", ""))
        self._bearer_token = bearer_token
        self._user_agent = user_agent or f"torgnexa-python-sdk/{API_VERSION}"
        self._transport = transport or _default_transport

    def _request(self, method: str, path: str, query: Mapping[str, Any], body: Any = None, request_headers: Mapping[str, str] | None = None) -> APIResponse:
        query_string = urllib.parse.urlencode({k: v for k, v in query.items() if v not in (None, "", 0, False)})
        endpoint = self._base_url + path + (("?" + query_string) if query_string else "")
        headers = {"Accept": "application/json", "User-Agent": self._user_agent}
        headers.update(request_headers or {})
        data = None
        if body is not None:
            data = json.dumps(body, separators=(",", ":")).encode("utf-8")
            headers["Content-Type"] = "application/json"
        if self._bearer_token:
            headers["Authorization"] = "Bearer " + self._bearer_token
        request = urllib.request.Request(endpoint, data=data, headers=headers, method=method)
        status, response_headers, raw = self._transport(request)
        payload: Any = None
        content_type = next((value for key, value in response_headers.items() if key.lower() == "content-type"), "").split(";", 1)[0].strip().lower()
        binary = bool(content_type) and not content_type.startswith("text/") and "json" not in content_type and "xml" not in content_type
        if raw and binary:
            payload = raw
        elif raw:
            text = raw.decode("utf-8")
            try:
                payload = json.loads(text)
            except json.JSONDecodeError:
                payload = text
        response = APIResponse(status, response_headers, payload)
        if status < 200 or status >= 300:
            raise APIError(status, payload)
        return response
`)
	for _, op := range s.Operations {
		name := snake(op.OperationID)
		out.WriteString("\n    def " + name + "(self")
		for _, p := range paramsAt(op, "path") {
			fmt.Fprintf(&out, ", %s: str", pythonName(p.Name))
		}
		queryParams := paramsAt(op, "query")
		headerParams := paramsAt(op, "header")
		if len(queryParams) > 0 || len(headerParams) > 0 || op.HasBody {
			out.WriteString(", *")
		}
		for _, p := range queryParams {
			parameterName := pythonName(p.Name)
			if p.Required {
				fmt.Fprintf(&out, ", %s: %s", parameterName, pyType(p.Type))
			} else {
				fmt.Fprintf(&out, ", %s: %s | None = None", parameterName, pyType(p.Type))
			}
		}
		for _, p := range headerParams {
			parameterName := pythonName(p.Name)
			if p.Required {
				fmt.Fprintf(&out, ", %s: %s", parameterName, pyType(p.Type))
			} else {
				fmt.Fprintf(&out, ", %s: %s | None = None", parameterName, pyType(p.Type))
			}
		}
		if op.HasBody {
			out.WriteString(", body: Any")
		}
		out.WriteString(") -> APIResponse:\n")
		fmt.Fprintf(&out, "        path = %q\n", op.Path)
		for _, p := range paramsAt(op, "path") {
			parameterName := pythonName(p.Name)
			fmt.Fprintf(&out, "        if not %s.strip():\n            raise ValueError(%q)\n", parameterName, p.Name+" is required")
			fmt.Fprintf(&out, "        path = path.replace(%q, urllib.parse.quote(%s, safe=\"\"))\n", "{"+p.Name+"}", parameterName)
		}
		out.WriteString("        query = {}\n")
		for _, p := range queryParams {
			parameterName := pythonName(p.Name)
			fmt.Fprintf(&out, "        if %s is not None:\n            query[%q] = %s\n", parameterName, p.Name, parameterName)
		}
		out.WriteString("        request_headers = {}\n")
		for _, p := range headerParams {
			parameterName := pythonName(p.Name)
			if p.Required {
				fmt.Fprintf(&out, "        if not %s.strip():\n            raise ValueError(%q)\n", parameterName, p.Name+" is required")
			}
			fmt.Fprintf(&out, "        if %s is not None:\n            request_headers[%q] = %s\n", parameterName, p.Name, parameterName)
		}
		body := "None"
		if op.HasBody {
			body = "body"
		}
		fmt.Fprintf(&out, "        return self._request(%q, path, query, %s, request_headers)\n", op.Method, body)
	}
	return out.Bytes(), nil
}

func renderManifest(s spec) ([]byte, error) {
	type manifestOperation struct {
		OperationID string `json:"operation_id"`
		Method      string `json:"method"`
		Path        string `json:"path"`
	}
	type artifact struct {
		Language string   `json:"language"`
		Runtime  string   `json:"minimum_runtime"`
		Paths    []string `json:"paths"`
	}
	type openAPIRecord struct {
		Path       string `json:"path"`
		SHA256     string `json:"sha256"`
		Version    string `json:"openapi_version"`
		APIVersion string `json:"api_version"`
		ServerPath string `json:"server_path"`
	}
	m := struct {
		SchemaVersion int                 `json:"schema_version"`
		Generator     string              `json:"generator"`
		OpenAPI       openAPIRecord       `json:"openapi"`
		Operations    []manifestOperation `json:"operations"`
		SDKs          []artifact          `json:"sdks"`
	}{SchemaVersion: 1, Generator: generatorVersion}
	m.OpenAPI.Path = "contracts/openapi/torgnexa-v1.yaml"
	m.OpenAPI.SHA256 = s.SHA256
	m.OpenAPI.Version = s.OpenAPIVersion
	m.OpenAPI.APIVersion = s.APIVersion
	m.OpenAPI.ServerPath = s.ServerPath
	for _, op := range s.Operations {
		m.Operations = append(m.Operations, manifestOperation{op.OperationID, op.Method, op.Path})
	}
	m.SDKs = []artifact{
		{Language: "go", Runtime: "go1.23", Paths: []string{"sdk/go/torgnexa/client.gen.go"}},
		{Language: "typescript", Runtime: "ES2022 + Fetch", Paths: []string{"sdk/typescript/src/client.gen.mjs", "sdk/typescript/src/client.gen.d.mts"}},
		{Language: "python", Runtime: "python3.11", Paths: []string{"sdk/python/torgnexa_sdk/client_gen.py"}},
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func paramsAt(op operation, location string) []parameter {
	var out []parameter
	for _, p := range op.Parameters {
		if p.Location == location {
			out = append(out, p)
		}
	}
	return out
}

func goType(p parameter) string {
	switch p.Type {
	case "integer":
		return "int"
	case "boolean":
		return "bool"
	default:
		return "string"
	}
}
func tsType(t string) string {
	if t == "integer" {
		return "number"
	}
	if t == "boolean" {
		return "boolean"
	}
	return "string"
}
func pyType(t string) string {
	if t == "integer" {
		return "int"
	}
	if t == "boolean" {
		return "bool"
	}
	return "str"
}

func pascal(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) == 1 {
		value = parts[0]
		var out []rune
		upperNext := true
		for i, r := range value {
			if i == 0 {
				out = append(out, []rune(strings.ToUpper(string(r)))...)
				upperNext = false
				continue
			}
			if upperNext {
				out = append(out, []rune(strings.ToUpper(string(r)))...)
				upperNext = false
			} else {
				out = append(out, r)
			}
		}
		return string(out)
	}
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		b.WriteString(part[1:])
	}
	return b.String()
}
func lowerFirst(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(value[:1]) + value[1:]
}
func snake(value string) string {
	runes := []rune(value)
	var b strings.Builder
	for i, r := range runes {
		isUpper := r >= 'A' && r <= 'Z'
		if isUpper {
			prevLowerOrDigit := i > 0 && ((runes[i-1] >= 'a' && runes[i-1] <= 'z') || (runes[i-1] >= '0' && runes[i-1] <= '9'))
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			prevUpper := i > 0 && runes[i-1] >= 'A' && runes[i-1] <= 'Z'
			if i > 0 && (prevLowerOrDigit || (prevUpper && nextLower)) {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pythonName(value string) string {
	// Suffixing keeps the public parameter recognizable while producing valid
	// Python for OpenAPI names such as "from", "class" and "async".
	value = snake(strings.ReplaceAll(value, "-", "_"))
	reserved := map[string]struct{}{"and": {}, "as": {}, "assert": {}, "async": {}, "await": {}, "break": {}, "class": {}, "continue": {}, "def": {}, "del": {}, "elif": {}, "else": {}, "except": {}, "finally": {}, "for": {}, "from": {}, "global": {}, "if": {}, "import": {}, "in": {}, "is": {}, "lambda": {}, "nonlocal": {}, "not": {}, "or": {}, "pass": {}, "raise": {}, "return": {}, "try": {}, "while": {}, "with": {}, "yield": {}}
	if _, ok := reserved[value]; ok {
		return value + "_"
	}
	return value
}
