package fxrepo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/fx"
)

var ErrInvalid = errors.New("fxrepo: invalid value")

type Repository struct{ db *sql.DB }

// ListQuery is the bounded public read filter for immutable FX facts. Base and
// quote must either both be empty or both be supplied.
type ListQuery struct {
	Base, Quote string
	Limit       int
}

func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, ErrInvalid
	}
	return &Repository{db: db}, nil
}

// ListFacts returns immutable facts newest-effective first. FX reference facts
// are global market data and therefore deliberately do not accept tenant input.
func (r *Repository) ListFacts(ctx context.Context, query ListQuery) ([]fx.RateFact, error) {
	if ctx == nil || r == nil || r.db == nil || query.Limit < 1 || query.Limit > 201 || (query.Base == "") != (query.Quote == "") {
		return nil, ErrInvalid
	}
	args := []any{query.Limit}
	statement := `SELECT base_currency,quote_currency,rate_type,id,rate_coefficient,rate_scale,source_id,source_reference,observed_at,effective_at FROM fx_rate_facts`
	if query.Base != "" {
		base, err := domain.NewCurrency(query.Base)
		if err != nil {
			return nil, ErrInvalid
		}
		quote, err := domain.NewCurrency(query.Quote)
		if err != nil || base == quote {
			return nil, ErrInvalid
		}
		args = []any{base.String(), quote.String(), query.Limit}
		statement += ` WHERE base_currency=$1 AND quote_currency=$2`
	}
	statement += ` ORDER BY effective_at DESC,observed_at DESC,id LIMIT $` + fmt.Sprint(len(args))
	rows, err := r.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("fxrepo list facts: %w", err)
	}
	defer rows.Close()
	items := make([]fx.RateFact, 0, query.Limit)
	for rows.Next() {
		var base, quote, rateType string
		var id, source, reference string
		var coefficient int64
		var scale int
		var observed, effective sql.NullTime
		if err := rows.Scan(&base, &quote, &rateType, &id, &coefficient, &scale, &source, &reference, &observed, &effective); err != nil {
			return nil, err
		}
		baseCurrency, err := domain.NewCurrency(base)
		if err != nil {
			return nil, ErrInvalid
		}
		quoteCurrency, err := domain.NewCurrency(quote)
		if err != nil {
			return nil, ErrInvalid
		}
		pair, err := fx.NewPair(baseCurrency, quoteCurrency)
		if err != nil {
			return nil, ErrInvalid
		}
		fact, err := scanFact(staticScanner{[]any{id, coefficient, scale, source, reference, observed, effective}}, pair, fx.RateType(rateType))
		if err != nil {
			return nil, err
		}
		items = append(items, fact)
	}
	return items, rows.Err()
}

func (r *Repository) AppendFact(ctx context.Context, fact fx.RateFact) error {
	if ctx == nil || fact.Validate() != nil {
		return ErrInvalid
	}
	res, err := r.db.ExecContext(ctx, `INSERT INTO fx_rate_facts(id,base_currency,quote_currency,rate_coefficient,rate_scale,source_id,source_reference,rate_type,observed_at,effective_at,schema_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT (id) DO NOTHING`, fact.ID(), fact.Pair().Base.String(), fact.Pair().Quote.String(), fact.Rate().Coefficient(), fact.Rate().Scale(), fact.Source().String(), fact.SourceReference(), string(fact.RateType()), fact.ObservedAt().Time(), fact.EffectiveAt().Time(), fx.SchemaVersion)
	if err != nil {
		return fmt.Errorf("fxrepo append fact: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	existing, err := r.FactByID(ctx, fact.ID())
	if err != nil {
		return err
	}
	a, _ := existing.MarshalJSON()
	b, _ := fact.MarshalJSON()
	if string(a) != string(b) {
		return fx.ErrStoreConflict
	}
	return nil
}

func (r *Repository) Candidates(ctx context.Context, req fx.LookupRequest, sources []fx.SourceID) ([]fx.RateFact, error) {
	if ctx == nil || req.Validate() != nil || len(sources) == 0 {
		return nil, ErrInvalid
	}
	args := []any{req.Pair.Base.String(), req.Pair.Quote.String(), string(req.RateType), req.AsOf.Time()}
	q := `SELECT id,rate_coefficient,rate_scale,source_id,source_reference,observed_at,effective_at FROM fx_rate_facts WHERE base_currency=$1 AND quote_currency=$2 AND rate_type=$3 AND effective_at<=$4 AND source_id IN (`
	for i, s := range sources {
		if s.Validate() != nil {
			return nil, ErrInvalid
		}
		if i > 0 {
			q += ","
		}
		q += fmt.Sprintf("$%d", len(args)+1)
		args = append(args, s.String())
	}
	q += `) ORDER BY effective_at DESC,observed_at DESC,id LIMIT 512`
	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []fx.RateFact{}
	for rows.Next() {
		f, err := scanFact(rows, req.Pair, req.RateType)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type scanner interface{ Scan(...any) error }

func scanFact(row scanner, pair fx.Pair, rt fx.RateType) (fx.RateFact, error) {
	var id, source, ref string
	var coeff int64
	var scale int
	var observed, effective sql.NullTime
	if err := row.Scan(&id, &coeff, &scale, &source, &ref, &observed, &effective); err != nil {
		return fx.RateFact{}, err
	}
	if !observed.Valid || !effective.Valid || scale < 0 || scale > int(domain.MaxDecimalScale) {
		return fx.RateFact{}, ErrInvalid
	}
	dec, err := domain.NewDecimal(coeff, uint8(scale))
	if err != nil {
		return fx.RateFact{}, err
	}
	sid, err := fx.NewSourceID(source)
	if err != nil {
		return fx.RateFact{}, err
	}
	oi, err := domain.NewUTCInstant(observed.Time.UTC())
	if err != nil {
		return fx.RateFact{}, err
	}
	ei, err := domain.NewUTCInstant(effective.Time.UTC())
	if err != nil {
		return fx.RateFact{}, err
	}
	return fx.NewRateFact(fx.RateFactInput{ID: id, Pair: pair, Rate: dec, Source: sid, SourceReference: ref, RateType: rt, ObservedAt: oi, EffectiveAt: ei})
}

func (r *Repository) FactByID(ctx context.Context, id string) (fx.RateFact, error) {
	if ctx == nil || id == "" {
		return fx.RateFact{}, ErrInvalid
	}
	var base, quote, rt string
	row := r.db.QueryRowContext(ctx, `SELECT base_currency,quote_currency,rate_type,id,rate_coefficient,rate_scale,source_id,source_reference,observed_at,effective_at FROM fx_rate_facts WHERE id=$1`, id)
	var rid, source, ref string
	var coeff int64
	var scale int
	var observed, effective sql.NullTime
	if err := row.Scan(&base, &quote, &rt, &rid, &coeff, &scale, &source, &ref, &observed, &effective); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fx.RateFact{}, fx.ErrRateMissing
		}
		return fx.RateFact{}, err
	}
	bc, err := domain.NewCurrency(base)
	if err != nil {
		return fx.RateFact{}, err
	}
	qc, err := domain.NewCurrency(quote)
	if err != nil {
		return fx.RateFact{}, err
	}
	pair, err := fx.NewPair(bc, qc)
	if err != nil {
		return fx.RateFact{}, err
	}
	return scanFact(staticScanner{[]any{rid, coeff, scale, source, ref, observed, effective}}, pair, fx.RateType(rt))
}

type staticScanner struct{ values []any }

func (s staticScanner) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return ErrInvalid
	}
	for i, v := range s.values {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *int64:
			*d = v.(int64)
		case *int:
			*d = v.(int)
		case *sql.NullTime:
			*d = v.(sql.NullTime)
		default:
			return ErrInvalid
		}
	}
	return nil
}

