package connectorsandbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	sdk "github.com/torgnexa/torgnexa/internal/platform/connectors"
	"github.com/torgnexa/torgnexa/internal/platform/pluginsecurity"
)

type SecretSource interface {
	UseSecret(context.Context, sdk.SecretReference, string, func([]byte) error) error
}

type NetworkRequest struct {
	Method        string
	Destination   pluginsecurity.NetworkDestination
	RouteTemplate string
}

type NetworkTransport interface {
	Do(context.Context, []DialTarget, NetworkRequest) error
}

type Executor interface {
	Execute(context.Context, Operation, *Runtime) ([]Change, error)
}

// OperationGuard is a host-owned fail-closed policy boundary. Provider code cannot implement or bypass the guard used by Session.
type OperationGuard interface {
	Authorize(context.Context, Operation) error
}

type Runtime struct {
	mode              Mode
	plan              pluginsecurity.AdmissionPlan
	binding           CredentialBinding
	secrets           SecretSource
	egress            EgressGuard
	transport         NetworkTransport
	mu                sync.Mutex
	actions           []ExternalAction
	productionBlocked bool
}

func NewRuntime(mode Mode, plan pluginsecurity.AdmissionPlan, binding CredentialBinding, secrets SecretSource, egress EgressGuard, transport NetworkTransport) (*Runtime, error) {
	if !mode.Valid() || validatePlan(plan) != nil || binding.Validate() != nil {
		return nil, ErrInvalidOperation
	}
	if mode == ModeTest && binding.Tier == CredentialProduction {
		return nil, ErrProductionCredential
	}
	return &Runtime{mode: mode, plan: plan, binding: binding, secrets: secrets, egress: egress, transport: transport, productionBlocked: true}, nil
}

func (runtime *Runtime) UseSecret(ctx context.Context, class string, callback func([]byte) error) error {
	if callback == nil || !secretGranted(runtime.plan, class) {
		return ErrSecretDenied
	}
	if runtime.mode == ModeDryRun {
		// Synthetic non-secret material lets deterministic emulators construct a
		// plan without ever touching the production/sandbox secret provider.
		placeholder := []byte("dry-run-secret-placeholder")
		defer clear(placeholder)
		return callback(placeholder)
	}
	if runtime.binding.Tier != CredentialSandbox || runtime.binding.Reference == "" || runtime.secrets == nil {
		return ErrProductionCredential
	}
	return runtime.secrets.UseSecret(ctx, runtime.binding.Reference, class, callback)
}

func (runtime *Runtime) Network(ctx context.Context, request NetworkRequest) error {
	request.Method = strings.ToUpper(strings.TrimSpace(request.Method))
	if request.Method == "" || !safeRoute(request.RouteTemplate) || !networkGranted(runtime.plan, request.Destination) {
		return ErrEgressDenied
	}
	runtime.mu.Lock()
	runtime.actions = append(runtime.actions, ExternalAction{Method: request.Method, Destination: request.Destination, RouteTemplate: request.RouteTemplate})
	runtime.mu.Unlock()
	if runtime.mode == ModeDryRun {
		return nil
	}
	if runtime.transport == nil {
		return ErrEgressDenied
	}
	targets, err := runtime.egress.Plan(ctx, runtime.plan, request.Destination)
	if err != nil {
		return err
	}
	return runtime.transport.Do(ctx, targets, request)
}

func (runtime *Runtime) externalActions() []ExternalAction {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	return append([]ExternalAction(nil), runtime.actions...)
}

type Session struct {
	mode      Mode
	plan      pluginsecurity.AdmissionPlan
	binding   CredentialBinding
	secrets   SecretSource
	egress    EgressGuard
	transport NetworkTransport
	guard     OperationGuard
	slots     chan struct{}
}

