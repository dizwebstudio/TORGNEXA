package builtinruntime

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	robokassa "github.com/torgnexa/torgnexa/connectors/payments/robokassa"
	sbp "github.com/torgnexa/torgnexa/connectors/payments/sbp"
	yookassa "github.com/torgnexa/torgnexa/connectors/payments/yookassa"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

func sha256Hex(v []byte) string {
	sum := sha256.Sum256(v)
	return hex.EncodeToString(sum[:])
}

// splitBasicCredential separates a "username\npassword" secret bundle, the
// same one-secret-slot convention onec/woocommerce/prestashop already use
// for HTTP Basic Auth (see connectors/erp/onec/connector.go parseCredentialBundle).
func splitBasicCredential(secret []byte) (user, pass []byte, err error) {
	if len(secret) < 3 || len(secret) > 4096 {
		return nil, nil, errors.New("payment credential: invalid length")
	}
	parts := bytes.SplitN(secret, []byte{'\n'}, 2)
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) == 0 {
		return nil, nil, errors.New("payment credential: expected \"id\\nsecret\"")
	}
	return parts[0], parts[1], nil
}

// decimalToMinorUnits converts a YooKassa-shaped two-decimal amount string
// ("150.00") to exact integer minor units without binary float arithmetic.
func decimalToMinorUnits(value string) (int64, error) {
	value = strings.TrimSpace(value)
	negative := strings.HasPrefix(value, "-")
	if negative {
		value = value[1:]
	}
	whole, frac, hasFrac := strings.Cut(value, ".")
	if whole == "" || (hasFrac && len(frac) == 0) || len(frac) > 2 {
		return 0, errors.New("payment amount: invalid decimal")
	}
	for _, r := range whole + frac {
		if r < '0' || r > '9' {
			return 0, errors.New("payment amount: invalid digit")
		}
	}
	for len(frac) < 2 {
		frac += "0"
	}
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, errors.New("payment amount: out of range")
	}
	if negative {
		n = -n
	}
	return n, nil
}

// minorUnitsToDecimal is decimalToMinorUnits's inverse, always rendering
// exactly two fractional digits as YooKassa's API requires.
func minorUnitsToDecimal(minor int64) string {
	negative := minor < 0
	if negative {
		minor = -minor
	}
	whole, frac := minor/100, minor%100
	sign := ""
	if negative {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, whole, frac)
}

// ---- YooKassa: fixed host, Basic Auth (shopId:secretKey), well-documented
// public REST API. https://yookassa.ru/developers/api ----

const yookassaHost = "api.yookassa.ru"

type yookassaHTTP struct{ h *httpTransport }

type yookassaAmount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type yookassaPayment struct {
	ID           string          `json:"id"`
	Status       string          `json:"status"`
	Amount       yookassaAmount  `json:"amount"`
	IncomeAmount *yookassaAmount `json:"income_amount,omitempty"`
	Confirmation *struct {
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation,omitempty"`
	CreatedAt         string `json:"created_at"`
	ExpiresAt         string `json:"expires_at,omitempty"`
	CancellationError string `json:"cancellation_details,omitempty"`
}

type yookassaRefund struct {
	ID        string         `json:"id"`
	PaymentID string         `json:"payment_id"`
	Status    string         `json:"status"`
	Amount    yookassaAmount `json:"amount"`
	CreatedAt string         `json:"created_at"`
}

type yookassaNotification struct {
	Type   string          `json:"type"`
	Event  string          `json:"event"`
	Object yookassaPayment `json:"object"`
}

func (t yookassaHTTP) Ping(ctx context.Context, secret []byte) error {
	user, pass, err := splitBasicCredential(secret)
	if err != nil {
		return err
	}
	status, _, _, _, _, err := t.h.do(ctx, http.MethodGet, yookassaHost, "/v3/payments", url.Values{"limit": []string{"1"}}, nil, http.Header{}, user, pass)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("yookassa: ping rejected with status %d", status)
	}
	return nil
}