func (r *Repository) AppendResolution(ctx context.Context, e fx.ResolutionEvidence) error {
	if ctx == nil || e.Validate() != nil {
		return ErrInvalid
	}
	p, _ := json.Marshal(e.Precedence)
	c, _ := json.Marshal(e.CandidateFactIDs)
	res, err := r.db.ExecContext(ctx, `INSERT INTO fx_resolution_evidence(id,base_currency,quote_currency,rate_type,as_of,precedence,candidate_fact_ids,selected_fact_id,resolved_at) VALUES($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8,$9) ON CONFLICT (id) DO NOTHING`, e.ID, e.Pair.Base.String(), e.Pair.Quote.String(), string(e.RateType), e.AsOf.Time(), string(p), string(c), e.SelectedFactID, e.ResolvedAt.Time())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	var base, quote, rateType, selected string
	var asOf, resolved sql.NullTime
	var existingPrecedence, existingCandidates []byte
	if err := r.db.QueryRowContext(ctx, `SELECT base_currency,quote_currency,rate_type,as_of,precedence,candidate_fact_ids,selected_fact_id,resolved_at FROM fx_resolution_evidence WHERE id=$1`, e.ID).Scan(&base, &quote, &rateType, &asOf, &existingPrecedence, &existingCandidates, &selected, &resolved); err != nil {
		return err
	}
	var ep []fx.SourceID
	var ec []string
	if json.Unmarshal(existingPrecedence, &ep) != nil || json.Unmarshal(existingCandidates, &ec) != nil || !asOf.Valid || !resolved.Valid {
		return ErrInvalid
	}
	if base != e.Pair.Base.String() || quote != e.Pair.Quote.String() || rateType != string(e.RateType) || !asOf.Time.UTC().Equal(e.AsOf.Time()) || !resolved.Time.UTC().Equal(e.ResolvedAt.Time()) || selected != e.SelectedFactID || fmt.Sprint(ep) != fmt.Sprint(e.Precedence) || fmt.Sprint(ec) != fmt.Sprint(e.CandidateFactIDs) {
		return fx.ErrStoreConflict
	}
	return nil
}
func (r *Repository) AppendConversion(ctx context.Context, c fx.ConversionRecord) error {
	if ctx == nil || c.Validate() != nil {
		return ErrInvalid
	}
	snap, err := json.Marshal(c.Snapshot)
	if err != nil {
		return err
	}
	ev, _ := json.Marshal(c.ResolutionEvidenceIDs)
	res, err := r.db.ExecContext(ctx, `INSERT INTO fx_conversion_records(id,source_currency,source_minor_units,source_minor_unit_scale,target_currency,target_minor_units,target_minor_unit_scale,snapshot,resolution_evidence_ids,digest,derived_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11) ON CONFLICT (id) DO NOTHING`, c.ID, c.Snapshot.SourceAmount.Currency().String(), c.Snapshot.SourceAmount.MinorUnits(), c.SourceMinorUnitScale, c.Snapshot.TargetAmount.Currency().String(), c.Snapshot.TargetAmount.MinorUnits(), c.Snapshot.TargetMinorUnitScale, string(snap), string(ev), c.Digest, c.Snapshot.DerivedAt.Time())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return nil
	}
	var digest string
	if err := r.db.QueryRowContext(ctx, `SELECT digest FROM fx_conversion_records WHERE id=$1`, c.ID).Scan(&digest); err != nil {
		return err
	}
	if digest != c.Digest {
		return fx.ErrStoreConflict
	}
	return nil
}
