package builtinruntime

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	deepseek "github.com/torgnexa/torgnexa/connectors/deepseek"
	gigachat "github.com/torgnexa/torgnexa/connectors/gigachat"
	kimi "github.com/torgnexa/torgnexa/connectors/kimi"
	moysklad "github.com/torgnexa/torgnexa/connectors/moysklad"
	onec "github.com/torgnexa/torgnexa/connectors/onec"
	openaicompatible "github.com/torgnexa/torgnexa/connectors/openai-compatible"
	ozon "github.com/torgnexa/torgnexa/connectors/ozon"
	qwen "github.com/torgnexa/torgnexa/connectors/qwen"
	wildberries "github.com/torgnexa/torgnexa/connectors/wildberries"
	woocommerce "github.com/torgnexa/torgnexa/connectors/woocommerce"
	yandexmarket "github.com/torgnexa/torgnexa/connectors/yandex-market"
	yandexgpt "github.com/torgnexa/torgnexa/connectors/yandexgpt"
	"github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
)

const maxResponseBody = 16 << 20

type httpTransport struct {
	resolver *net.Resolver
	client   *http.Client
}

// newHTTPTransport builds one pinned-dial http.Client shared by every
// outbound call this process makes. The selected (already public-IP
// verified) destination address travels per-request through dialTarget
// rather than by rebuilding the transport, so keep-alive connections to the
// same host are reused across calls instead of a fresh TCP+TLS handshake
// per request.
func newHTTPTransport() *httpTransport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(dialCtx context.Context, network, _ string) (net.Conn, error) {
			target, ok := dialTargetFromContext(dialCtx)
			if !ok {
				return nil, errors.New("provider http: missing dial target")
			}
			return dialer.DialContext(dialCtx, network, net.JoinHostPort(target, "443"))
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &httpTransport{resolver: net.DefaultResolver, client: client}
}

type dialTargetKey struct{}

func withDialTarget(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, dialTargetKey{}, ip)
}

func dialTargetFromContext(ctx context.Context) (string, bool) {
	ip, ok := ctx.Value(dialTargetKey{}).(string)
	return ip, ok && ip != ""
}

// publicIP delegates to the one SSRF address-blocklist shared with
// connectorsandbox's sandboxed provider egress path; it must not reimplement
// that blocklist locally (see connectorsandbox.PublicInternetAddress).
func publicIP(a netip.Addr) bool { return connectorsandbox.PublicInternetAddress(a) }

func (h *httpTransport) do(ctx context.Context, method, host, path string, query url.Values, body []byte, headers http.Header, basicUser, basicPass []byte) (int, []byte, string, int64, http.Header, error) {
	if ctx == nil || h == nil || h.resolver == nil || h.client == nil || method == "" || !validHost(host) || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "\r\n\x00") || len(body) > maxResponseBody {
		return 0, nil, "", 0, nil, errors.New("provider http: invalid request")
	}
	ips, err := h.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return 0, nil, "", 0, nil, errors.New("provider http: host resolution failed")
	}
	selected := ""
	for _, ip := range ips {
		addr := ip.Unmap()
		if !publicIP(addr) {
			return 0, nil, "", 0, nil, errors.New("provider http: non-public destination denied")
		}
		if selected == "" {
			selected = addr.String()
		}
	}
	if selected == "" {
		return 0, nil, "", 0, nil, errors.New("provider http: no public destination")
	}

	target := &url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	req, err := http.NewRequestWithContext(withDialTarget(ctx, selected), method, target.String(), bytes.NewReader(body))
	if err != nil {
		return 0, nil, "", 0, nil, errors.New("provider http: request build failed")
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if len(basicUser) > 0 || len(basicPass) > 0 {
		req.SetBasicAuth(string(basicUser), string(basicPass))
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return 0, nil, "", 0, nil, errors.New("provider http: transport failed")
	}
	defer resp.Body.Close()
	var responseBody io.Reader = resp.Body
	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("Content-Encoding")), "gzip") {
		zipped, gzipErr := gzip.NewReader(resp.Body)
		if gzipErr != nil {
			return 0, nil, "", 0, nil, errors.New("provider http: invalid gzip response")
		}
		defer zipped.Close()
		responseBody = zipped
	}
	payload, err := io.ReadAll(io.LimitReader(responseBody, maxResponseBody+1))
	if err != nil || len(payload) > maxResponseBody {
		return 0, nil, "", 0, nil, errors.New("provider http: response too large")
	}
	requestID := firstHeader(resp.Header, "X-Request-Id", "X-Request-ID", "Request-Id", "X-Trace-Id")
	return resp.StatusCode, payload, requestID, retryAfterMS(resp.Header.Get("Retry-After")), resp.Header.Clone(), nil
}

