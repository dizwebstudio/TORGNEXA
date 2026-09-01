// Package repricing contains the provider-neutral, deterministic preview
// engine for safe price decisions. It never performs a remote write.
package repricing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

var (
	ErrInvalid = errors.New("repricing: invalid input")
)

type CandidateInput struct {
	ID            string `json:"id"`
	OfferID       string `json:"offer_id"`
	SKU           string `json:"sku"`
	Currency      string `json:"currency"`
	CurrentMinor  int64  `json:"current_minor"`
	ProposedMinor int64  `json:"proposed_minor"`
	FloorMinor    int64  `json:"floor_minor"`
	MaxChangeBPS  int64  `json:"max_change_bps"`
}

type Candidate struct {
	CandidateInput
	Status           string `json:"status"`
	DeltaMinor       int64  `json:"delta_minor"`
	DeltaBPS         int64  `json:"delta_bps"`
	Explanation      string `json:"explanation"`
	ApprovalRequired bool   `json:"approval_required"`
}

type Preview struct {
	ID             string      `json:"id"`
	InputDigest    string      `json:"input_digest"`
	Status         string      `json:"status"`
	GeneratedAt    time.Time   `json:"generated_at"`
	CandidateCount int         `json:"candidate_count"`
	BlockedCount   int         `json:"blocked_count"`
	Candidates     []Candidate `json:"candidates"`
}

// BuildPreview evaluates at most 1000 candidates without a remote side
// effect. Results are sorted by candidate ID and carry a stable input digest.
func BuildPreview(runID string, inputs []CandidateInput, now time.Time) (Preview, error) {
	if !validRef(runID) || len(inputs) == 0 || len(inputs) > 1000 || now.IsZero() || now.Location() != time.UTC {
		return Preview{}, ErrInvalid
	}
	ordered := append([]CandidateInput(nil), inputs...)
	seen := make(map[string]struct{}, len(ordered))
	for _, input := range ordered {
		if !validRef(input.ID) || !validRef(input.OfferID) || !validText(input.SKU, 200) || !domain.ValidCurrencyCode(input.Currency) || input.CurrentMinor < 0 || input.ProposedMinor < 0 || input.FloorMinor < 0 || input.MaxChangeBPS < 0 || input.MaxChangeBPS > 10000 {
			return Preview{}, ErrInvalid
		}
		if _, ok := seen[input.ID]; ok {
			return Preview{}, ErrInvalid
		}
		seen[input.ID] = struct{}{}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	digestBytes, err := json.Marshal(ordered)
	if err != nil {
		return Preview{}, ErrInvalid
	}
	sum := sha256.Sum256(digestBytes)
	preview := Preview{ID: runID, InputDigest: hex.EncodeToString(sum[:]), Status: "completed", GeneratedAt: now, CandidateCount: len(ordered), Candidates: make([]Candidate, 0, len(ordered))}
	for _, input := range ordered {
		candidate := Candidate{CandidateInput: input, Status: "approved", DeltaMinor: input.ProposedMinor - input.CurrentMinor, Explanation: "изменение проходит floor price и лимит шага"}
		candidate.DeltaBPS = deltaBPS(input.CurrentMinor, candidate.DeltaMinor)
		candidate.ApprovalRequired = candidate.DeltaMinor != 0
		if input.ProposedMinor < input.FloorMinor {
			candidate.Status = "blocked"
			candidate.Explanation = "цена ниже минимально разрешённой"
		} else if !withinStep(input.CurrentMinor, candidate.DeltaMinor, input.MaxChangeBPS) {
			candidate.Status = "blocked"
			candidate.Explanation = "изменение превышает лимит шага"
		}
		if candidate.Status == "blocked" {
			preview.BlockedCount++
		}
		preview.Candidates = append(preview.Candidates, candidate)
	}
	if preview.BlockedCount > 0 {
		preview.Status = "partial"
	}
	return preview, nil
}

func withinStep(current, delta, limitBPS int64) bool {
	if delta == 0 {
		return true
	}
	if current == 0 {
		return false
	}
	absolute := delta
	if absolute < 0 {
		absolute = -absolute
	}
	left := new(big.Int).Mul(big.NewInt(absolute), big.NewInt(10000))
	right := new(big.Int).Mul(big.NewInt(current), big.NewInt(limitBPS))
	return left.Cmp(right) <= 0
}

func deltaBPS(current, delta int64) int64 {
	if current == 0 || delta == 0 {
		return 0
	}
	value := new(big.Int).Mul(big.NewInt(delta), big.NewInt(10000))
	value.Quo(value, big.NewInt(current))
	if value.IsInt64() {
		return value.Int64()
	}
	if value.Sign() < 0 {
		return -9223372036854775807
	}
	return 9223372036854775807
}

func validRef(value string) bool {
	if len(value) == 0 || len(value) > 192 || value != strings.TrimSpace(value) {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (i > 0 && strings.ContainsRune("._:/-", r)) {
			continue
		}
		return false
	}
	return true
}
func validText(value string, max int) bool {
	return len(value) > 0 && len(value) <= max && value == strings.TrimSpace(value)
}
