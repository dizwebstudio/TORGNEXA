// Package paymentreconciliation compares expected commerce facts, settlements and receipts without hidden FX.
package paymentreconciliation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/settlements"
	"sort"
	"time"
)

var (
	ErrInvalid       = errors.New("paymentreconciliation: invalid value")
	ErrFXUnavailable = errors.New("paymentreconciliation: FX conversion unavailable")
)

type Expected struct {
	ID, OrderID, ProviderRef string
	Amount                   domain.Money
	ExpectedAt               time.Time
}
type Receipt struct {
	ID, ExternalRef, OrderID string
	Amount                   domain.Money
	ReceivedAt               time.Time
	Disputed                 bool
}
type DifferenceKind string

const (
	DifferenceTiming    DifferenceKind = "timing"
	DifferenceKnownFee  DifferenceKind = "known_fee"
	DifferenceUnmatched DifferenceKind = "unmatched"
	DifferenceDuplicate DifferenceKind = "duplicate"
	DifferenceDisputed  DifferenceKind = "disputed"
)

type Difference struct {
	Kind                       DifferenceKind
	Reference, OrderID, Detail string
}
type Report struct {
	GeneratedAt                                         time.Time
	Differences                                         []Difference
	MatchedExpected, MatchedSettlement, MatchedReceipts int
	FXConversionRefs                                    []string
}
type Policy struct {
	TimingWindow  time.Duration
	KnownFeeCodes map[string]bool
}

// FXConverter is intentionally narrow. A successful cross-currency result must
// return the immutable persisted conversion-record reference used to derive it.
type FXConverter interface {
	Convert(context.Context, string, domain.Money, domain.Currency, time.Time) (domain.Money, string, error)
}

func Reconcile(scope tenancy.Scope, expected []Expected, entries []settlements.Entry, receipts []Receipt, p Policy, now time.Time) (Report, error) {
	return reconcile(context.Background(), scope, expected, entries, receipts, p, now, nil)
}

func ReconcileWithFX(ctx context.Context, scope tenancy.Scope, expected []Expected, entries []settlements.Entry, receipts []Receipt, p Policy, now time.Time, converter FXConverter) (Report, error) {
	if ctx == nil || converter == nil {
		return Report{}, ErrInvalid
	}
	return reconcile(ctx, scope, expected, entries, receipts, p, now, converter)
}

func reconcile(ctx context.Context, scope tenancy.Scope, expected []Expected, entries []settlements.Entry, receipts []Receipt, p Policy, now time.Time, converter FXConverter) (Report, error) {
	if !scope.Valid() || p.TimingWindow < 0 || now.IsZero() {
		return Report{}, ErrInvalid
	}
	r := Report{GeneratedAt: now.UTC()}
	expByOrder := map[string]Expected{}
	for _, x := range expected {
		if x.ID == "" || x.OrderID == "" || x.Amount.Validate() != nil || x.ExpectedAt.IsZero() {
			return Report{}, ErrInvalid
		}
		expByOrder[x.OrderID] = x
	}
	seenEntry := map[string]bool{}
	for _, e := range entries {
		if e.Validate() != nil {
			return Report{}, ErrInvalid
		}
		key := e.SourceSystem + "/" + e.SourceAccountID + "/" + e.SourceEntryRef
		if seenEntry[key] {
			r.Differences = append(r.Differences, Difference{DifferenceDuplicate, e.SourceEntryRef, e.OrderID, "duplicate settlement provider reference"})
			continue
		}
		seenEntry[key] = true
		if e.Disputed {
			r.Differences = append(r.Differences, Difference{DifferenceDisputed, e.SourceEntryRef, e.OrderID, "settlement marked disputed"})
			continue
		}
		if e.Kind == settlements.KindFee && p.KnownFeeCodes[e.FeeCode] {
			r.Differences = append(r.Differences, Difference{DifferenceKnownFee, e.SourceEntryRef, e.OrderID, e.FeeCode})
			continue
		}
		x, ok := expByOrder[e.OrderID]
		if !ok {
			r.Differences = append(r.Differences, Difference{DifferenceUnmatched, e.SourceEntryRef, e.OrderID, "no expected commerce fact"})
			continue
		}
		settlementAmount := e.Amount
		if x.Amount.Currency() != settlementAmount.Currency() {
			if converter == nil {
				r.Differences = append(r.Differences, Difference{DifferenceDisputed, e.SourceEntryRef, e.OrderID, "currency mismatch; FX conversion unavailable"})
				continue
			}
			conversionID := reconciliationConversionID(x, e)
			converted, ref, convErr := converter.Convert(ctx, conversionID, settlementAmount, x.Amount.Currency(), e.OccurredAt.UTC())
			if convErr != nil {
				return Report{}, fmt.Errorf("%w for settlement %s: %v", ErrFXUnavailable, e.SourceEntryRef, convErr)
			}
			if converted.Validate() != nil || converted.Currency() != x.Amount.Currency() || ref == "" {
				return Report{}, fmt.Errorf("%w for settlement %s: invalid conversion result", ErrFXUnavailable, e.SourceEntryRef)
			}
			settlementAmount = converted
			r.FXConversionRefs = append(r.FXConversionRefs, ref)
		}
		delta := e.OccurredAt.Sub(x.ExpectedAt)
		if delta < 0 {
			delta = -delta
		}
		if delta > p.TimingWindow {
			r.Differences = append(r.Differences, Difference{DifferenceTiming, e.SourceEntryRef, e.OrderID, "outside timing window"})
			continue
		}
		if x.Amount.MinorUnits() != settlementAmount.MinorUnits() {
			r.Differences = append(r.Differences, Difference{DifferenceDisputed, e.SourceEntryRef, e.OrderID, "amount mismatch"})
			continue
		}
		r.MatchedExpected++
		r.MatchedSettlement++
	}
	seenReceipt := map[string]bool{}
	for _, x := range receipts {
		if x.ID == "" || x.ExternalRef == "" || x.Amount.Validate() != nil || x.ReceivedAt.IsZero() {
			return Report{}, ErrInvalid
		}
		if seenReceipt[x.ExternalRef] {
			r.Differences = append(r.Differences, Difference{DifferenceDuplicate, x.ExternalRef, x.OrderID, "duplicate receipt"})
			continue
		}
		seenReceipt[x.ExternalRef] = true
		if x.Disputed {
			r.Differences = append(r.Differences, Difference{DifferenceDisputed, x.ExternalRef, x.OrderID, "receipt disputed"})
			continue
		}
		if _, ok := expByOrder[x.OrderID]; !ok {
			r.Differences = append(r.Differences, Difference{DifferenceUnmatched, x.ExternalRef, x.OrderID, "receipt has no expected commerce fact"})
			continue
		}
		r.MatchedReceipts++
	}
	sort.Slice(r.Differences, func(i, j int) bool {
		if r.Differences[i].Kind == r.Differences[j].Kind {
			return r.Differences[i].Reference < r.Differences[j].Reference
		}
		return r.Differences[i].Kind < r.Differences[j].Kind
	})
	return r, nil
}

func reconciliationConversionID(expected Expected, entry settlements.Entry) string {
	raw := expected.ID + "\x00" + entry.ID + "\x00" + entry.SourceSystem + "\x00" + entry.SourceAccountID + "\x00" + entry.SourceEntryRef + "\x00" + expected.Amount.Currency().String() + "\x00" + entry.OccurredAt.UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(raw))
	return "payrec:" + hex.EncodeToString(sum[:16])
}
