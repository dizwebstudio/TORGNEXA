package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/torgnexa/torgnexa/internal/app/api"
	"github.com/torgnexa/torgnexa/internal/platform/bootstrap"
	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		url := os.Getenv("TORGNEXA_HEALTHCHECK_URL")
		if url == "" {
			url = "http://127.0.0.1:8080/api/v1/health"
		}
		if err := healthcheck(url); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := bootstrap.Run(config.ServiceAPI, api.Run); err != nil {
		os.Exit(1)
	}
}

func healthcheck(url string) error {
	validated, err := validateHealthcheckURL(url)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// #nosec G704 -- validated is produced by validateHealthcheckURL, which accepts only loopback IP literals.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, validated, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// #nosec G704 -- validateHealthcheckURL permits only loopback IP literals and redirects are disabled.
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

func validateHealthcheckURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("healthcheck URL must be a loopback HTTP URL")
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("healthcheck URL must target a loopback IP")
	}
	if port := parsed.Port(); port != "" {
		value, parseErr := strconv.Atoi(port)
		if parseErr != nil || value < 1 || value > 65535 {
			return "", fmt.Errorf("healthcheck URL has an invalid port")
		}
	}
	return parsed.String(), nil
}
