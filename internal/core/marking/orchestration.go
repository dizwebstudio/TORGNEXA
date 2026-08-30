package marking

import (
	"errors"
	"strconv"
	"time"
)

// Stage is the ordered full-lifecycle process. It is deliberately independent
// of a provider and can be resumed from PostgreSQL after a worker lease loss.
type Stage string

const (
	StageCodesRequest Stage = "codes_request"
	StageReserve      Stage = "reserve"
	StagePrint        Stage = "print"
	StageScan         Stage = "scan"
	StageAggregate    Stage = "aggregate"
	StageUPD          Stage = "upd"
	StageSign         Stage = "sign"
	StageEDO          Stage = "edo"
	StageCirculation  Stage = "circulation"
	StageReconcile    Stage = "reconciliation"
	StageComplete     Stage = "complete"
)

var ErrInvalidStage = errors.New("marking: invalid process stage")

func (s Stage) Valid() bool {
	switch s {
	case StageCodesRequest, StageReserve, StagePrint, StageScan, StageAggregate, StageUPD, StageSign, StageEDO, StageCirculation, StageReconcile, StageComplete:
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "unknown"
}

func quantityString(value int64) string {
	if value < 0 {
		return "unknown"
	}
	return strconv.FormatInt(value, 10)
}

func nowUTC() time.Time { return time.Now().UTC() }

// NextStage advances only after the current operation succeeded. Unknown or
// failed operations pause the process and require reconciliation/manual action.
func NextStage(current Stage, operationState OperationState) (Stage, error) {
	if !current.Valid() {
		return "", ErrInvalidStage
	}
	if operationState != OperationSucceeded {
		return current, nil
	}
	next := map[Stage]Stage{
		StageCodesRequest: StageReserve,
		StageReserve:      StagePrint,
		StagePrint:        StageScan,
		StageScan:         StageAggregate,
		StageAggregate:    StageUPD,
		StageUPD:          StageSign,
		StageSign:         StageEDO,
		StageEDO:          StageCirculation,
		StageCirculation:  StageReconcile,
		StageReconcile:    StageComplete,
	}
	value, ok := next[current]
	if !ok {
		return current, nil
	}
	return value, nil
}

// ReconciliationInput describes two safe bounded views of one entity.
type ReconciliationInput struct {
	EntityType       string
	EntityRef        string
	ExpectedStatus   string
	RemoteStatus     string
	ExpectedQuantity int64
	RemoteQuantity   int64
	ExpectedDigest   string
	RemoteDigest     string
	UnknownWrite     bool
}

// Reconcile classifies drift without modifying the authoritative local
// aggregate. The caller appends the resulting Drift and a remote observation.
func Reconcile(input ReconciliationInput, id string) (Drift, bool) {
	kind := DriftType("")
	switch {
	case input.UnknownWrite:
		kind = DriftUnknownWrite
	case input.ExpectedStatus != "" && input.ExpectedStatus != input.RemoteStatus:
		kind = DriftStatus
	case input.ExpectedQuantity >= 0 && input.RemoteQuantity >= 0 && input.ExpectedQuantity != input.RemoteQuantity:
		kind = DriftQuantity
	case input.ExpectedDigest != "" && input.RemoteDigest != "" && input.ExpectedDigest != input.RemoteDigest:
		kind = DriftComposition
	default:
		return Drift{}, false
	}
	return Drift{ID: id, EntityType: input.EntityType, EntityRef: input.EntityRef, Kind: kind, Expected: firstNonEmpty(input.ExpectedStatus, input.ExpectedDigest, quantityString(input.ExpectedQuantity)), Observed: firstNonEmpty(input.RemoteStatus, input.RemoteDigest, quantityString(input.RemoteQuantity)), ObservedAt: nowUTC()}, true
}
