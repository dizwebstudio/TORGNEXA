package main

import (
	"encoding/json"
	"net"
	"os"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
)

func main() {
	if len(os.Args) != 2 || os.Args[1] != "--isolation-probe" {
		os.Exit(64)
	}
	report := connectorsandbox.ProbeReport{
		EnvironmentVisible:     os.Getenv("TORGNEXA_PRODUCTION_SECRET") != "",
		FilesystemVisible:      fileExists("/run/secrets/torgnexa-production") || fileExists("/etc/passwd"),
		DirectNetworkReachable: canDial(),
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(report); err != nil {
		os.Exit(70)
	}
}
func fileExists(path string) bool { _, err := os.Stat(path); return err == nil }
func canDial() bool {
	connection, err := net.DialTimeout("tcp", "1.1.1.1:443", 150*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}
