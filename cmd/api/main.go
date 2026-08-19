package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}
