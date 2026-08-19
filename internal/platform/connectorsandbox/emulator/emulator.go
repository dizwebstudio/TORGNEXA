// Package emulator provides the deterministic, provider-neutral test connector
// used by Task 029 sandbox/dry-run qualification.
package emulator

import (
	"context"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/platform/connectorsandbox"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

type Connector struct {
	SecretClass   string
	Destination   pluginsecurity.NetworkDestination
	RouteTemplate string
	ChangeKind    connectorsandbox.ChangeKind
}

func (emulator Connector) Execute(ctx context.Context, operation connectorsandbox.Operation, runtime *connectorsandbox.Runtime) ([]connectorsandbox.Change, error) {
	if emulator.SecretClass != "" {
		if err := runtime.UseSecret(ctx, emulator.SecretClass, func(secret []byte) error {
			if len(secret) == 0 {
				return fmt.Errorf("empty synthetic secret")
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if emulator.Destination.Host != "" {
		if err := runtime.Network(ctx, connectorsandbox.NetworkRequest{Method: "POST", Destination: emulator.Destination, RouteTemplate: emulator.RouteTemplate}); err != nil {
			return nil, err
		}
	}
	kind := emulator.ChangeKind
	if !kind.Valid() {
		kind = connectorsandbox.ChangeUpdate
	}
	return []connectorsandbox.Change{{ResourceType: operation.ResourceType, ResourceID: operation.ResourceID, Kind: kind, BeforeSHA256: connectorsandbox.DigestCanonical(map[string]any{"version": 1}), AfterSHA256: connectorsandbox.DigestCanonical(map[string]any{"version": 2})}}, nil
}
