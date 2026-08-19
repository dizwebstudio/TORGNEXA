package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/connectors/conformance"
)

func main() {
	var emulator string
	flag.StringVar(&emulator, "emulator", os.Getenv("TORGNEXA_EMULATOR_BINARY"), "path to the Task-029 sandbox emulator executable")
	flag.Parse()
	if emulator == "" {
		fmt.Fprintln(os.Stderr, "conformance reference requires -emulator or TORGNEXA_EMULATOR_BINARY")
		os.Exit(2)
	}
	primary := conformance.Tenant{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002"}
	foreign := conformance.Tenant{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0004"}
	candidate := conformance.NewReferenceCandidate(emulator)
	report := conformance.Run(context.Background(), candidate, primary, foreign, func() time.Time { return time.Now().UTC() })
	if err := conformance.WriteJSON(os.Stdout, report); err != nil {
		fmt.Fprintln(os.Stderr, "conformance report rejected")
		os.Exit(1)
	}
	if err := conformance.Require(report); err != nil {
		os.Exit(1)
	}
}
