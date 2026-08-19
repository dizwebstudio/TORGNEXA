package connectors

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidMapping  = errors.New("connectors: invalid entity mapping")
	ErrMappingNotFound = errors.New("connectors: entity mapping not found")
	ErrMappingConflict = errors.New("connectors: entity mapping conflict")
)

var mappingTypePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
var mappingIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

// EntityMapping is the only supported bridge between canonical local entity
// identity and a remote connector identity. Provider-specific IDs must never be
// added to Product/Offer or other Core structs.
type EntityMapping struct {
	OrganizationID     string
	WorkspaceID        string
	ConnectorAccountID string
	EntityType         string
	LocalEntityID      string
	RemoteID           string
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

func (mapping EntityMapping) Validate() error {
	organizationOK := mappingIDPattern.MatchString(mapping.OrganizationID)
	workspaceOK := mappingIDPattern.MatchString(mapping.WorkspaceID)
	accountOK := mappingIDPattern.MatchString(mapping.ConnectorAccountID)
	typeOK := mappingTypePattern.MatchString(mapping.EntityType)
	localOK := mappingIDPattern.MatchString(mapping.LocalEntityID)
	remoteOK := validRemoteID(mapping.RemoteID)
	timeOK := isUTC(mapping.CreatedAt) && isUTC(mapping.UpdatedAt) && !mapping.UpdatedAt.Before(mapping.CreatedAt)
	if !organizationOK || !workspaceOK || !accountOK || !typeOK || !localOK || !remoteOK || mapping.Version < 1 || !timeOK {
		return ErrInvalidMapping
	}
	return nil
}

type MappingUpsert struct {
	OrganizationID     string
	WorkspaceID        string
	ConnectorAccountID string
	EntityType         string
	LocalEntityID      string
	RemoteID           string
	ExpectedVersion    int64
}

func (command MappingUpsert) Validate() error {
	organizationOK := mappingIDPattern.MatchString(command.OrganizationID)
	workspaceOK := mappingIDPattern.MatchString(command.WorkspaceID)
	accountOK := mappingIDPattern.MatchString(command.ConnectorAccountID)
	typeOK := mappingTypePattern.MatchString(command.EntityType)
	localOK := mappingIDPattern.MatchString(command.LocalEntityID)
	remoteOK := validRemoteID(command.RemoteID)
	if !organizationOK || !workspaceOK || !accountOK || !typeOK || !localOK || !remoteOK || command.ExpectedVersion < 0 {
		return ErrInvalidMapping
	}
	return nil
}

// MappingRepository is provider-neutral. Connector runtime may resolve either
// direction without teaching Core anything about provider names or remote IDs.
type MappingRepository interface {
	UpsertMapping(context.Context, MappingUpsert) (EntityMapping, error)
	MappingByLocal(ctx context.Context, organizationID, workspaceID, connectorAccountID, entityType, localEntityID string) (EntityMapping, error)
	MappingByRemote(ctx context.Context, organizationID, workspaceID, connectorAccountID, entityType, remoteID string) (EntityMapping, error)
}

func validRemoteID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 512 {
		return false
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}
func isUTC(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC
}
