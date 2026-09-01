package repricing

import (
	"testing"
	"time"
)

func TestBuildPreviewIsStableAndBlocksFloorAndStep(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	inputs := []CandidateInput{
		{ID: "offer-2", OfferID: "offer-2", SKU: "SKU-2", Currency: "RUB", CurrentMinor: 10000, ProposedMinor: 10500, FloorMinor: 9000, MaxChangeBPS: 1000},
		{ID: "offer-1", OfferID: "offer-1", SKU: "SKU-1", Currency: "RUB", CurrentMinor: 10000, ProposedMinor: 8000, FloorMinor: 9000, MaxChangeBPS: 1000},
	}
	first, err := BuildPreview("run-1", inputs, now)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := BuildPreview("run-2", []CandidateInput{inputs[1], inputs[0]}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.InputDigest != reversed.InputDigest || first.Status != "partial" || first.BlockedCount != 1 || first.Candidates[0].Status != "blocked" || first.Candidates[1].Status != "approved" {
		t.Fatalf("unexpected preview: %#v", first)
	}
}

func TestBuildPreviewRejectsDuplicateAndTooLargeStep(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	input := CandidateInput{ID: "offer-1", OfferID: "offer-1", SKU: "SKU-1", Currency: "RUB", CurrentMinor: 1, ProposedMinor: 2, FloorMinor: 0, MaxChangeBPS: 10000}
	if _, err := BuildPreview("run-1", []CandidateInput{input, input}, now); err == nil {
		t.Fatal("expected duplicate candidate rejection")
	}
	input.MaxChangeBPS = 0
	preview, err := BuildPreview("run-1", []CandidateInput{input}, now)
	if err != nil || preview.Candidates[0].Status != "blocked" {
		t.Fatalf("expected step block, preview=%#v err=%v", preview, err)
	}
}
