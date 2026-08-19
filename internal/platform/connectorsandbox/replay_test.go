package connectorsandbox_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	sandbox "github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
	"github.com/torgnexa/torgnexa/internal/platform/connectorsandbox/emulator"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

type replayFixture struct {
	Operation sandbox.Operation `json:"operation"`
	Emulator  struct {
		SecretClass   string                            `json:"secret_class"`
		Destination   pluginsecurity.NetworkDestination `json:"destination"`
		RouteTemplate string                            `json:"route_template"`
	} `json:"emulator"`
	Expected struct {
		Status          sandbox.ResultStatus `json:"status"`
		Before          string               `json:"before_sha256"`
		After           string               `json:"after_sha256"`
		ExternalActions int                  `json:"external_action_count"`
	} `json:"expected"`
}

func TestSanitizedReplayFixtureIsDeterministic(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/sandbox/fixtures/synthetic-product-read-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture replayFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	plan := pluginsecurity.AdmissionPlan{BoundaryVersion: 1, ExecutionMode: pluginsecurity.ExecutionIsolatedProcessV1, ExtensionID: "synthetic-shop", ExtensionVersion: "1.2.3", ArtifactSHA256: digest, Trust: pluginsecurity.TrustVerified, Granted: pluginsecurity.PermissionGrant{ExtensionID: "synthetic-shop", ExtensionVersion: "1.2.3", ArtifactSHA256: digest, Capabilities: []sdk.Capability{"products.read"}, SecretClasses: []string{"marketplace.oauth"}, Network: []pluginsecurity.NetworkDestination{{Host: "api.synthetic.example", Port: 443}}, GrantedAt: time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)}, Limits: pluginsecurity.IsolationLimits{MemoryMiB: 128, CPUTimeMS: 5000, WallTimeMS: 10000, MaxOutputBytes: 1 << 20, MaxConcurrentCalls: 1}}
	session, err := sandbox.NewSession(sandbox.ModeDryRun, plan, sandbox.CredentialBinding{Tier: sandbox.CredentialProduction, Reference: "sec:v1:0123456789abcdef0123456789abcdef"}, nil, sandbox.EgressGuard{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := session.Run(context.Background(), fixture.Operation, emulator.Connector{SecretClass: fixture.Emulator.SecretClass, Destination: fixture.Emulator.Destination, RouteTemplate: fixture.Emulator.RouteTemplate})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != fixture.Expected.Status || len(result.Changes) != 1 || result.Changes[0].BeforeSHA256 != fixture.Expected.Before || result.Changes[0].AfterSHA256 != fixture.Expected.After || len(result.ExternalActions) != fixture.Expected.ExternalActions {
		t.Fatalf("replay drift: %#v", result)
	}
}
