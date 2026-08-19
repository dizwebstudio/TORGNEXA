package tenancy

import (
	"testing"
	"time"
)

func TestEntityValidation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	organizationID, _ := ParseOrganizationID(testOrganizationID)
	workspaceID, _ := ParseWorkspaceID(testWorkspaceID)
	storeID, _ := ParseStoreID(testStoreID)

	organization := Organization{ID: organizationID, Name: "Synthetic Organization", Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	workspace := Workspace{ID: workspaceID, OrganizationID: organizationID, Name: "Synthetic Workspace", Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	store := Store{ID: storeID, OrganizationID: organizationID, WorkspaceID: workspaceID, Code: "synthetic-store", Name: "Synthetic Store", Kind: StoreKindStore, Status: StatusActive, Version: 1, CreatedAt: now, UpdatedAt: now}
	if !organization.Valid() || !workspace.Valid() || !store.Valid() {
		t.Fatalf("valid entities were rejected: %#v %#v %#v", organization, workspace, store)
	}

	organization.Name = " Synthetic Organization"
	workspace.Status = "deleted"
	store.Code = "Synthetic Store"
	if organization.Valid() || workspace.Valid() || store.Valid() {
		t.Fatal("invalid entity invariant was accepted")
	}
}
