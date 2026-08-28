package builtinruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	cdek "github.com/torgnexa/torgnexa/connectors/cdek"
	dellin "github.com/torgnexa/torgnexa/connectors/dellin"
	fivepost "github.com/torgnexa/torgnexa/connectors/fivepost"
	pek "github.com/torgnexa/torgnexa/connectors/pek"
	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
)

var errLogisticsOperationNotAdmitted = errors.New("logistics operation requires carrier qualification")

// fivepostHTTP is the reviewed host-side credential probe. The partner API
// publishes the token endpoint, while shipment payload mappings remain behind
// the SDK until a current partner contract is qualified. Keeping those calls
// fail-closed prevents a guessed write shape from creating a real shipment.
type fivepostHTTP struct{ h *httpTransport }

func (transport fivepostHTTP) Ping(ctx context.Context, secret []byte) error {
	if len(secret) == 0 || transport.h == nil {
		return errors.New("5post credential probe unavailable")
	}
	body, err := json.Marshal(struct {
		APIKey string `json:"apiKey"`
	}{APIKey: string(secret)})
	if err != nil {
		return err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}}
	status, _, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.5post.ru", "/api/v1/auth/token", url.Values{}, body, headers, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("5post credential probe rejected with status %d", status)
	}
	return nil
}

func (fivepostHTTP) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error) {
	return sdk.LabelResult{}, errLogisticsOperationNotAdmitted
}
func (fivepostHTTP) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return nil, errLogisticsOperationNotAdmitted
}

var _ fivepost.Transport = fivepostHTTP{}

// cdekHTTP performs the two-step CDEK OAuth client-credentials probe. The
// token is used only for the bounded city-directory request and is never
// persisted or returned to the application layer.
type cdekHTTP struct{ h *httpTransport }

func (transport cdekHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("СДЭК credential probe unavailable")
	}
	var credentials struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if json.Unmarshal(secret, &credentials) != nil || strings.TrimSpace(credentials.ClientID) == "" || credentials.ClientSecret == "" {
		return errors.New("СДЭК credentials must be JSON with client_id and client_secret")
	}
	form := url.Values{
		"grant_type":    []string{"client_credentials"},
		"client_id":     []string{credentials.ClientID},
		"client_secret": []string{credentials.ClientSecret},
	}
	headers := http.Header{"Content-Type": []string{"application/x-www-form-urlencoded"}, "Accept": []string{"application/json"}}
	status, body, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.cdek.ru", "/v2/oauth/token", url.Values{}, []byte(form.Encode()), headers, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("СДЭК token probe rejected with status %d", status)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(body, &token) != nil || strings.TrimSpace(token.AccessToken) == "" {
		return errors.New("СДЭК token probe returned no access token")
	}
	cityHeaders := http.Header{"Authorization": []string{"Bearer " + token.AccessToken}, "Accept": []string{"application/json"}}
	status, _, _, _, _, err = transport.h.do(ctx, http.MethodGet, "api.cdek.ru", "/v2/location/cities", url.Values{"size": []string{"1"}}, nil, cityHeaders, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("СДЭК city probe rejected with status %d", status)
	}
	return nil
}

func (cdekHTTP) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	return nil, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Cancel(context.Context, []byte, sdk.ShipmentCancelRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Return(context.Context, []byte, sdk.ReturnCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Label(context.Context, []byte, sdk.LabelRequest) (sdk.LabelResult, error) {
	return sdk.LabelResult{}, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return nil, errLogisticsOperationNotAdmitted
}
func (cdekHTTP) Webhook(context.Context, []byte, []byte, []byte) (sdk.LogisticsWebhook, error) {
	return sdk.LogisticsWebhook{}, errLogisticsOperationNotAdmitted
}

var _ cdek.Transport = cdekHTTP{}

// pekHTTP probes the official ПЭК personal-cabinet API with the documented
// Basic login/access-key pair. The operation methods stay fail-closed until
// request/response fixtures are qualified against the current API revision.
type pekHTTP struct{ h *httpTransport }

func (transport pekHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("ПЭК credential probe unavailable")
	}
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.Unmarshal(secret, &credentials) != nil || strings.TrimSpace(credentials.Username) == "" || credentials.Password == "" {
		return errors.New("ПЭК credentials must be JSON with username and password")
	}
	headers := http.Header{"Content-Type": []string{"application/json;charset=utf-8"}, "Accept": []string{"application/json"}}
	status, _, _, _, _, err := transport.h.do(ctx, http.MethodPost, "kabinet.pecom.ru", "/api/v1/branches/all/", url.Values{}, []byte(`{}`), headers, []byte(credentials.Username), []byte(credentials.Password))
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("ПЭК credential probe rejected with status %d", status)
	}
	return nil
}

func (pekHTTP) Rates(context.Context, []byte, sdk.RateRequest) ([]sdk.RateQuote, error) {
	return nil, errLogisticsOperationNotAdmitted
}
func (pekHTTP) Create(context.Context, []byte, sdk.ShipmentCreateRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (pekHTTP) Track(context.Context, []byte, sdk.ShipmentStatusRequest) (sdk.ShipmentResult, error) {
	return sdk.ShipmentResult{}, errLogisticsOperationNotAdmitted
}
func (pekHTTP) Pickup(context.Context, []byte, sdk.PickupPointQuery) ([]sdk.PickupPoint, error) {
	return nil, errLogisticsOperationNotAdmitted
}

var _ pek.Transport = pekHTTP{}

// dellinHTTP validates an app key and personal access token by opening a
// short-lived Деловые Линии API session. The returned session ID is discarded;
// future operational calls remain qualification-gated.
type dellinHTTP struct{ h *httpTransport }

func (transport dellinHTTP) Ping(ctx context.Context, secret []byte) error {
	if transport.h == nil {
		return errors.New("Деловые Линии credential probe unavailable")
	}
	var credentials struct {
		AppKey string `json:"appkey"`
		PAT    string `json:"pat"`
	}
	if json.Unmarshal(secret, &credentials) != nil || strings.TrimSpace(credentials.AppKey) == "" || strings.TrimSpace(credentials.PAT) == "" {
		return errors.New("Деловые Линии credentials must be JSON with appkey and pat")
	}
	body, err := json.Marshal(credentials)
	if err != nil {
		return err
	}
	headers := http.Header{"Content-Type": []string{"application/json"}, "Accept": []string{"application/json"}}
	status, response, _, _, _, err := transport.h.do(ctx, http.MethodPost, "api.dellin.ru", "/v4/auth/login.json", url.Values{}, body, headers, nil, nil)
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("Деловые Линии credential probe rejected with status %d", status)
	}
	var result struct {
		Metadata struct {
			Status int `json:"status"`
		} `json:"metadata"`
		Data struct {
			SessionID string `json:"sessionID"`
		} `json:"data"`
	}
	if json.Unmarshal(response, &result) != nil || (result.Metadata.Status != 0 && result.Metadata.Status != http.StatusOK) || strings.TrimSpace(result.Data.SessionID) == "" {
		return errors.New("Деловые Линии credential probe returned no session")
	}
	return nil
}

var _ dellin.Transport = dellinHTTP{}
