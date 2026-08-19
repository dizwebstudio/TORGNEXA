package connectorsandbox

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
)

func TestLinuxSandboxExternalIsolationProbe(t *testing.T) {
	executable := os.Getenv("TORGNEXA_EMULATOR_BINARY")
	if executable == "" {
		t.Skip("set TORGNEXA_EMULATOR_BINARY to run Linux namespace isolation probe")
	}
	if runtime.GOOS != "linux" {
		t.Skip("linux sandbox only")
	}
	// This exists in the host environment and must never reach the child.
	t.Setenv("TORGNEXA_PRODUCTION_SECRET", "must-not-cross-sandbox")
	sandbox, err := NewLinuxSandbox(testPlan())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := sandbox.Probe(ctx, executable)
	if errors.Is(err, ErrSandboxUnavailable) {
		t.Skipf("kernel user namespaces unavailable: %v", err)
	}
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if result.Report.EnvironmentVisible || result.Report.FilesystemVisible || result.Report.DirectNetworkReachable {
		t.Fatalf("sandbox leak: %#v", result)
	}
	if !result.Isolation.EnvironmentIsolated || !result.Isolation.FilesystemIsolated || !result.Isolation.DirectNetworkBlocked || !result.Isolation.ResourceLimitsEnforced {
		t.Fatalf("missing evidence: %#v", result.Isolation)
	}
}
