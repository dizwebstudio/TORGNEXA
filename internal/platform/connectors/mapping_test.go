package connectors

import (
	"testing"
	"time"
)

func TestEntityMappingValidation(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	mapping := EntityMapping{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorAccountID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0011", EntityType: "offer", LocalEntityID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0102", RemoteID: "remote-synthetic-42", Version: 1, CreatedAt: now, UpdatedAt: now}
	if err := mapping.Validate(); err != nil {
		t.Fatal(err)
	}
	mapping.RemoteID = " token\n"
	if err := mapping.Validate(); err == nil {
		t.Fatal("unsafe remote id accepted")
	}
}

func TestMappingUpsertUsesZeroExpectedVersionForCreate(t *testing.T) {
	command := MappingUpsert{OrganizationID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", WorkspaceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", ConnectorAccountID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0011", EntityType: "product", LocalEntityID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0101", RemoteID: "synthetic-1", ExpectedVersion: 0}
	if err := command.Validate(); err != nil {
		t.Fatal(err)
	}
	command.ExpectedVersion = -1
	if err := command.Validate(); err == nil {
		t.Fatal("negative version accepted")
	}
}