func (t yookassaHTTP) Create(ctx context.Context, secret []byte, request sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	user, pass, err := splitBasicCredential(secret)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	payload := struct {
		Amount       yookassaAmount `json:"amount"`
		Capture      bool           `json:"capture"`
		Description  string         `json:"description,omitempty"`
		Confirmation struct {
			Type string `json:"type"`
		} `json:"confirmation"`
		Metadata struct {
			ExternalID string `json:"external_id"`
		} `json:"metadata"`
	}{
		Amount:      yookassaAmount{Value: minorUnitsToDecimal(request.Amount.MinorUnits), Currency: string(request.Amount.Currency)},
		Capture:     true,
		Description: request.Purpose,
	}
	payload.Confirmation.Type = "redirect"
	payload.Metadata.ExternalID = request.ExternalID
	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	headers := http.Header{"Idempotence-Key": []string{request.IdempotencyKey}}
	status, respBody, _, _, _, err := t.h.do(ctx, http.MethodPost, yookassaHost, "/v3/payments", nil, body, headers, user, pass)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentCreateResult{}, fmt.Errorf("yookassa: create rejected with status %d", status)
	}
	var payment yookassaPayment
	if err := json.Unmarshal(respBody, &payment); err != nil || payment.ID == "" {
		return sdk.PaymentCreateResult{}, errors.New("yookassa: invalid create response")
	}
	result := sdk.PaymentCreateResult{RemoteID: payment.ID, Status: payment.Status, ObservedAt: time.Now().UTC(), ExpiresAt: request.ExpiresAt}
	if payment.Confirmation != nil {
		result.PaymentURL = payment.Confirmation.ConfirmationURL
	}
	return result, nil
}

func (t yookassaHTTP) Status(ctx context.Context, secret []byte, request sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	payment, _, err := t.fetchPayment(ctx, secret, request.RemoteID)
	if err != nil {
		return sdk.PaymentStatus{}, err
	}
	return paymentStatusFromRemote(payment)
}

func (t yookassaHTTP) fetchPayment(ctx context.Context, secret []byte, remoteID string) (yookassaPayment, int, error) {
	user, pass, err := splitBasicCredential(secret)
	if err != nil {
		return yookassaPayment{}, 0, err
	}
	if remoteID == "" {
		return yookassaPayment{}, 0, errors.New("yookassa: remote id required")
	}
	status, body, _, _, _, err := t.h.do(ctx, http.MethodGet, yookassaHost, "/v3/payments/"+url.PathEscape(remoteID), nil, nil, http.Header{}, user, pass)
	if err != nil {
		return yookassaPayment{}, 0, err
	}
	if status < 200 || status >= 300 {
		return yookassaPayment{}, status, fmt.Errorf("yookassa: status fetch rejected with status %d", status)
	}
	var payment yookassaPayment
	if err := json.Unmarshal(body, &payment); err != nil || payment.ID == "" {
		return yookassaPayment{}, status, errors.New("yookassa: invalid status response")
	}
	return payment, status, nil
}

func paymentStatusFromRemote(payment yookassaPayment) (sdk.PaymentStatus, error) {
	minor, err := decimalToMinorUnits(payment.Amount.Value)
	if err != nil {
		return sdk.PaymentStatus{}, err
	}
	var commission int64
	if payment.IncomeAmount != nil {
		income, incomeErr := decimalToMinorUnits(payment.IncomeAmount.Value)
		if incomeErr == nil && minor > income {
			commission = minor - income
		}
	}
	return sdk.PaymentStatus{
		RemoteID:             payment.ID,
		Status:               payment.Status,
		Amount:               sdk.PaymentAmount{MinorUnits: minor, Currency: payment.Amount.Currency},
		CommissionMinorUnits: commission,
		ObservedAt:           time.Now().UTC(),
	}, nil
}

func (t yookassaHTTP) Refund(ctx context.Context, secret []byte, request sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	user, pass, err := splitBasicCredential(secret)
	if err != nil {
		return sdk.PaymentRefundResult{}, err
	}
	payload := struct {
		PaymentID string         `json:"payment_id"`
		Amount    yookassaAmount `json:"amount"`
	}{PaymentID: request.RemotePaymentID, Amount: yookassaAmount{Value: minorUnitsToDecimal(request.Amount.MinorUnits), Currency: string(request.Amount.Currency)}}
	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.PaymentRefundResult{}, err
	}
	headers := http.Header{"Idempotence-Key": []string{request.IdempotencyKey}}
	status, respBody, _, _, _, err := t.h.do(ctx, http.MethodPost, yookassaHost, "/v3/refunds", nil, body, headers, user, pass)
	if err != nil {
		return sdk.PaymentRefundResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentRefundResult{}, fmt.Errorf("yookassa: refund rejected with status %d", status)
	}
	var refund yookassaRefund
	if err := json.Unmarshal(respBody, &refund); err != nil || refund.ID == "" {
		return sdk.PaymentRefundResult{}, errors.New("yookassa: invalid refund response")
	}
	return sdk.PaymentRefundResult{RemoteRefundID: refund.ID, Status: refund.Status, ObservedAt: time.Now().UTC()}, nil
}

