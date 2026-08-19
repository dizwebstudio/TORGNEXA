package notifications

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

type DestinationResolver interface {
	ResolveDestination(context.Context, tenancy.Scope, string, Channel) (string, error)
}

type EmailTransport interface {
	Send(context.Context, string, string, string) error
}
type ChatTransport interface {
	Send(context.Context, string, string, string) error
}

type EmailProvider struct {
	Destinations DestinationResolver
	Transport    EmailTransport
}

func (EmailProvider) Channel() Channel { return ChannelEmail }
func (p EmailProvider) Deliver(ctx context.Context, scope tenancy.Scope, n Notification) error {
	if p.Destinations == nil || p.Transport == nil {
		return ErrInvalid
	}
	destination, err := p.Destinations.ResolveDestination(ctx, scope, n.RecipientID, ChannelEmail)
	if err != nil {
		return err
	}
	if _, err = mail.ParseAddress(destination); err != nil {
		return ErrInvalid
	}
	return p.Transport.Send(ctx, destination, n.Title, n.Body)
}

type ChatProvider struct {
	Destinations DestinationResolver
	Transport    ChatTransport
}

func (ChatProvider) Channel() Channel { return ChannelChat }
func (p ChatProvider) Deliver(ctx context.Context, scope tenancy.Scope, n Notification) error {
	if p.Destinations == nil || p.Transport == nil {
		return ErrInvalid
	}
	destination, err := p.Destinations.ResolveDestination(ctx, scope, n.RecipientID, ChannelChat)
	if err != nil {
		return err
	}
	return p.Transport.Send(ctx, destination, n.Title, n.Body)
}

type SMTPConfig struct {
	Address, From, Username, Password, ServerName string
	Timeout                                       time.Duration
	ImplicitTLS                                   bool
}
type SMTPTransport struct{ Config SMTPConfig }

func (t SMTPTransport) Send(ctx context.Context, to, subject, body string) error {
	cfg := t.Config
	if ctx == nil || cfg.Address == "" || cfg.From == "" || cfg.Timeout <= 0 {
		return ErrInvalid
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return ErrInvalid
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return ErrInvalid
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil {
		return ErrInvalid
	}
	if cfg.ServerName == "" {
		cfg.ServerName = host
	}
	dialer := &net.Dialer{Timeout: cfg.Timeout}
	var conn net.Conn
	if cfg.ImplicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", cfg.Address, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.ServerName})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", cfg.Address)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if !cfg.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("notifications: smtp starttls required")
		}
		if err = client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: cfg.ServerName}); err != nil {
			return err
		}
	}
	if cfg.Username != "" {
		if cfg.Password == "" {
			return ErrInvalid
		}
		if err = client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, host)); err != nil {
			return err
		}
	}
	if err = client.Mail(cfg.From); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	message := "From: " + cfg.From + "\r\nTo: " + to + "\r\nSubject: " + sanitizeHeader(subject) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if _, err = writer.Write([]byte(message)); err != nil {
		_ = writer.Close()
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func sanitizeHeader(v string) string {
	return strings.ReplaceAll(strings.ReplaceAll(v, "\r", " "), "\n", " ")
}

// BotHTTPTransport is a bounded JSON chat transport. The endpoint is supplied
// by deployment configuration; Telegram Community deployments can use
// https://api.telegram.org/bot<TOKEN>/sendMessage without leaking the token to
// normal application tables.
type BotHTTPTransport struct {
	Endpoint string
	Client   *http.Client
}

func (t BotHTTPTransport) Send(ctx context.Context, destination, title, body string) error {
	if ctx == nil || t.Endpoint == "" || destination == "" {
		return ErrInvalid
	}
	parsed, err := url.Parse(t.Endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return ErrInvalid
	}
	if len(destination) > 128 {
		return ErrInvalid
	}
	if _, err = strconv.ParseInt(destination, 10, 64); err != nil {
		return ErrInvalid
	}
	payload, err := json.Marshal(map[string]string{"chat_id": destination, "text": strings.TrimSpace(title + "\n\n" + body)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.Endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notifications: chat transport status %d", resp.StatusCode)
	}
	// Consume a bounded status line only; remote bodies are never persisted/logged.
	_ = bufio.NewReader(resp.Body)
	return nil
}
