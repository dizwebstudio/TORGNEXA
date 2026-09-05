package main

import (
	"context"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/app/mcp"
	"github.com/torgnexa/torgnexa/internal/platform/bootstrap"
	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		address := os.Getenv("TORGNEXA_HEALTHCHECK_ADDR")
		if address == "" {
			address = "127.0.0.1:8090"
		}
		if err := healthcheck(address); err != nil {
			os.Exit(1)
		}
		return
	}
	if err := bootstrap.Run(config.ServiceMCP, mcp.Run); err != nil {
		os.Exit(1)
	}
}

func healthcheck(address string) error {
	validated, err := validateHealthcheckAddress(address)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// #nosec G704 -- validateHealthcheckAddress permits only loopback IP literals and a bounded TCP port.
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", validated)
	if err != nil {
		return err
	}
	return conn.Close()
}

func validateHealthcheckAddress(raw string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return "", net.InvalidAddrError("healthcheck address must target a loopback IP")
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return "", net.InvalidAddrError("healthcheck address has an invalid port")
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(value)), nil
}