// Reconcile lists refunds is not directly supported by a single YooKassa
// endpoint; instead this walks the payments list for the window. Amounts
// already flow through the same decimal parser as Create/Status.
func (t yookassaHTTP) Reconcile(ctx context.Context, secret []byte, request sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	user, pass, err := splitBasicCredential(secret)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	query := url.Values{
		"created_at.gte": []string{request.From.UTC().Format(time.RFC3339)},
		"created_at.lte": []string{request.To.UTC().Format(time.RFC3339)},
		"limit":          []string{"100"},
	}
	status, body, _, _, _, err := t.h.do(ctx, http.MethodGet, yookassaHost, "/v3/payments", query, nil, http.Header{}, user, pass)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentReconcileResult{}, fmt.Errorf("yookassa: reconcile rejected with status %d", status)
	}
	var page struct {
		Items []yookassaPayment `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return sdk.PaymentReconcileResult{}, errors.New("yookassa: invalid reconcile response")
	}
	items := make([]sdk.PaymentSettlement, 0, len(page.Items))
	for _, payment := range page.Items {
		minor, amountErr := decimalToMinorUnits(payment.Amount.Value)
		if amountErr != nil {
			continue
		}
		occurred, timeErr := time.Parse(time.RFC3339, payment.CreatedAt)
		if timeErr != nil {
			occurred = time.Now().UTC()
		}
		items = append(items, sdk.PaymentSettlement{RemoteID: payment.ID, Kind: "sale", Amount: sdk.PaymentAmount{MinorUnits: minor, Currency: payment.Amount.Currency}, OccurredAt: occurred.UTC()})
	}
	return sdk.PaymentReconcileResult{Items: items, ObservedAt: time.Now().UTC()}, nil
}

// VerifyWebhook deliberately ignores the claimed status inside body: YooKassa
// notifications carry no cryptographic signature, so ADR-0071 requires
// re-fetching the authoritative record from the API before accepting any
// state change — eventType/remotePaymentID below come only from that
// verified re-fetch, never from the untrusted body. deliveryID is a content
// hash of the raw notification, which is what makes true provider retries
// (identical bytes) dedup correctly while a genuinely new state transition
// (different bytes) is treated as a new delivery. sig is reserved for
// providers that do sign deliveries; YooKassa does not.
func (t yookassaHTTP) VerifyWebhook(ctx context.Context, secret, body, sig []byte) (deliveryID, eventType, remotePaymentID string, err error) {
	var notification yookassaNotification
	if jsonErr := json.Unmarshal(body, &notification); jsonErr != nil || notification.Object.ID == "" || notification.Event == "" {
		return "", "", "", errors.New("yookassa: invalid webhook body")
	}
	verified, _, fetchErr := t.fetchPayment(ctx, secret, notification.Object.ID)
	if fetchErr != nil {
		return "", "", "", fetchErr
	}
	return sha256Hex(body)[:32], "payment_" + verified.Status, verified.ID, nil
}

var _ yookassa.Transport = yookassaHTTP{}

// ---- SBP: host-injected acquiring gateway, mTLS client certificate. There
// is no single universal SBP host; every acquiring bank fronts the same
// NSPK C2B protocol shape on its own gateway (ADR-0071). This implements
// that common shape with the merchant-configured GatewayHost. Field names
// follow the widely published NSPK QR C2B contract (order registration and
// status polling) and are intentionally the seam a specific bank's exact
// dialect would be adjusted against once real credentials exist — see the
// "Known limitation" note in the payments plan.
type sbpHTTP struct {
	base *httpTransport
	// certTransport is normally nil, in which case transportFor builds the
	// real mTLS client via newCertHTTPTransport. Tests override it to point
	// the resulting client at a local listener without a real DNS-resolvable
	// gateway host — see paymentstransport_test.go.
	certTransport func(secret []byte) (*httpTransport, error)
}

func newSBPHTTP(base *httpTransport) sbpHTTP { return sbpHTTP{base: base} }

// transportFor builds a fresh, unpooled mTLS client for exactly one call
// (see newCertHTTPTransport) from the account's certificate secret. It is
// never cached: this method is invoked inside the caller's UseSecret
// callback and the resulting *httpTransport is discarded when that call
// returns.
func (t sbpHTTP) transportFor(secret []byte) (*httpTransport, error) {
	if t.certTransport != nil {
		return t.certTransport(secret)
	}
	cert, err := parseClientCertificate(secret)
	if err != nil {
		return nil, err
	}
	return newCertHTTPTransport(t.base.resolver, cert), nil
}

type sbpOrderRequest struct {
	MemberID string `json:"memberId,omitempty"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	OrderID  string `json:"order,omitempty"`
	Purpose  string `json:"purpose,omitempty"`
}

