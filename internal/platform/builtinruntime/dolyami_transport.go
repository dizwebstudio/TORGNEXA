package builtinruntime

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	dolyami "github.com/torgnexa/torgnexa/connectors/payments/dolyami"
)

type dolyamiHTTP struct{ base *httpTransport }

type dolyamiCredential struct {
	Login          string `json:"login"`
	Password       string `json:"password"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

func parseDolyamiCredential(secret []byte) (dolyamiCredential, tls.Certificate, error) {
	if len(secret) == 0 || len(secret) > 1<<20 {
		return dolyamiCredential{}, tls.Certificate{}, errors.New("dolyami: credential bundle invalid")
	}
	var value dolyamiCredential
	if err := json.Unmarshal(secret, &value); err != nil || strings.TrimSpace(value.Login) == "" || strings.TrimSpace(value.Password) == "" || strings.TrimSpace(value.CertificatePEM) == "" || strings.TrimSpace(value.PrivateKeyPEM) == "" {
		return dolyamiCredential{}, tls.Certificate{}, errors.New("dolyami: credential bundle requires login, password, certificate_pem and private_key_pem")
	}
	certificate, err := parseClientCertificate([]byte(value.CertificatePEM + "\n" + value.PrivateKeyPEM))
	if err != nil {
		return dolyamiCredential{}, tls.Certificate{}, errors.New("dolyami: invalid mTLS certificate bundle")
	}
	return value, certificate, nil
}

func (transport dolyamiHTTP) Ping(ctx context.Context, configuration dolyami.Configuration, secret []byte) error {
	credential, certificate, err := parseDolyamiCredential(secret)
	if err != nil {
		return err
	}
	if transport.base == nil {
		return errors.New("dolyami: transport unavailable")
	}
	parsed, err := url.Parse(configuration.ProbeURL)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return errors.New("dolyami: probe URL invalid")
	}
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	client := newCertHTTPTransport(transport.base.resolver, certificate)
	status, _, _, _, _, err := client.do(ctx, http.MethodGet, strings.ToLower(parsed.Hostname()), path, parsed.Query(), nil, http.Header{}, []byte(credential.Login), []byte(credential.Password))
	if err != nil {
		return err
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return errors.New("dolyami: probe rejected")
	}
	return nil
}

var _ dolyami.Transport = dolyamiHTTP{}