func NewSession(mode Mode, plan pluginsecurity.AdmissionPlan, binding CredentialBinding, secrets SecretSource, egress EgressGuard, transport NetworkTransport) (*Session, error) {
	if validatePlan(plan) != nil || !mode.Valid() || binding.Validate() != nil {
		return nil, ErrInvalidOperation
	}
	if mode == ModeTest && binding.Tier == CredentialProduction {
		return nil, ErrProductionCredential
	}
	return &Session{mode: mode, plan: plan, binding: binding, secrets: secrets, egress: egress, transport: transport, slots: make(chan struct{}, plan.Limits.MaxConcurrentCalls)}, nil
}

// NewGuardedSession is required for product publication writes. The guard runs before provider execution and egress.
func NewGuardedSession(mode Mode, plan pluginsecurity.AdmissionPlan, binding CredentialBinding, secrets SecretSource, egress EgressGuard, transport NetworkTransport, guard OperationGuard) (*Session, error) {
	session, err := NewSession(mode, plan, binding, secrets, egress, transport)
	if err != nil {
		return nil, err
	}
	if guard == nil {
		return nil, ErrPolicyDenied
	}
	session.guard = guard
	return session, nil
}

func (session *Session) Run(ctx context.Context, operation Operation, executor Executor) (OperationResult, error) {
	start := time.Now()
	base := OperationResult{Version: ResultVersion, Mode: session.mode, RequestID: operation.RequestID, ExtensionID: operation.ExtensionID, ExtensionVersion: operation.ExtensionVersion, Capability: operation.Capability, CompletedAt: start.UTC(), Isolation: IsolationEvidence{ProductionCredentialsBlocked: true, EgressMediated: true}}
	if operation.Validate() != nil || executor == nil || operation.ExtensionID != session.plan.ExtensionID || operation.ExtensionVersion != session.plan.ExtensionVersion {
		base.Status = StatusRejected
		base.ReasonCode = "invalid_operation"
		return base, ErrInvalidOperation
	}
	if !capabilityGranted(session.plan, operation.Capability) {
		base.Status = StatusRejected
		base.ReasonCode = "capability_denied"
		return base, ErrCapabilityDenied
	}
	if operation.Capability == sdk.Capability("products.write") {
		if session.guard == nil || session.guard.Authorize(ctx, operation) != nil {
			base.Status = StatusRejected
			base.ReasonCode = "product_compliance_denied"
			return base, ErrPolicyDenied
		}
	}
	select {
	case session.slots <- struct{}{}:
		defer func() { <-session.slots }()
	case <-ctx.Done():
		base.Status = StatusRejected
		base.ReasonCode = "cancelled"
		return base, ctx.Err()
	}
	runctx, cancel := context.WithTimeout(ctx, time.Duration(session.plan.Limits.WallTimeMS)*time.Millisecond)
	defer cancel()
	runtime, err := NewRuntime(session.mode, session.plan, session.binding, session.secrets, session.egress, session.transport)
	if err != nil {
		base.Status = StatusRejected
		base.ReasonCode = "runtime_rejected"
		return base, err
	}
	changes, err := executor.Execute(runctx, operation, runtime)
	base.Changes = changes
	base.ExternalActions = runtime.externalActions()
	base.CompletedAt = time.Now().UTC()
	base.Usage.WallTimeMS = time.Since(start).Milliseconds()
	encoded, _ := jsonSize(base)
	base.Usage.OutputBytes = int64(encoded)
	if encoded > session.plan.Limits.MaxOutputBytes {
		base.Status = StatusLimitExceeded
		base.ReasonCode = "output_limit"
		return base, ErrResourceLimit
	}
	if err != nil {
		base.Status = StatusRejected
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			base.ReasonCode = "wall_time_limit"
		case errors.Is(err, ErrProductionCredential):
			base.ReasonCode = "production_credential_blocked"
		case errors.Is(err, ErrEgressDenied):
			base.ReasonCode = "egress_denied"
		default:
			base.ReasonCode = "connector_rejected"
		}
		return base, err
	}
	if session.mode == ModeDryRun {
		base.Status = StatusPlanned
	} else {
		base.Status = StatusSucceeded
	}
	return base, nil
}

func jsonSize(value any) (int, error) { data, err := json.Marshal(value); return len(data), err }