type sbpOrderResponse struct {
	QRCID      string `json:"qrcId"`
	PayloadURL string `json:"payload"`
}

type sbpStatusResponse struct {
	Status   string `json:"status"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

func (t sbpHTTP) Ping(ctx context.Context, host string, secret []byte) error {
	transport, err := t.transportFor(secret)
	if err != nil {
		return err
	}
	status, _, _, _, _, err := transport.do(ctx, http.MethodGet, host, "/health", nil, nil, http.Header{}, nil, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("sbp: ping rejected with status %d", status)
	}
	return nil
}

// sbpCreateRequestBody and parseSBPOrderResponse are split out of Create as
// pure functions so the wire contract can be tested directly, independent
// of do()'s SSRF-safe DNS resolution (which correctly refuses to resolve any
// non-public test host — see paymentstransport_test.go).
func sbpCreateRequestBody(request sdk.PaymentCreateRequest) ([]byte, error) {
	return json.Marshal(sbpOrderRequest{Amount: request.Amount.MinorUnits, Currency: string(request.Amount.Currency), OrderID: request.ExternalID, Purpose: request.Purpose})
}

func parseSBPOrderResponse(body []byte) (sbpOrderResponse, error) {
	var order sbpOrderResponse
	if err := json.Unmarshal(body, &order); err != nil || order.QRCID == "" {
		return sbpOrderResponse{}, errors.New("sbp: invalid create response")
	}
	return order, nil
}

func (t sbpHTTP) Create(ctx context.Context, host string, secret []byte, request sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	transport, err := t.transportFor(secret)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	payload, err := sbpCreateRequestBody(request)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	headers := http.Header{"Idempotency-Key": []string{request.IdempotencyKey}}
	status, body, _, _, _, err := transport.do(ctx, http.MethodPost, host, "/qr/c2b/register", nil, payload, headers, nil, nil)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentCreateResult{}, fmt.Errorf("sbp: create rejected with status %d", status)
	}
	order, err := parseSBPOrderResponse(body)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	return sdk.PaymentCreateResult{RemoteID: order.QRCID, Status: "created", PaymentURL: order.PayloadURL, ExpiresAt: request.ExpiresAt, ObservedAt: time.Now().UTC()}, nil
}

func parseSBPStatusResponse(body []byte) (sbpStatusResponse, error) {
	var remote sbpStatusResponse
	if err := json.Unmarshal(body, &remote); err != nil || remote.Status == "" {
		return sbpStatusResponse{}, errors.New("sbp: invalid status response")
	}
	return remote, nil
}

func (t sbpHTTP) Status(ctx context.Context, host string, secret []byte, request sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	transport, err := t.transportFor(secret)
	if err != nil {
		return sdk.PaymentStatus{}, err
	}
	status, body, _, _, _, err := transport.do(ctx, http.MethodGet, host, "/qr/c2b/orders/"+url.PathEscape(request.RemoteID), nil, nil, http.Header{}, nil, nil)
	if err != nil {
		return sdk.PaymentStatus{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentStatus{}, fmt.Errorf("sbp: status rejected with status %d", status)
	}
	remote, err := parseSBPStatusResponse(body)
	if err != nil {
		return sdk.PaymentStatus{}, err
	}
	return sdk.PaymentStatus{RemoteID: request.RemoteID, Status: remote.Status, Amount: sdk.PaymentAmount{MinorUnits: remote.Amount, Currency: remote.Currency}, ObservedAt: time.Now().UTC()}, nil
}

func (t sbpHTTP) Refund(ctx context.Context, host string, secret []byte, request sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	transport, err := t.transportFor(secret)
	if err != nil {
		return sdk.PaymentRefundResult{}, err
	}
	payload, err := json.Marshal(struct {
		QRCID  string `json:"qrcId"`
		Amount int64  `json:"amount"`
		Order  string `json:"order,omitempty"`
	}{QRCID: request.RemotePaymentID, Amount: request.Amount.MinorUnits, Order: request.ExternalID})
	if err != nil {
		return sdk.PaymentRefundResult{}, err
	}
	headers := http.Header{"Idempotency-Key": []string{request.IdempotencyKey}}
	status, body, _, _, _, err := transport.do(ctx, http.MethodPost, host, "/qr/c2b/refund", nil, payload, headers, nil, nil)
	if err != nil {
		return sdk.PaymentRefundResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentRefundResult{}, fmt.Errorf("sbp: refund rejected with status %d", status)
	}
	var refund struct {
		RefundID string `json:"refundId"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(body, &refund); err != nil || refund.RefundID == "" {
		return sdk.PaymentRefundResult{}, errors.New("sbp: invalid refund response")
	}
	return sdk.PaymentRefundResult{RemoteRefundID: refund.RefundID, Status: refund.Status, ObservedAt: time.Now().UTC()}, nil
}

