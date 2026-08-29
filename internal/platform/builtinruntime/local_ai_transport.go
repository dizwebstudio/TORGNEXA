package builtinruntime

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	lmstudio "github.com/torgnexa/torgnexa/connectors/ai/lm-studio"
	ollama "github.com/torgnexa/torgnexa/connectors/ai/ollama"
	openwebui "github.com/torgnexa/torgnexa/connectors/ai/open-webui"
)

const maxLocalAIResponseBody = 16 << 20

// localAIHTTP is the only transport allowed to reach explicitly approved
// local AI endpoints. It resolves and pins a private address per request and
// never follows redirects or consults proxy environment variables.
type localAIHTTP struct {
	resolver *net.Resolver
	client   *http.Client
}

type localAIDialTarget struct{ ip, port string }
type localAIDialTargetKey struct{}

func withLocalAIDialTarget(ctx context.Context, target localAIDialTarget) context.Context {
	return context.WithValue(ctx, localAIDialTargetKey{}, target)
}

func localAIDialTargetFromContext(ctx context.Context) (localAIDialTarget, bool) {
	target, ok := ctx.Value(localAIDialTargetKey{}).(localAIDialTarget)
	return target, ok && target.ip != "" && target.port != ""
}

func newLocalAIHTTP() *localAIHTTP {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			target, ok := localAIDialTargetFromContext(ctx)
			if !ok {
				return nil, errors.New("local ai http: missing dial target")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(target.ip, target.port))
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}
	return &localAIHTTP{
		resolver: net.DefaultResolver,
		client: &http.Client{
			Transport: transport,
			Timeout:   120 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (h *localAIHTTP) do(ctx context.Context, baseURL, suffix string, body []byte, headers http.Header) (int, []byte, error) {
	if ctx == nil || h == nil || h.resolver == nil || h.client == nil || len(body) > maxLocalAIResponseBody {
		return 0, nil, errors.New("local ai http: invalid request")
	}
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if !IsLocalBaseURL(baseURL) || err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return 0, nil, errors.New("local ai http: invalid endpoint")
	}
	portText := parsed.Port()
	if portText == "" {
		if parsed.Scheme == "https" {
			portText = "443"
		} else {
			portText = "80"
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return 0, nil, errors.New("local ai http: invalid port")
	}
	host := parsed.Hostname()
	addresses, err := h.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
			addresses = []netip.Addr{literal}
		} else {
			return 0, nil, errors.New("local ai http: host resolution failed")
		}
	}
	var selected netip.Addr
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsLoopback() && !address.IsPrivate() {
			return 0, nil, errors.New("local ai http: non-private destination denied")
		}
		if selected == (netip.Addr{}) {
			selected = address
		}
	}
	if selected == (netip.Addr{}) {
		return 0, nil, errors.New("local ai http: no local destination")
	}
	path := strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(suffix, "/")
	if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") || strings.ContainsAny(path, "\r\n\x00") {
		return 0, nil, errors.New("local ai http: invalid path")
	}
	target := &url.URL{Scheme: parsed.Scheme, Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: path}
	req, err := http.NewRequestWithContext(withLocalAIDialTarget(ctx, localAIDialTarget{ip: selected.String(), port: strconv.Itoa(port)}), http.MethodPost, target.String(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("local ai http: request build failed: %w", err)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	response, err := h.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("local ai http: transport failed: %w", err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxLocalAIResponseBody+1))
	if err != nil || len(payload) > maxLocalAIResponseBody {
		return 0, nil, errors.New("local ai http: response too large")
	}
	return response.StatusCode, payload, nil
}

// IsLocalBaseURL reports whether raw is an explicitly approved local AI
// endpoint. It is the single host-policy predicate shared by API validation
// and the local transport; hosted providers continue to require HTTPS.
func IsLocalBaseURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "http" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Hostname() == "" || strings.ContainsAny(raw, " \t\r\n\x00") || !isApprovedLocalAIHost(parsed.Hostname()) {
		return false
	}
	if port := parsed.Port(); port != "" {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return false
		}
	}
	return parsed.Path == "" || (strings.HasPrefix(parsed.Path, "/") && !strings.Contains(parsed.Path, ".."))
}

func isApprovedLocalAIHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	switch host {
	case "localhost", "127.0.0.1", "::1", "ollama", "lm-studio", "open-webui", "host.docker.internal":
		return true
	default:
		return false
	}
}

type ollamaHTTP struct{ h *localAIHTTP }

func (t ollamaHTTP) Do(ctx context.Context, request ollama.Request) (ollama.Response, error) {
	status, body, err := t.h.do(ctx, request.BaseURL, request.Path, request.Body, localHeaders(request.Headers))
	return ollama.Response{StatusCode: status, Body: body}, err
}

type lmStudioHTTP struct{ h *localAIHTTP }

func (t lmStudioHTTP) Do(ctx context.Context, request lmstudio.Request) (lmstudio.Response, error) {
	status, body, err := t.h.do(ctx, request.BaseURL, request.Path, request.Body, localHeaders(request.Headers))
	return lmstudio.Response{StatusCode: status, Body: body}, err
}

type openWebUIHTTP struct{ h *localAIHTTP }

func (t openWebUIHTTP) Do(ctx context.Context, request openwebui.Request) (openwebui.Response, error) {
	status, body, err := t.h.do(ctx, request.BaseURL, request.Path, request.Body, localHeaders(request.Headers))
	return openwebui.Response{StatusCode: status, Body: body}, err
}

func localHeaders(values map[string]string) http.Header {
	headers := make(http.Header, len(values))
	for key, value := range values {
		headers.Set(key, value)
	}
	return headers
}

var _ ollama.Transport = ollamaHTTP{}
var _ lmstudio.Transport = lmStudioHTTP{}
var _ openwebui.Transport = openWebUIHTTP{}
