package marketplaceoperations

import "errors"

// StageContract describes the canonical bounded-context reference required by
// one marketplace workflow stage. The contract is provider-neutral: provider
// names never appear in the core package.
type StageContract struct {
	Stage                  FlowStage `json:"stage"`
	Owner                  string    `json:"owner"`
	RequiredReferenceKinds []string  `json:"required_reference_kinds"`
}

var ErrMissingReference = errors.New("marketplace operations: required reference missing")

var stageContracts = []StageContract{
	{Stage: StageAccount, Owner: "integration_center", RequiredReferenceKinds: []string{"account"}},
	{Stage: StageProduct, Owner: "catalog", RequiredReferenceKinds: []string{"product"}},
	{Stage: StagePublication, Owner: "marketplace_publication", RequiredReferenceKinds: []string{"publication"}},
	{Stage: StagePricing, Owner: "pricing", RequiredReferenceKinds: []string{"price"}},
	{Stage: StageInventory, Owner: "inventory", RequiredReferenceKinds: []string{"inventory"}},
	{Stage: StageOrder, Owner: "orders", RequiredReferenceKinds: []string{"order"}},
	{Stage: StageReservation, Owner: "inventory", RequiredReferenceKinds: []string{"reservation"}},
	{Stage: StagePickPack, Owner: "wms", RequiredReferenceKinds: []string{"wms_task"}},
	{Stage: StageLabel, Owner: "logistics", RequiredReferenceKinds: []string{"label"}},
	{Stage: StageShipment, Owner: "logistics", RequiredReferenceKinds: []string{"shipment"}},
	{Stage: StageReturn, Owner: "returns", RequiredReferenceKinds: []string{"return"}},
	{Stage: StageSettlement, Owner: "settlements", RequiredReferenceKinds: []string{"settlement"}},
	{Stage: StageProfitability, Owner: "unit_economics", RequiredReferenceKinds: []string{"pnl"}},
	{Stage: StageReconciliation, Owner: "reconciliation", RequiredReferenceKinds: []string{"reconciliation"}},
}

// StageContracts returns a copy of the stable stage contract table.
func StageContracts() []StageContract {
	result := make([]StageContract, len(stageContracts))
	for i, contract := range stageContracts {
		result[i] = StageContract{Stage: contract.Stage, Owner: contract.Owner, RequiredReferenceKinds: append([]string(nil), contract.RequiredReferenceKinds...)}
	}
	return result
}

// ContractFor returns the canonical owner and reference requirements for a
// stage. The terminal stage has no command contract.
func ContractFor(stage FlowStage) (StageContract, bool) {
	for _, contract := range stageContracts {
		if contract.Stage == stage {
			return StageContract{Stage: contract.Stage, Owner: contract.Owner, RequiredReferenceKinds: append([]string(nil), contract.RequiredReferenceKinds...)}, true
		}
	}
	return StageContract{}, false
}

// ValidateCommandReferences ensures a successful stage command is linked to
// the canonical aggregate owned by that bounded context. Rejected and unknown
// outcomes may omit the reference because the operation itself is the evidence
// that must be reconciled.
func ValidateCommandReferences(command Command) error {
	if command.Outcome != OutcomeSucceeded {
		return nil
	}
	contract, ok := ContractFor(command.Stage)
	if !ok {
		return ErrInvalidTransition
	}
	present := make(map[string]struct{}, len(command.References))
	for _, reference := range command.References {
		present[reference.Kind] = struct{}{}
	}
	for _, required := range contract.RequiredReferenceKinds {
		if _, ok := present[required]; !ok {
			return ErrMissingReference
		}
	}
	return nil
}
