package builtinruntime

// SupportStage describes whether a connector is executable in the current
// binary and whether its configuration belongs to this catalog surface.
type SupportStage string

const (
	SupportReady           SupportStage = "ready"
	SupportSeparateSurface SupportStage = "separate_surface"
	SupportPlanned         SupportStage = "planned"
)

// SyncSupport is one entity/direction pair implemented by the production
// worker composition. Manifest capabilities alone do not imply this support.
type SyncSupport struct {
	EntityType string
	Directions []string
}

// Support is the safe projection of executable built-in runtime authority.
// RuntimeConfigTemplate is frontend-only and deliberately omitted here.
type Support struct {
	ConnectorID             string
	Stage                   SupportStage
	Surface                 string
	OperationalCapabilities []string
	Sync                    []SyncSupport
	RuntimeConfigRequired   bool
	SocialTextMaxRunes      int
}

// SocialTextLimit reports the exact text ceiling admitted by the current
// provider composition. Zero means text publication is not executable.
func SocialTextLimit(connectorID string) int {
	value, ok := SupportFor(connectorID)
	if !ok || !SupportsCapability(connectorID, "social.post.text") {
		return 0
	}
	return value.SocialTextMaxRunes
}

// SupportFor returns a defensive copy of the generated support declaration.
func SupportFor(connectorID string) (Support, bool) {
	value, ok := generatedRuntimeSupport[connectorID]
	if !ok {
		return Support{}, false
	}
	value.OperationalCapabilities = append([]string(nil), value.OperationalCapabilities...)
	value.Sync = append([]SyncSupport(nil), value.Sync...)
	for index := range value.Sync {
		value.Sync[index].Directions = append([]string(nil), value.Sync[index].Directions...)
	}
	return value, true
}

// SupportsAccountConfiguration reports whether Settings -> Integrations may
// create and operate a generic connector account for this binary. Payment
// rails (surface "finance", the same separate_surface shape social channels
// use) need real per-tenant credentials — unlike cbr-fx, which is the only
// other "finance" connector and bypasses accounts entirely with one
// synthetic global reference source — so they reuse this generic
// create/secret/capability flow exactly as telegram and max-messenger do.
func SupportsAccountConfiguration(connectorID string) bool {
	value, ok := SupportFor(connectorID)
	return ok && ((value.Stage == SupportReady && value.Surface == "integrations") ||
		(value.Stage == SupportSeparateSurface && value.Surface == "social") ||
		(value.Stage == SupportSeparateSurface && value.Surface == "finance" && connectorID != "cbr-fx"))
}

// SupportsCapability reports whether a manifest capability has an executable
// route in the generic connector-account runtime.
func SupportsCapability(connectorID, capability string) bool {
	value, ok := SupportFor(connectorID)
	if !ok || !SupportsAccountConfiguration(connectorID) {
		return false
	}
	for _, candidate := range value.OperationalCapabilities {
		if candidate == capability {
			return true
		}
	}
	return false
}

// SupportsSync reports whether the worker can execute the requested exact
// entity and direction. Bidirectional requires both directions.
func SupportsSync(connectorID, entityType, direction string) bool {
	value, ok := SupportFor(connectorID)
	if !ok || value.Stage != SupportReady || value.Surface != "integrations" {
		return false
	}
	wantsInbound := direction == "inbound" || direction == "bidirectional"
	wantsOutbound := direction == "outbound" || direction == "bidirectional"
	if !wantsInbound && !wantsOutbound {
		return false
	}
	for _, candidate := range value.Sync {
		if candidate.EntityType != entityType {
			continue
		}
		hasInbound, hasOutbound := false, false
		for _, supported := range candidate.Directions {
			hasInbound = hasInbound || supported == "inbound"
			hasOutbound = hasOutbound || supported == "outbound"
		}
		return (!wantsInbound || hasInbound) && (!wantsOutbound || hasOutbound)
	}
	return false
}
