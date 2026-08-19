package main

import (
	"net"
	"os"
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
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}
