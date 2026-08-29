package builtinruntime

import (
	"bytes"
	"compress/gzip"
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
	"sync"
	"time"

	claude "github.com/torgnexa/torgnexa/connectors/ai/claude"
	deepseek "github.com/torgnexa/torgnexa/connectors/ai/deepseek"
	gemini "github.com/torgnexa/torgnexa/connectors/ai/gemini"
	gigachat "github.com/torgnexa/torgnexa/connectors/ai/gigachat"
	grok "github.com/torgnexa/torgnexa/connectors/ai/grok"
	kimi "github.com/torgnexa/torgnexa/connectors/ai/kimi"
	openaicompatible "github.com/torgnexa/torgnexa/connectors/ai/openai-compatible"
	qwen "github.com/torgnexa/torgnexa/connectors/ai/qwen"
	yandexgpt "github.com/torgnexa/torgnexa/connectors/ai/yandexgpt"
	bitrix24 "github.com/torgnexa/torgnexa/connectors/crm/bitrix24"
	moysklad "github.com/torgnexa/torgnexa/connectors/erp/moysklad"
	onec "github.com/torgnexa/torgnexa/connectors/erp/onec"
	cbrfx "github.com/torgnexa/torgnexa/connectors/finance/cbr-fx"
	aliexpressru "github.com/torgnexa/torgnexa/connectors/marketplaces/aliexpress-ru"
	magnitmarket "github.com/torgnexa/torgnexa/connectors/marketplaces/magnit-market"
	megamarket "github.com/torgnexa/torgnexa/connectors/marketplaces/megamarket"
	ozon "github.com/torgnexa/torgnexa/connectors/marketplaces/ozon"
	wildberries "github.com/torgnexa/torgnexa/connectors/marketplaces/wildberries"
	yandexmarket "github.com/torgnexa/torgnexa/connectors/marketplaces/yandex-market"
	maxmessenger "github.com/torgnexa/torgnexa/connectors/social/max-messenger"
	telegram "github.com/torgnexa/torgnexa/connectors/social/telegram"
	bitrixstore "github.com/torgnexa/torgnexa/connectors/storefronts/bitrix"
	cscart "github.com/torgnexa/torgnexa/connectors/storefronts/cs-cart"
	magento "github.com/torgnexa/torgnexa/connectors/storefronts/magento"
	medusa "github.com/torgnexa/torgnexa/connectors/storefronts/medusa"
	opencart "github.com/torgnexa/torgnexa/connectors/storefronts/opencart"
	prestashop "github.com/torgnexa/torgnexa/connectors/storefronts/prestashop"
	saleor "github.com/torgnexa/torgnexa/connectors/storefronts/saleor"
	shopify "github.com/torgnexa/torgnexa/connectors/storefronts/shopify"
	shopware "github.com/torgnexa/torgnexa/connectors/storefronts/shopware"
	woocommerce "github.com/torgnexa/torgnexa/connectors/storefronts/woocommerce"
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
	targets := make([]string, 0, len(ips))
	for _, ip := range ips {
		addr := ip.Unmap()
		if !publicIP(addr) {
			return 0, nil, "", 0, nil, errors.New("provider http: non-public destination denied")
		}
		targets = append(targets, addr.String())
	}
	if len(targets) == 0 {
		return 0, nil, "", 0, nil, errors.New("provider http: no public destination")
	}

	target := &url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	// Retrying a failed network connection against another already validated
	// address is safe only for reads. A write may have reached the remote before
	// the connection error and must retain the existing ambiguous-write policy.
	if method != http.MethodGet && method != http.MethodHead {
		targets = targets[:1]
	}
	var resp *http.Response
	var transportErr error
	for _, selected := range targets {
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
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}
		if len(basicUser) > 0 || len(basicPass) > 0 {
			req.SetBasicAuth(string(basicUser), string(basicPass))
		}

		resp, transportErr = h.client.Do(req)
		if transportErr == nil {
			break
		}
	}
	if transportErr != nil {
		return 0, nil, "", 0, nil, fmt.Errorf("provider http: transport failed: %w", transportErr)
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

// cbrDailyHTTP keeps one short-lived copy of the explicitly dated public XML
// document. The FX connector reads one currency at a time, while the official
// source returns the whole daily table; caching avoids one remote download per
// currency without making the cache authoritative.
type cbrDailyHTTP struct {
	h       *httpTransport
	mu      sync.Mutex
	key     string
	body    []byte
	expires time.Time
}

func newCBRDailyHTTP(transport *httpTransport) *cbrDailyHTTP {
	return &cbrDailyHTTP{h: transport}
}

func (transport *cbrDailyHTTP) Daily(ctx context.Context, at time.Time) ([]byte, error) {
	if transport == nil || transport.h == nil || ctx == nil || at.IsZero() {
		return nil, errors.New("cbr daily transport: invalid request")
	}
	moscow := time.FixedZone("MSK", 3*60*60)
	key := at.In(moscow).Format("02/01/2006")
	now := time.Now().UTC()
	transport.mu.Lock()
	if transport.key == key && now.Before(transport.expires) && len(transport.body) > 0 {
		body := append([]byte(nil), transport.body...)
		transport.mu.Unlock()
		return body, nil
	}
	transport.mu.Unlock()

	headers := http.Header{
		"Accept":     []string{"application/xml, text/xml;q=0.9"},
		"User-Agent": []string{"TORGNEXA/0.1"},
	}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodGet, "www.cbr.ru", "/scripts/XML_daily.asp", url.Values{"date_req": []string{key}}, nil, headers, nil, nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK || len(body) == 0 {
		return nil, fmt.Errorf("cbr daily transport: remote response rejected with status %d", status)
	}
	transport.mu.Lock()
	transport.key = key
	transport.body = append(transport.body[:0], body...)
	transport.expires = now.Add(15 * time.Minute)
	result := append([]byte(nil), transport.body...)
	transport.mu.Unlock()
	return result, nil
}

var _ cbrfx.Transport = (*cbrDailyHTTP)(nil)

type bitrix24HTTP struct{ h *httpTransport }

func (t bitrix24HTTP) Do(ctx context.Context, r bitrix24.Request) (bitrix24.Response, error) {
	hdr := http.Header{}
	if len(r.Bearer) > 0 {
		hdr.Set("Authorization", "Bearer "+string(r.Bearer))
	}
	status, body, requestID, retryAfter, _, err := t.h.do(ctx, r.Method, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return bitrix24.Response{StatusCode: status, Body: body, RequestID: requestID, RetryAfterMS: retryAfter}, err
}

var _ bitrix24.Transport = bitrix24HTTP{}

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
type aliexpressHTTP struct{ h *httpTransport }

func (t aliexpressHTTP) Do(ctx context.Context, r aliexpressru.Request) (aliexpressru.Response, error) {
	hdr := http.Header{}
	if len(r.XAuthToken) > 0 {
		hdr.Set("X-Auth-Token", string(r.XAuthToken))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return aliexpressru.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

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

type magnitHTTP struct{ h *httpTransport }

func (t magnitHTTP) Do(ctx context.Context, r magnitmarket.Request) (magnitmarket.Response, error) {
	hdr := http.Header{}
	if len(r.APIKey) > 0 {
		hdr.Set("X-Api-Key", string(r.APIKey))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return magnitmarket.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

type megamarketHTTP struct{ h *httpTransport }

func (t megamarketHTTP) Do(ctx context.Context, r megamarket.Request) (megamarket.Response, error) {
	hdr := http.Header{}
	if len(r.MerchantToken) > 0 {
		hdr.Set("X-Token", string(r.MerchantToken))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return megamarket.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
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

type shopifyHTTP struct{ h *httpTransport }

func (t shopifyHTTP) Do(ctx context.Context, r shopify.Request) (shopify.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	hdr := http.Header{}
	if len(r.Bearer) > 0 {
		hdr.Set("X-Shopify-Access-Token", string(r.Bearer))
	}
	s, b, id, ra, respHeader, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, hdr, nil, nil)
	nextPageInfo := ""
	if respHeader != nil {
		nextPageInfo = shopifyNextPageInfo(respHeader.Get("Link"))
	}
	return shopify.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra, NextPageInfo: nextPageInfo}, e
}

// shopifyNextPageInfo extracts the page_info query parameter from the
// rel="next" entry of Shopify's RFC 8288 Link response header
// (https://shopify.dev/docs/api/usage/pagination-rest), e.g.
// `<https://shop.myshopify.com/admin/api/2025-01/products.json?limit=50&page_info=abc>; rel="next"`.
func shopifyNextPageInfo(linkHeader string) string {
	for _, part := range strings.Split(linkHeader, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start := strings.Index(part, "<")
		end := strings.Index(part, ">")
		if start == -1 || end == -1 || end <= start {
			continue
		}
		parsed, err := url.Parse(part[start+1 : end])
		if err != nil {
			continue
		}
		return parsed.Query().Get("page_info")
	}
	return ""
}

var _ shopify.Transport = shopifyHTTP{}

type medusaHTTP struct{ h *httpTransport }

// Do sends "Authorization: Basic <token>" verbatim, per Medusa's own
// published OpenAPI security scheme for its secret admin API key
// (api_token: {type: http, scheme: basic}) — this is Medusa's own chosen
// wire format for a single opaque secret, not RFC 7617 user:pass Basic
// auth, so the raw token is set as the header value rather than passed
// through httpTransport's real HTTP-Basic (user, pass) encoding.
func (t medusaHTTP) Do(ctx context.Context, r medusa.Request) (medusa.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	hdr := http.Header{}
	if len(r.Bearer) > 0 {
		hdr.Set("Authorization", "Basic "+string(r.Bearer))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, hdr, nil, nil)
	return medusa.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

var _ medusa.Transport = medusaHTTP{}

type magentoHTTP struct{ h *httpTransport }

func (t magentoHTTP) Do(ctx context.Context, r magento.Request) (magento.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	hdr := http.Header{}
	if len(r.Bearer) > 0 {
		hdr.Set("Authorization", "Bearer "+string(r.Bearer))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, hdr, nil, nil)
	return magento.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

var _ magento.Transport = magentoHTTP{}

type saleorHTTP struct{ h *httpTransport }

func (t saleorHTTP) Do(ctx context.Context, r saleor.Request) (saleor.Response, error) {
	hdr := http.Header{}
	if len(r.Bearer) > 0 {
		hdr.Set("Authorization", "Bearer "+string(r.Bearer))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return saleor.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

var _ saleor.Transport = saleorHTTP{}

type bitrixStoreHTTP struct{ h *httpTransport }

func (t bitrixStoreHTTP) Do(ctx context.Context, r bitrixstore.Request) (bitrixstore.Response, error) {
	if len(r.UserID) == 0 || len(r.WebhookCode) == 0 || r.APIMethod == "" || strings.ContainsAny(r.APIMethod, "/?#\\\r\n\x00") {
		return bitrixstore.Response{}, errors.New("bitrix transport: invalid webhook request")
	}
	basePath := strings.TrimRight(r.BasePath, "/")
	path := basePath + "/rest/" + url.PathEscape(string(r.UserID)) + "/" + url.PathEscape(string(r.WebhookCode)) + "/" + r.APIMethod
	s, b, id, ra, _, err := t.h.do(ctx, http.MethodPost, r.Host, path, nil, r.Body, http.Header{}, nil, nil)
	return bitrixstore.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, err
}

var _ bitrixstore.Transport = bitrixStoreHTTP{}

type csCartHTTP struct{ h *httpTransport }

func (t csCartHTTP) Do(ctx context.Context, r cscart.Request) (cscart.Response, error) {
	query := url.Values{}
	for _, parameter := range r.Query {
		query.Add(parameter.Name, parameter.Value)
	}
	status, body, requestID, retryAfter, _, err := t.h.do(ctx, r.Method, r.Host, r.Path, query, r.Body, http.Header{}, r.Username, r.Password)
	return cscart.Response{StatusCode: status, Body: body, RequestID: requestID, RetryAfterMS: retryAfter}, err
}

var _ cscart.Transport = csCartHTTP{}

type openCartHTTP struct{ h *httpTransport }

func (t openCartHTTP) Do(ctx context.Context, r opencart.Request) (opencart.Response, error) {
	query := url.Values{}
	for _, parameter := range r.Query {
		query.Add(parameter.Name, parameter.Value)
	}
	hdr := http.Header{}
	if len(r.Token) > 0 {
		hdr.Set("Authorization", "Bearer "+string(r.Token))
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, query, r.Body, hdr, nil, nil)
	return opencart.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

type prestaShopHTTP struct{ h *httpTransport }

func (t prestaShopHTTP) Do(ctx context.Context, r prestashop.Request) (prestashop.Response, error) {
	query := url.Values{}
	for _, parameter := range r.Query {
		query.Add(parameter.Name, parameter.Value)
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, query, r.Body, http.Header{}, r.Username, r.Password)
	return prestashop.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
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

type claudeHTTP struct{ h *httpTransport }

func (t claudeHTTP) Do(ctx context.Context, r claude.Request) (claude.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return claude.Response{StatusCode: s, Body: b}, e
}

type geminiHTTP struct{ h *httpTransport }

func (t geminiHTTP) Do(ctx context.Context, r gemini.Request) (gemini.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return gemini.Response{StatusCode: s, Body: b}, e
}

type grokHTTP struct{ h *httpTransport }

func (t grokHTTP) Do(ctx context.Context, r grok.Request) (grok.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return grok.Response{StatusCode: s, Body: b}, e
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

type shopwareHTTP struct{ h *httpTransport }

// Do passes headers through verbatim: unlike the other storefront
// transports, connectors/storefronts/shopware manages its own OAuth2 client_credentials
// token lifecycle (Authorization: Bearer <token>, set by the connector
// itself once it has exchanged/cached one), so this transport never sets an
// auth header on its own.
func (t shopwareHTTP) Do(ctx context.Context, r shopware.Request) (shopware.Response, error) {
	q := url.Values{}
	for _, p := range r.Query {
		q.Add(p.Name, p.Value)
	}
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, id, ra, _, e := t.h.do(ctx, r.Method, r.Host, r.Path, q, r.Body, hdr, nil, nil)
	return shopware.Response{StatusCode: s, Body: b, RequestID: id, RetryAfterMS: ra}, e
}

var _ shopware.Transport = shopwareHTTP{}

type yandexGPTHTTP struct{ h *httpTransport }

func (t yandexGPTHTTP) Do(ctx context.Context, r yandexgpt.Request) (yandexgpt.Response, error) {
	hdr := http.Header{}
	for k, v := range r.Headers {
		hdr.Set(k, v)
	}
	s, b, _, _, _, e := t.h.do(ctx, http.MethodPost, r.Host, r.Path, url.Values{}, r.Body, hdr, nil, nil)
	return yandexgpt.Response{StatusCode: s, Body: b}, e
}

type telegramHTTP struct{ h *httpTransport }

func (t telegramHTTP) Do(ctx context.Context, request telegram.Request) (telegram.Response, error) {
	if t.h == nil || request.Host != "api.telegram.org" || request.Method != http.MethodPost || request.APIMethod == "" || len(request.Files) != 0 || len(request.BotToken) == 0 {
		return telegram.Response{}, errors.New("telegram http: invalid request")
	}
	form := url.Values{}
	for _, parameter := range request.Params {
		form.Add(parameter.Name, parameter.Value)
	}
	headers := http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}}
	status, body, requestID, _, _, err := t.h.do(ctx, request.Method, request.Host, "/bot"+string(request.BotToken)+"/"+request.APIMethod, url.Values{}, []byte(form.Encode()), headers, nil, nil)
	return telegram.Response{StatusCode: status, Body: body, RequestID: requestID}, err
}

type maxHTTP struct{ h *httpTransport }

func (t maxHTTP) Do(ctx context.Context, request maxmessenger.Request) (maxmessenger.Response, error) {
	if t.h == nil || request.Host != "platform-api2.max.ru" || len(request.AccessToken) == 0 || !validMAXRuntimeRequest(request) {
		return maxmessenger.Response{}, errors.New("max http: invalid request")
	}
	query := url.Values{}
	for _, parameter := range request.Params {
		query.Add(parameter.Name, parameter.Value)
	}
	headers := http.Header{"Authorization": []string{string(request.AccessToken)}}
	if len(request.Body) > 0 {
		headers.Set("Content-Type", "application/json")
	}
	status, body, requestID, retryAfter, _, err := t.h.do(ctx, request.Method, request.Host, request.Path, query, request.Body, headers, nil, nil)
	return maxmessenger.Response{StatusCode: status, Body: body, RequestID: requestID, RetryAfterMS: retryAfter}, err
}

func (maxHTTP) Upload(context.Context, maxmessenger.UploadRequest) (maxmessenger.Response, error) {
	return maxmessenger.Response{}, errors.New("max http: media upload is not admitted")
}

func validMAXRuntimeRequest(request maxmessenger.Request) bool {
	if request.Method == http.MethodPost {
		if request.Path != "/messages" || len(request.Body) == 0 || len(request.Params) != 1 || request.Params[0].Name != "chat_id" {
			return false
		}
		chatID, err := strconv.ParseInt(request.Params[0].Value, 10, 64)
		return err == nil && chatID != 0
	}
	if request.Method != http.MethodGet || len(request.Body) != 0 || len(request.Params) != 0 {
		return false
	}
	if request.Path == "/me" {
		return true
	}
	if !strings.HasPrefix(request.Path, "/") {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(request.Path, "/"), "/")
	if len(parts) != 2 && len(parts) != 4 {
		return false
	}
	if parts[0] != "chats" {
		return false
	}
	chatID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || chatID == 0 {
		return false
	}
	return len(parts) == 2 || (parts[2] == "members" && parts[3] == "me")
}
