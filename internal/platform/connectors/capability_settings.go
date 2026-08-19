package connectors

import (
	"errors"
	"sort"
)

var ErrInvalidCapabilitySettings = errors.New("connectors: invalid account capability settings")

// AccountCapabilitySetting is one host-owned permission in an immutable
// account configuration revision. Disabled entries are retained so a revision
// is a complete, auditable snapshot of the manifest capability surface.
type AccountCapabilitySetting struct {
	Capability       Capability          `json:"capability"`
	Direction        CapabilityDirection `json:"direction"`
	Risk             CapabilityRisk      `json:"risk"`
	ApprovalRequired bool                `json:"approval_required"`
	Enabled          bool                `json:"enabled"`
}

// BuildAccountCapabilitySettings validates a user selection against the
// connector manifest and returns a stable complete snapshot. Omitted
// capabilities are disabled (default deny).
func BuildAccountCapabilitySettings(manifest Manifest, enabled []Capability) ([]AccountCapabilitySetting, error) {
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	selected := make(map[Capability]struct{}, len(enabled))
	for _, capability := range enabled {
		if !manifest.Supports(capability) {
			return nil, ErrInvalidCapabilitySettings
		}
		if _, duplicate := selected[capability]; duplicate {
			return nil, ErrInvalidCapabilitySettings
		}
		selected[capability] = struct{}{}
	}
	capabilities := append([]Capability(nil), manifest.Capabilities...)
	sort.Slice(capabilities, func(i, j int) bool { return capabilities[i] < capabilities[j] })
	settings := make([]AccountCapabilitySetting, 0, len(capabilities))
	for _, capability := range capabilities {
		definition, ok := CapabilityDefinitionFor(capability)
		if !ok || !validCapabilityPolicy(definition) {
			return nil, ErrInvalidCapabilitySettings
		}
		_, isEnabled := selected[capability]
		settings = append(settings, AccountCapabilitySetting{
			Capability: capability, Direction: definition.Direction, Risk: definition.Risk,
			ApprovalRequired: definition.ApprovalRequired, Enabled: isEnabled,
		})
	}
	return settings, nil
}

func validCapabilityPolicy(definition CapabilityDefinition) bool {
	switch definition.Direction {
	case CapabilityRead:
		return definition.Risk == CapabilityRiskRead && !definition.ApprovalRequired
	case CapabilityWrite:
		return definition.Risk == CapabilityRiskWriteSensitive && definition.ApprovalRequired
	default:
		return false
	}
}

// CapabilityEnabled reports whether the complete current snapshot grants an
// operation. An absent or malformed snapshot always denies access.
func CapabilityEnabled(settings []AccountCapabilitySetting, capability Capability) bool {
	for _, setting := range settings {
		if setting.Capability == capability {
			definition, ok := CapabilityDefinitionFor(capability)
			return ok && setting.Enabled && setting.Direction == definition.Direction && setting.Risk == definition.Risk && setting.ApprovalRequired == definition.ApprovalRequired
		}
	}
	return false
}
