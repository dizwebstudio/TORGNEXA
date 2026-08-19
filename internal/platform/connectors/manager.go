package connectors

import (
	"context"
	"errors"
	"time"
)

var ErrManagerUnavailable = errors.New("connectors: manager is not initialized")

// Manager binds tenant accounts to registered connector manifests. It is a
// host-side SDK service; provider implementations do not receive repositories.
type Manager struct {
	registry   *Registry
	repository AccountRepository
	clock      func() time.Time
}

func NewManager(registry *Registry, repository AccountRepository) (*Manager, error) {
	if registry == nil || repository == nil {
		return nil, ErrManagerUnavailable
	}
	return &Manager{registry: registry, repository: repository, clock: func() time.Time { return time.Now().UTC() }}, nil
}

func (manager *Manager) CreateAccount(ctx context.Context, command AccountCreate) (Account, error) {
	if manager == nil || manager.registry == nil || manager.repository == nil {
		return Account{}, ErrManagerUnavailable
	}
	if ctx == nil {
		return Account{}, ErrManagerUnavailable
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	if err := command.Validate(); err != nil {
		return Account{}, err
	}
	_, manifest, err := manager.registry.Connector(command.ConnectorID)
	if err != nil {
		return Account{}, err
	}
	if manifest.RequiresSecret() && command.SecretReference == "" {
		return Account{}, ErrSecretReference
	}
	account, err := manager.repository.CreateAccount(ctx, command, manifest)
	if err != nil {
		return Account{}, err
	}
	if err := ValidateAccountAgainstManifest(account, manifest); err != nil {
		return Account{}, err
	}
	return account, nil
}

func (manager *Manager) Account(ctx context.Context, organizationID, workspaceID, accountID string) (Account, Manifest, error) {
	if manager == nil || manager.registry == nil || manager.repository == nil {
		return Account{}, Manifest{}, ErrManagerUnavailable
	}
	account, err := manager.repository.AccountByID(ctx, organizationID, workspaceID, accountID)
	if err != nil {
		return Account{}, Manifest{}, err
	}
	_, manifest, err := manager.registry.Connector(account.ConnectorID)
	if err != nil {
		return Account{}, Manifest{}, err
	}
	if err := ValidateAccountAgainstManifest(account, manifest); err != nil {
		return Account{}, Manifest{}, err
	}
	return account, manifest, nil
}

// CheckHealth runs the registered connector health probe and persists only a
// validated normalized Health value. Any unnormalized provider error is reduced
// to connector_health_failed; its Error() text is intentionally discarded.
func (manager *Manager) CheckHealth(ctx context.Context, organizationID, workspaceID, accountID string, runtime Runtime) (Account, error) {
	account, manifest, err := manager.Account(ctx, organizationID, workspaceID, accountID)
	if err != nil {
		return Account{}, err
	}
	connector, _, err := manager.registry.Connector(manifest.ID)
	if err != nil {
		return Account{}, err
	}
	health, probeErr := connector.Health(ctx, account, runtime)
	if probeErr != nil {
		reason := "connector_health_failed"
		var remote *RemoteError
		if errors.As(probeErr, &remote) && remote.Validate() == nil {
			reason = remote.Code
		}
		checkedAt := health.CheckedAt
		if checkedAt.IsZero() {
			checkedAt = manager.clock().UTC()
		}
		health = Health{Status: HealthUnavailable, ReasonCode: reason, CheckedAt: checkedAt}
	}
	if health.CheckedAt.IsZero() {
		return Account{}, ErrInvalidHealth
	}
	if err := health.Validate(); err != nil {
		return Account{}, err
	}
	return manager.repository.RecordAccountHealth(ctx, AccountHealthUpdate{
		OrganizationID:  organizationID,
		WorkspaceID:     workspaceID,
		AccountID:       accountID,
		Health:          health,
		ExpectedVersion: account.Version,
	})
}