func (t sbpHTTP) Reconcile(ctx context.Context, host string, secret []byte, request sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	transport, err := t.transportFor(secret)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	query := url.Values{"from": []string{request.From.UTC().Format(time.RFC3339)}, "to": []string{request.To.UTC().Format(time.RFC3339)}}
	status, body, _, _, _, err := transport.do(ctx, http.MethodGet, host, "/qr/c2b/orders", query, nil, http.Header{}, nil, nil)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentReconcileResult{}, fmt.Errorf("sbp: reconcile rejected with status %d", status)
	}
	var page struct {
		Orders []struct {
			QRCID     string `json:"qrcId"`
			Amount    int64  `json:"amount"`
			Currency  string `json:"currency"`
			CreatedAt string `json:"createdAt"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return sdk.PaymentReconcileResult{}, errors.New("sbp: invalid reconcile response")
	}
	items := make([]sdk.PaymentSettlement, 0, len(page.Orders))
	for _, order := range page.Orders {
		occurred, timeErr := time.Parse(time.RFC3339, order.CreatedAt)
		if timeErr != nil {
			occurred = time.Now().UTC()
		}
		items = append(items, sdk.PaymentSettlement{RemoteID: order.QRCID, Kind: "sale", Amount: sdk.PaymentAmount{MinorUnits: order.Amount, Currency: order.Currency}, OccurredAt: occurred.UTC()})
	}
	return sdk.PaymentReconcileResult{Items: items, ObservedAt: time.Now().UTC()}, nil
}

// VerifyWebhook re-fetches the order status through the mTLS-authenticated
// channel before trusting anything the notification body claims, matching
// ADR-0071. sig carries the gateway's delivery signature where the specific
// acquiring bank provides one; verifying it is bank-specific and left for
// that bank's integration to plug in alongside GatewayHost.
func (t sbpHTTP) VerifyWebhook(ctx context.Context, host string, secret, body, sig []byte) (deliveryID, eventType, remotePaymentID string, err error) {
	var notification struct {
		QRCID  string `json:"qrcId"`
		Status string `json:"status"`
	}
	if jsonErr := json.Unmarshal(body, &notification); jsonErr != nil || notification.QRCID == "" {
		return "", "", "", errors.New("sbp: invalid webhook body")
	}
	verified, err := t.Status(ctx, host, secret, sdk.PaymentStatusRequest{RemoteID: notification.QRCID})
	if err != nil {
		return "", "", "", err
	}
	return sha256Hex(body)[:32], "payment_" + verified.Status, verified.RemoteID, nil
}

var _ sbp.Transport = sbpHTTP{}

// ---- Robokassa: fixed hosts, JWT-signed (HMAC-MD5) CreateInvoice REST API,
// legacy XML OpStateExt status API, MD5-signed ResultURL webhook. Verified
// against Robokassa's own PHP SDK (github.com/robokassa/sdk-php) and
// https://docs.robokassa.ru//ru/xml-interfaces. There is no merchant-level
// refund API (only a Partner/aggregator RefundOperation, a distinct business
// relationship), so Refund fails closed rather than faking success. ----

const (
	robokassaInvoiceHost = "services.robokassa.ru"
	robokassaAuthHost    = "auth.robokassa.ru"
)

type robokassaHTTP struct{ h *httpTransport }

// splitRobokassaCredential separates the "login\npassword1\npassword2"
// three-part secret bundle Robokassa's API requires: Password1 signs
// CreateInvoice requests, Password2 signs OpStateExt/ResultURL callbacks.
func splitRobokassaCredential(secret []byte) (login, password1, password2 []byte, err error) {
	if len(secret) < 5 || len(secret) > 4096 {
		return nil, nil, nil, errors.New("payment credential: invalid length")
	}
	parts := bytes.SplitN(secret, []byte{'\n'}, 3)
	if len(parts) != 3 || len(parts[0]) == 0 || len(parts[1]) == 0 || len(parts[2]) == 0 {
		return nil, nil, nil, errors.New("payment credential: expected \"login\\npassword1\\npassword2\"")
	}
	return parts[0], parts[1], parts[2], nil
}

func robokassaMD5Hex(v string) string {
	sum := md5.Sum([]byte(v))
	return hex.EncodeToString(sum[:])
}

// robokassaInvID deterministically derives Robokassa's required positive
// 31-bit integer InvId from ExternalID: Robokassa's JWT CreateInvoice flow
// has no auto-assigned invoice id, the merchant must supply one, and this
// must be stable across create/status idempotent retries for the same
// ExternalID without a separate counter or DB round-trip.
func robokassaInvID(externalID string) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(externalID))
	return int64(h.Sum32() &^ (1 << 31))
}

type robokassaJWTPayload struct {
	MerchantLogin string  `json:"MerchantLogin"`
	InvoiceType   string  `json:"InvoiceType"`
	Culture       string  `json:"Culture"`
	InvId         int64   `json:"InvId"`
	OutSum        float64 `json:"OutSum"`
	Description   string  `json:"Description,omitempty"`
}

// robokassaSignJWT mirrors sdk-php's jwtSignMd5: HMAC-MD5 over
// "base64url(header).base64url(payload)", keyed by "login:password1", with
// the raw digest itself base64url-encoded as the third JWT segment.
func robokassaSignJWT(login, password1 string, invID int64, outSum float64, description string) (string, error) {
	header := []byte(`{"alg":"MD5","typ":"JWT"}`)
	payload, err := json.Marshal(robokassaJWTPayload{MerchantLogin: login, InvoiceType: "OneTime", Culture: "ru", InvId: invID, OutSum: outSum, Description: description})
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(md5.New, []byte(login+":"+password1))
	mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

type robokassaOpState struct {
	XMLName xml.Name `xml:"OperationStateResponse"`
	Result  struct {
		Code int `xml:"Code"`
	} `xml:"Result"`
	State struct {
		Code int `xml:"Code"`
	} `xml:"State"`
	Info struct {
		OutSum string `xml:"OutSum"`
	} `xml:"Info"`
}

// robokassaStatusFromCode maps the documented State.Code values
// (https://docs.robokassa.ru//ru/xml-interfaces) conservatively: only 100
// ("funds credited") is treated as succeeded and only 10 ("cancelled") as
// failed. Every transitional/uncertain code (5 initiated, 20 hold, 50
// crediting-in-progress, 60 refused/returned, 80 suspended) is left
// "pending" rather than guessed, since paymentsCanonicalStatus treats an
// unrecognized status as still-pending — the fail-closed choice.
func robokassaStatusFromCode(code int) string {
	switch code {
	case 100:
		return "succeeded"
	case 10:
		return "canceled"
	default:
		return "pending"
	}
}

func (t robokassaHTTP) fetchState(ctx context.Context, secret []byte, invID string) (robokassaOpState, error) {
	login, _, password2, err := splitRobokassaCredential(secret)
	if err != nil {
		return robokassaOpState{}, err
	}
	if invID == "" {
		return robokassaOpState{}, errors.New("robokassa: remote id required")
	}
	sig := robokassaMD5Hex(string(login) + ":" + invID + ":" + string(password2))
	query := url.Values{"MerchantLogin": []string{string(login)}, "InvoiceID": []string{invID}, "Signature": []string{sig}}
	status, body, _, _, _, err := t.h.do(ctx, http.MethodGet, robokassaAuthHost, "/Merchant/WebService/Service.asmx/OpStateExt", query, nil, http.Header{}, nil, nil)
	if err != nil {
		return robokassaOpState{}, err
	}
	if status < 200 || status >= 300 {
		return robokassaOpState{}, fmt.Errorf("robokassa: status fetch rejected with status %d", status)
	}
	var state robokassaOpState
	if err := xml.Unmarshal(body, &state); err != nil {
		return robokassaOpState{}, errors.New("robokassa: invalid status response")
	}
	if state.Result.Code == 1 {
		return robokassaOpState{}, errors.New("robokassa: invalid signature rejected by gateway")
	}
	return state, nil
}

func (t robokassaHTTP) Ping(ctx context.Context, secret []byte) error {
	// OpStateExt against an invoice id that will not exist is the closest
	// thing to a health probe this API offers without creating a real
	// invoice: Result.Code=3 ("operation not found") still proves the
	// credential pair itself was accepted, while Result.Code=1 ("invalid
	// signature") proves it was not.
	_, err := t.fetchState(ctx, secret, "1")
	return err
}

func (t robokassaHTTP) Create(ctx context.Context, secret []byte, request sdk.PaymentCreateRequest) (sdk.PaymentCreateResult, error) {
	login, password1, _, err := splitRobokassaCredential(secret)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	if request.Amount.Currency != "RUB" {
		return sdk.PaymentCreateResult{}, errors.New("robokassa: only RUB is supported")
	}
	invID := robokassaInvID(request.ExternalID)
	outSum, err := strconv.ParseFloat(minorUnitsToDecimal(request.Amount.MinorUnits), 64)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	token, err := robokassaSignJWT(string(login), string(password1), invID, outSum, request.Purpose)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	body, err := json.Marshal(token)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	status, respBody, _, _, _, err := t.h.do(ctx, http.MethodPost, robokassaInvoiceHost, "/InvoiceServiceWebApi/api/CreateInvoice", nil, body, headers, nil, nil)
	if err != nil {
		return sdk.PaymentCreateResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentCreateResult{}, fmt.Errorf("robokassa: create rejected with status %d", status)
	}
	var response struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(respBody, &response); err != nil || response.URL == "" {
		return sdk.PaymentCreateResult{}, errors.New("robokassa: invalid create response")
	}
	return sdk.PaymentCreateResult{RemoteID: strconv.FormatInt(invID, 10), Status: "pending", PaymentURL: response.URL, ExpiresAt: request.ExpiresAt, ObservedAt: time.Now().UTC()}, nil
}

func (t robokassaHTTP) Status(ctx context.Context, secret []byte, request sdk.PaymentStatusRequest) (sdk.PaymentStatus, error) {
	state, err := t.fetchState(ctx, secret, request.RemoteID)
	if err != nil {
		return sdk.PaymentStatus{}, err
	}
	minor, amountErr := decimalToMinorUnits(state.Info.OutSum)
	if amountErr != nil {
		minor = 0
	}
	return sdk.PaymentStatus{RemoteID: request.RemoteID, Status: robokassaStatusFromCode(state.State.Code), Amount: sdk.PaymentAmount{MinorUnits: minor, Currency: "RUB"}, ObservedAt: time.Now().UTC()}, nil
}

// Refund: Robokassa exposes no merchant-level refund API — only a
// Partner/aggregator "RefundOperation" that requires a registered-partner
// relationship distinct from a regular merchant account. Rather than
// silently no-op or fabricate a success response, this fails closed with a
// normalized "unsupported" remote error, no network call made.
func (t robokassaHTTP) Refund(context.Context, []byte, sdk.PaymentRefundRequest) (sdk.PaymentRefundResult, error) {
	remoteErr, err := sdk.NewRemoteError(sdk.ErrorUnsupported, "refund_not_supported", "", 0)
	if err != nil {
		return sdk.PaymentRefundResult{}, err
	}
	return sdk.PaymentRefundResult{}, remoteErr
}

// Reconcile: GetInvoiceInformationList is a date/status filtered list report
// (CurrentPage/PageSize/InvoiceStatuses/DateFrom/DateTo/InvoiceTypes), not a
// single-invoice lookup, so it fits this window-based reconciliation shape
// directly — unlike Status, which needs one specific InvId and uses
// OpStateExt instead.
func (t robokassaHTTP) Reconcile(ctx context.Context, secret []byte, request sdk.PaymentReconcileRequest) (sdk.PaymentReconcileResult, error) {
	login, password1, _, err := splitRobokassaCredential(secret)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	filters := map[string]any{
		"MerchantLogin":   string(login),
		"CurrentPage":     1,
		"PageSize":        100,
		"InvoiceStatuses": []string{"Paid"},
		"DateFrom":        request.From.UTC().Format(time.RFC3339),
		"DateTo":          request.To.UTC().Format(time.RFC3339),
		"InvoiceTypes":    []string{"OneTime"},
	}
	payload, err := json.Marshal(filters)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	header := []byte(`{"alg":"MD5","typ":"JWT"}`)
	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(md5.New, []byte(string(login)+":"+string(password1)))
	mac.Write([]byte(signingInput))
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	body, err := json.Marshal(token)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	status, respBody, _, _, _, err := t.h.do(ctx, http.MethodPost, robokassaInvoiceHost, "/InvoiceServiceWebApi/api/GetInvoiceInformationList", nil, body, headers, nil, nil)
	if err != nil {
		return sdk.PaymentReconcileResult{}, err
	}
	if status < 200 || status >= 300 {
		return sdk.PaymentReconcileResult{}, fmt.Errorf("robokassa: reconcile rejected with status %d", status)
	}
	var items []struct {
		InvId     int64   `json:"InvId"`
		OutSum    float64 `json:"OutSum"`
		State     string  `json:"State"`
		StateDate string  `json:"StateDate"`
	}
	if err := json.Unmarshal(respBody, &items); err != nil {
		return sdk.PaymentReconcileResult{}, errors.New("robokassa: invalid reconcile response")
	}
	settlements := make([]sdk.PaymentSettlement, 0, len(items))
	for _, item := range items {
		occurred, timeErr := time.Parse(time.RFC3339, item.StateDate)
		if timeErr != nil {
			occurred = time.Now().UTC()
		}
		minor, amountErr := strconv.ParseFloat(fmt.Sprintf("%.2f", item.OutSum), 64)
		if amountErr != nil {
			continue
		}
		settlements = append(settlements, sdk.PaymentSettlement{RemoteID: strconv.FormatInt(item.InvId, 10), Kind: "sale", Amount: sdk.PaymentAmount{MinorUnits: int64(minor*100 + 0.5), Currency: "RUB"}, OccurredAt: occurred.UTC()})
	}
	return sdk.PaymentReconcileResult{Items: settlements, ObservedAt: time.Now().UTC()}, nil
}

// VerifyWebhook: Robokassa's ResultURL delivery is itself cryptographically
// signed (unlike YooKassa/SBP), but this still re-fetches the authoritative
// state via OpStateExt rather than trusting the body's OutSum directly,
// matching this codebase's uniform "never trust the callback body alone"
// rule (ADR-0071/ADR-0105) — the signature only proves the delivery is
// genuinely from Robokassa, the re-fetch proves the state is current.
func (t robokassaHTTP) VerifyWebhook(ctx context.Context, secret, body, _ []byte) (deliveryID, eventType, remotePaymentID, ack string, err error) {
	values, parseErr := url.ParseQuery(string(body))
	if parseErr != nil {
		return "", "", "", "", errors.New("robokassa: invalid webhook body")
	}
	outSum, invID, signature := values.Get("OutSum"), values.Get("InvId"), values.Get("SignatureValue")
	if outSum == "" || invID == "" || signature == "" {
		return "", "", "", "", errors.New("robokassa: incomplete webhook body")
	}
	_, _, password2, credErr := splitRobokassaCredential(secret)
	if credErr != nil {
		return "", "", "", "", credErr
	}
	expected := robokassaMD5Hex(outSum + ":" + invID + ":" + string(password2))
	if !strings.EqualFold(expected, signature) {
		return "", "", "", "", errors.New("robokassa: signature mismatch")
	}
	state, err := t.fetchState(ctx, secret, invID)
	if err != nil {
		return "", "", "", "", err
	}
	return sha256Hex(body)[:32], "payment_" + robokassaStatusFromCode(state.State.Code), invID, "OK" + invID, nil
}

var _ robokassa.Transport = robokassaHTTP{}
