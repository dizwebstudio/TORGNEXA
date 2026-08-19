package checker

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const eventTypePattern = `^[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.[a-z][a-z0-9]*(_[a-z0-9]+)*\.v[1-9][0-9]{0,2}$`

const maxEventVersion = 999

var (
	eventTypeRE     = regexp.MustCompile(eventTypePattern)
	eventFilenameRE = regexp.MustCompile(`-v([1-9][0-9]*)\.schema\.json$`)
)

type eventCatalog struct {
	Version int          `json:"version"`
	Events  []eventEntry `json:"events"`
}

type eventEntry struct {
	EventType     string `json:"event_type"`
	PayloadSchema string `json:"payload_schema"`
}

func checkEvents(ctx context.Context, files []contractFile, parsed map[string]any, problems *diagnostics) {
	const catalogPath = "events/event-catalog.json"
	catalogValue, ok := parsed[catalogPath]
	var catalog eventCatalog
	if !ok {
		problems.add(catalogPath, "event catalog is required")
	} else if err := decodeKnownFields(catalogValue, &catalog); err != nil {
		problems.add(catalogPath, "decode catalog: %v", err)
	}
	if catalog.Version != 1 {
		problems.add(catalogPath, "version must be 1")
	}

	eventSchemas := make(map[string]contractFile)
	for _, file := range files {
		if strings.HasPrefix(file.Rel, "events/") && file.Rel != "events/event-envelope.schema.json" {
			eventSchemas[file.Rel] = file
		}
	}
	seenTypes := make(map[string]string)
	seenPaths := make(map[string]string)
	versions := make(map[string]map[int]struct{})
	previousType := ""
	for _, entry := range catalog.Events {
		if !checkContext(ctx, problems) {
			return
		}
		if !eventTypeRE.MatchString(entry.EventType) {
			problems.add(catalogPath, "event type %q does not match canonical policy", entry.EventType)
		}
		if previousType != "" && entry.EventType <= previousType {
			problems.add(catalogPath, "events must be strictly sorted by event_type")
		}
		previousType = entry.EventType
		if previous, duplicate := seenTypes[entry.EventType]; duplicate {
			problems.add(catalogPath, "duplicate event type %q (also %s)", entry.EventType, previous)
		}
		seenTypes[entry.EventType] = entry.PayloadSchema
		cleaned, err := safeContractPath(entry.PayloadSchema)
		if err != nil {
			problems.add(catalogPath, "payload schema %q: %v", entry.PayloadSchema, err)
			continue
		}
		if previous, duplicate := seenPaths[cleaned]; duplicate {
			problems.add(catalogPath, "payload schema %q is reused by %s", cleaned, previous)
		}
		seenPaths[cleaned] = entry.EventType
		file, exists := eventSchemas[cleaned]
		if !exists {
			problems.add(catalogPath, "payload schema %q is not an event schema", cleaned)
			continue
		}
		document, _ := parsed[file.Rel].(map[string]any)
		title, _ := document["title"].(string)
		if title != entry.EventType {
			problems.add(cleaned, "title must equal event type %q", entry.EventType)
		}
		match := eventFilenameRE.FindStringSubmatch(cleaned)
		if match == nil {
			problems.add(cleaned, "filename must end in -vN.schema.json")
			continue
		}
		fileVersion, fileVersionErr := parseEventVersion(match[1])
		if fileVersionErr != nil {
			problems.add(cleaned, "invalid filename version: %v", fileVersionErr)
		}
		typeVersion, family, err := splitEventVersion(entry.EventType)
		if err != nil {
			problems.add(catalogPath, "event type %q has invalid version: %v", entry.EventType, err)
			continue
		}
		if fileVersionErr == nil && fileVersion != typeVersion {
			problems.add(cleaned, "filename version v%d does not match event type v%d", fileVersion, typeVersion)
		}
		if versions[family] == nil {
			versions[family] = make(map[int]struct{})
		}
		versions[family][typeVersion] = struct{}{}
	}
	for relative := range eventSchemas {
		if _, registered := seenPaths[relative]; !registered {
			problems.add(relative, "event schema is not registered in event catalog")
		}
	}
	for family, set := range versions {
		var values []int
		for version := range set {
			values = append(values, version)
		}
		sort.Ints(values)
		for i, version := range values {
			if version != i+1 {
				problems.add(catalogPath, "event family %q has a version gap before v%d", family, version)
				break
			}
		}
	}
	checkEventEnvelope(parsed["events/event-envelope.schema.json"], problems)
}

func splitEventVersion(eventType string) (int, string, error) {
	index := strings.LastIndex(eventType, ".v")
	if index < 0 {
		return 0, "", fmt.Errorf("missing event version")
	}
	version, err := parseEventVersion(eventType[index+2:])
	return version, eventType[:index], err
}

func parseEventVersion(raw string) (int, error) {
	version, err := strconv.ParseUint(raw, 10, 16)
	if err != nil || version == 0 {
		return 0, fmt.Errorf("must be an integer between 1 and %d", maxEventVersion)
	}
	if version > maxEventVersion {
		return 0, fmt.Errorf("must not exceed %d", maxEventVersion)
	}
	return int(version), nil
}

func checkEventEnvelope(value any, problems *diagnostics) {
	const relative = "events/event-envelope.schema.json"
	document, ok := value.(map[string]any)
	if !ok {
		problems.add(relative, "event envelope schema is required")
		return
	}
	properties, _ := document["properties"].(map[string]any)
	eventType, _ := properties["event_type"].(map[string]any)
	pattern, _ := eventType["pattern"].(string)
	if pattern != eventTypePattern {
		problems.add(relative, "event_type pattern must equal canonical policy")
	}
	requiredValues, _ := document["required"].([]any)
	required := make(map[string]bool, len(requiredValues))
	for _, value := range requiredValues {
		if name, ok := value.(string); ok {
			required[name] = true
		}
	}
	for _, name := range []string{"event_id", "event_type", "occurred_at", "organization_id", "workspace_id", "correlation_id", "causation_id", "entity_type", "entity_id", "source", "data"} {
		if !required[name] {
			problems.add(relative, "canonical field %q must be required", name)
		}
	}
}