func validHost(host string) bool {
	if host == "" || host != strings.ToLower(strings.TrimSpace(host)) || len(host) > 253 || strings.ContainsAny(host, "/:@?#[]\\") || host == "localhost" || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
		return false
	}
	if net.ParseIP(host) != nil {
		return false
	}
	if !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	return true
}

func firstHeader(h http.Header, names ...string) string {
	for _, n := range names {
		if v := strings.TrimSpace(h.Get(n)); v != "" {
			if len(v) > 256 {
				return v[:256]
			}
			return v
		}
	}
	return ""
}
func retryAfterMS(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(v, 10, 64); err == nil && seconds >= 0 && seconds <= 86400 {
		return seconds * 1000
	}
	return 0
}

// These adapters are intentionally confined to the reviewed built-in provider composition boundary.
type wbHTTP struct{ h *httpTransport }

func (t wbHTTP) Do(ctx context.Context, r wildberries.Request) (wildberries.Response, error) {
	q := url.Values{}
	hdr := http.Header{}
	if len(r.Token) > 0 {
		hdr.Set("Authorization", string(r.Token))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, hdr, nil, nil)
	return wildberries.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

type ozonHTTP struct{ h *httpTransport }

func (t ozonHTTP) Do(ctx context.Context, r ozon.Request) (ozon.Response, error) {
	q := url.Values{}
	hdr := http.Header{}
	if len(r.ClientID) > 0 {
		hdr.Set("Client-Id", string(r.ClientID))
	}
	if len(r.APIKey) > 0 {
		hdr.Set("Api-Key", string(r.APIKey))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, hdr, nil, nil)
	return ozon.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

type ymHTTP struct{ h *httpTransport }

func (t ymHTTP) Do(ctx context.Context, r yandexmarket.Request) (yandexmarket.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	hdr := http.Header{}
	if len(r.APIKey) > 0 {
		hdr.Set("Api-Key", string(r.APIKey))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, hdr, nil, nil)
	return yandexmarket.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

type onecHTTP struct{ h *httpTransport }

func (t onecHTTP) Do(ctx context.Context, r onec.Request) (onec.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, nil, http.Header{}, r.Username, r.Password)
	return onec.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

type msHTTP struct{ h *httpTransport }

func (t msHTTP) Do(ctx context.Context, r moysklad.Request) (moysklad.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	hdr := http.Header{}
	if len(r.Token) > 0 {
		hdr.Set("Authorization", "Bearer "+string(r.Token))
	}
	if r.AcceptGzip {
		hdr.Set("Accept-Encoding", "gzip")
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, nil, hdr, nil, nil)
	return moysklad.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

type wooHTTP struct{ h *httpTransport }

func (t wooHTTP) Do(ctx context.Context, r woocommerce.Request) (woocommerce.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	s, b, id, ra, hdr, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, http.Header{}, r.Username, r.Password)
	pages := 0
	if hdr != nil {
		pages, _ = strconv.Atoi(hdr.Get("X-WP-TotalPages"))
	}
	return woocommerce.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra, TotalPages: pages}, e
}

type openAICompatibleHTTP struct{ h *httpTransport }

func (t openAICompatibleHTTP) Do(ctx context.Context, r openaicompatible.Request) (openaicompatible.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return openaicompatible.Response{StatusCode: s, Body: b}, e
}

type kimiHTTP struct{ h *httpTransport }

func (t kimiHTTP) Do(ctx context.Context, r kimi.Request) (kimi.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return kimi.Response{StatusCode: s, Body: b}, e
}

type qwenHTTP struct{ h *httpTransport }

func (t qwenHTTP) Do(ctx context.Context, r qwen.Request) (qwen.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return qwen.Response{StatusCode: s, Body: b}, e
}

type deepseekHTTP struct{ h *httpTransport }

func (t deepseekHTTP) Do(ctx context.Context, r deepseek.Request) (deepseek.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return deepseek.Response{StatusCode: s, Body: b}, e
}

type gigaChatHTTP struct{ h *httpTransport }

func (t gigaChatHTTP) Do(ctx context.Context, r gigachat.Request) (gigachat.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return gigachat.Response{StatusCode: s, Body: b}, e
}

type yandexGPTHTTP struct{ h *httpTransport }

func (t yandexGPTHTTP) Do(ctx context.Context, r yandexgpt.Request) (yandexgpt.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return yandexgpt.Response{StatusCode: s, Body: b}, e
}
