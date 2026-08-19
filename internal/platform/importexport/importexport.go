// Package importexport implements the provider-neutral CSV/JSON import/export
// skeleton. It consumes only release-gated upload references from Task 088 and
// never accepts client-controlled object keys or raw quarantine objects.
package importexport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/torgnexa/torgnexa/internal/core/catalog"
	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/uploads"
)

var (
	ErrInvalid     = errors.New("importexport: invalid value")
	ErrNotReleased = errors.New("importexport: released object required")
	ErrTooLarge    = errors.New("importexport: source exceeds import limit")
	ErrNotReady    = errors.New("importexport: preview contains validation errors")
)

const (
	DefaultMaxImportBytes int64 = 32 * 1024 * 1024
	DefaultMaxRows              = 10000
	DefaultMaxErrors            = 100
	DefaultMaxColumns           = 64
	maxSourceFieldRunes         = 128
)

type Format string

const (
	FormatCSV  Format = "csv"
	FormatJSON Format = "json"
)

func (f Format) Valid() bool { return f == FormatCSV || f == FormatJSON }

type TargetField string

const (
	FieldProductID   TargetField = "product_id"
	FieldCode        TargetField = "code"
	FieldTitle       TargetField = "title"
	FieldDescription TargetField = "description"
)

func (f TargetField) Valid() bool {
	return f == FieldProductID || f == FieldCode || f == FieldTitle || f == FieldDescription
}

// Mapping is reusable and explicitly versioned. Fields map canonical target
// fields to source column/property names.
type Mapping struct {
	ID      string                 `json:"id"`
	Version int64                  `json:"version"`
	Format  Format                 `json:"format"`
	Fields  map[TargetField]string `json:"fields"`
}

func (m Mapping) Validate() error {
	if !validCode(m.ID) || m.Version < 1 || !m.Format.Valid() || len(m.Fields) < 3 || len(m.Fields) > 4 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, required := range []TargetField{FieldProductID, FieldCode, FieldTitle} {
		if _, ok := m.Fields[required]; !ok {
			return ErrInvalid
		}
	}
	for target, source := range m.Fields {
		if !target.Valid() || !validSourceField(source) || seen[source] {
			return ErrInvalid
		}
		seen[source] = true
	}
	return nil
}
func (m Mapping) Fingerprint() string {
	if m.Validate() != nil {
		return ""
	}
	keys := make([]string, 0, len(m.Fields))
	for k := range m.Fields {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	h := sha256.New()
	io.WriteString(h, m.ID)
	io.WriteString(h, "\x00")
	io.WriteString(h, strconv.FormatInt(m.Version, 10))
	io.WriteString(h, "\x00")
	io.WriteString(h, string(m.Format))
	for _, key := range keys {
		io.WriteString(h, "\x00"+key+"="+m.Fields[TargetField(key)])
	}
	return hex.EncodeToString(h.Sum(nil))
}

type RowError struct {
	Row   int    `json:"row"`
	Field string `json:"field,omitempty"`
	Code  string `json:"code"`
}
type ProductPreview struct {
	Row         int    `json:"row"`
	ProductID   string `json:"product_id"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}
type PreviewReport struct {
	SourceSHA256       string           `json:"source_sha256"`
	MappingFingerprint string           `json:"mapping_fingerprint"`
	TotalRows          int              `json:"total_rows"`
	ValidRows          int              `json:"valid_rows"`
	InvalidRows        int              `json:"invalid_rows"`
	ErrorsTruncated    bool             `json:"errors_truncated"`
	Errors             []RowError       `json:"errors"`
	Sample             []ProductPreview `json:"sample"`
}

func (r PreviewReport) Ready() bool {
	return r.TotalRows > 0 && r.InvalidRows == 0 && r.ValidRows == r.TotalRows
}

type ResultReport struct {
	SourceSHA256       string     `json:"source_sha256"`
	MappingFingerprint string     `json:"mapping_fingerprint"`
	TotalRows          int        `json:"total_rows"`
	CreatedRows        int        `json:"created_rows"`
	UnchangedRows      int        `json:"unchanged_rows"`
	FailedRows         int        `json:"failed_rows"`
	ErrorsTruncated    bool       `json:"errors_truncated"`
	Errors             []RowError `json:"errors"`
}

type rowProduct struct {
	row int
	cmd catalog.CreateProduct
}

// PreparedImport has no exported fields and can only be produced by Preview.
// Commit therefore cannot bypass source-integrity and validation checks.
type PreparedImport struct {
	ref      uploads.ReleasedObjectRef
	mapping  Mapping
	report   PreviewReport
	products []rowProduct
}

func (p PreparedImport) Preview() PreviewReport { return p.report }

type ReleasedReader interface {
	OpenReleased(context.Context, tenancy.Scope, uploads.ReleasedObjectRef) (io.ReadCloser, error)
}
type ReleaseAuthorizer interface {
	ValidateReleasedRef(context.Context, tenancy.Scope, uploads.ReleasedObjectRef) error
}
type Catalog interface {
	Product(context.Context, catalog.Scope, catalog.ProductID) (catalog.Product, error)
	CreateProduct(context.Context, catalog.Scope, catalog.CreateProduct, catalog.Mutation) (catalog.Product, error)
}
type Policy struct {
	MaxImportBytes                 int64
	MaxRows, MaxErrors, MaxColumns int
}

func DefaultPolicy() Policy {
	return Policy{DefaultMaxImportBytes, DefaultMaxRows, DefaultMaxErrors, DefaultMaxColumns}
}
func (p Policy) Validate() error {
	if p.MaxImportBytes <= 0 || p.MaxImportBytes > uploads.DefaultMaxFileBytes || p.MaxRows < 1 || p.MaxRows > 100000 || p.MaxErrors < 1 || p.MaxErrors > 10000 || p.MaxColumns < 3 || p.MaxColumns > 512 {
		return ErrInvalid
	}
	return nil
}

type Service struct {
	reader     ReleasedReader
	authorizer ReleaseAuthorizer
	catalog    Catalog
	policy     Policy
	now        func() time.Time
}

func New(reader ReleasedReader, authorizer ReleaseAuthorizer, c Catalog, policy Policy) (*Service, error) {
	if reader == nil || authorizer == nil || c == nil || policy.Validate() != nil {
		return nil, ErrInvalid
	}
	return &Service{reader: reader, authorizer: authorizer, catalog: c, policy: policy, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Service) Preview(ctx context.Context, scope tenancy.Scope, ref uploads.ReleasedObjectRef, mapping Mapping) (PreparedImport, error) {
	if ctx == nil || s == nil || !scope.Valid() || !ref.Valid() || mapping.Validate() != nil {
		return PreparedImport{}, ErrInvalid
	}
	if ref.SizeBytes() > s.policy.MaxImportBytes || !strings.HasPrefix(ref.ObjectKey(), "released/"+scope.OrganizationID().String()+"/"+scope.WorkspaceID().String()+"/") {
		return PreparedImport{}, ErrNotReleased
	}
	data, digest, err := s.readVerified(ctx, scope, ref)
	if err != nil {
		return PreparedImport{}, err
	}
	rows, errs, truncated, err := parseRows(data, mapping, s.policy)
	if err != nil {
		return PreparedImport{}, err
	}
	products := make([]rowProduct, 0, len(rows))
	sample := make([]ProductPreview, 0, min(10, len(rows)))
	seenID, seenCode := map[string]bool{}, map[string]bool{}
	invalidRows := map[int]bool{}
	addErr := func(e RowError) {
		invalidRows[e.Row] = true
		if len(errs) < s.policy.MaxErrors {
			errs = append(errs, e)
		} else {
			truncated = true
		}
	}
	for _, rr := range rows {
		if rr.invalid {
			invalidRows[rr.row] = true
			continue
		}
		pid, e := catalog.ParseProductID(rr.values[string(FieldProductID)])
		cmd := catalog.CreateProduct{ID: pid, Code: rr.values[string(FieldCode)], Title: rr.values[string(FieldTitle)], Description: rr.values[string(FieldDescription)]}
		if e != nil || cmd.Validate() != nil {
			addErr(RowError{Row: rr.row, Code: "invalid_product"})
			continue
		}
		if seenID[pid.String()] {
			addErr(RowError{Row: rr.row, Field: string(FieldProductID), Code: "duplicate_product_id"})
			continue
		}
		if seenCode[cmd.Code] {
			addErr(RowError{Row: rr.row, Field: string(FieldCode), Code: "duplicate_code"})
			continue
		}
		seenID[pid.String()] = true
		seenCode[cmd.Code] = true
		products = append(products, rowProduct{rr.row, cmd})
		if len(sample) < 10 {
			sample = append(sample, ProductPreview{rr.row, pid.String(), cmd.Code, cmd.Title, cmd.Description})
		}
	}
	report := PreviewReport{SourceSHA256: digest, MappingFingerprint: mapping.Fingerprint(), TotalRows: len(rows), ValidRows: len(products), InvalidRows: len(invalidRows), ErrorsTruncated: truncated, Errors: errs, Sample: sample}
	report.InvalidRows = report.TotalRows - report.ValidRows
	if report.TotalRows == 0 {
		return PreparedImport{}, ErrInvalid
	}
	return PreparedImport{ref: ref, mapping: mapping, report: report, products: products}, nil
}

type rawRow struct {
	row     int
	values  map[string]string
	invalid bool
}

func parseRows(data []byte, mapping Mapping, policy Policy) ([]rawRow, []RowError, bool, error) {
	if mapping.Format == FormatCSV {
		return parseCSV(data, mapping, policy)
	}
	return parseJSON(data, mapping, policy)
}
func parseCSV(data []byte, mapping Mapping, policy Policy) ([]rawRow, []RowError, bool, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.ReuseRecord = false
	header, err := r.Read()
	if err != nil || len(header) < 1 || len(header) > policy.MaxColumns {
		return nil, nil, false, ErrInvalid
	}
	idx := map[string]int{}
	for i, h := range header {
		if !validSourceField(h) || idx[h] > 0 || (idx[h] == 0 && i != 0 && header[0] == h) {
			return nil, nil, false, ErrInvalid
		}
		if _, ok := idx[h]; ok {
			return nil, nil, false, ErrInvalid
		}
		idx[h] = i
	}
	for _, src := range mapping.Fields {
		if _, ok := idx[src]; !ok {
			return nil, nil, false, ErrInvalid
		}
	}
	rows := []rawRow{}
	errs := []RowError{}
	truncated := false
	for logical := 1; ; logical++ {
		rec, e := r.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, nil, false, ErrInvalid
		}
		if logical > policy.MaxRows {
			return nil, nil, false, ErrTooLarge
		}
		vals := map[string]string{}
		rowErr := false
		for target, src := range mapping.Fields {
			i := idx[src]
			if i >= len(rec) {
				rowErr = true
				if len(errs) < policy.MaxErrors {
					errs = append(errs, RowError{Row: logical, Field: string(target), Code: "missing_field"})
				} else {
					truncated = true
				}
				continue
			}
			v := rec[i]
			if !validCell(v) {
				rowErr = true
				if len(errs) < policy.MaxErrors {
					errs = append(errs, RowError{Row: logical, Field: string(target), Code: "invalid_text"})
				} else {
					truncated = true
				}
				continue
			}
			vals[string(target)] = v
		}
		_ = rowErr
		rows = append(rows, rawRow{row: logical, values: vals, invalid: rowErr})
	}
	return rows, errs, truncated, nil
}
func parseJSON(data []byte, mapping Mapping, policy Policy) ([]rawRow, []RowError, bool, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('[') {
		return nil, nil, false, ErrInvalid
	}
	rows := []rawRow{}
	errs := []RowError{}
	truncated := false
	logical := 0
	for dec.More() {
		logical++
		if logical > policy.MaxRows {
			return nil, nil, false, ErrTooLarge
		}
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return nil, nil, false, ErrInvalid
		}
		if len(obj) > policy.MaxColumns {
			return nil, nil, false, ErrInvalid
		}
		vals := map[string]string{}
		rowErr := false
		for target, src := range mapping.Fields {
			raw, ok := obj[src]
			if !ok {
				rowErr = true
				if len(errs) < policy.MaxErrors {
					errs = append(errs, RowError{Row: logical, Field: string(target), Code: "missing_field"})
				} else {
					truncated = true
				}
				continue
			}
			v, ok := scalarString(raw)
			if !ok || !validCell(v) {
				rowErr = true
				if len(errs) < policy.MaxErrors {
					errs = append(errs, RowError{Row: logical, Field: string(target), Code: "invalid_text"})
				} else {
					truncated = true
				}
				continue
			}
			vals[string(target)] = v
		}
		rows = append(rows, rawRow{row: logical, values: vals, invalid: rowErr})
	}
	end, err := dec.Token()
	if err != nil || end != json.Delim(']') {
		return nil, nil, false, ErrInvalid
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, nil, false, ErrInvalid
	}
	return rows, errs, truncated, nil
}
func scalarString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case json.Number:
		return x.String(), true
	case bool:
		return strconv.FormatBool(x), true
	case nil:
		return "", true
	default:
		return "", false
	}
}

func (s *Service) Commit(ctx context.Context, scope tenancy.Scope, prepared PreparedImport) (ResultReport, error) {
	if ctx == nil || s == nil || !scope.Valid() || !prepared.ref.Valid() || prepared.mapping.Validate() != nil || prepared.report.MappingFingerprint != prepared.mapping.Fingerprint() || !prepared.report.Ready() {
		return ResultReport{}, ErrNotReady
	}
	// Re-read immutable released bytes and verify digest immediately before writes.
	_, digest, err := s.readVerified(ctx, scope, prepared.ref)
	if err != nil {
		return ResultReport{}, err
	}
	if digest != prepared.report.SourceSHA256 {
		return ResultReport{}, ErrNotReleased
	}
	cs, err := catalog.ParseScope(scope.OrganizationID().String(), scope.WorkspaceID().String())
	if err != nil {
		return ResultReport{}, ErrInvalid
	}
	out := ResultReport{SourceSHA256: digest, MappingFingerprint: prepared.report.MappingFingerprint, TotalRows: prepared.report.TotalRows, Errors: []RowError{}}
	at := s.now().UTC()
	if at.IsZero() {
		return ResultReport{}, ErrInvalid
	}
	for _, rp := range prepared.products {
		mut := catalog.Mutation{EventID: fmt.Sprintf("import.%s.%d", shortFingerprint(digest), rp.row), OccurredAt: at, Source: "import_export"}
		_, e := s.catalog.CreateProduct(ctx, cs, rp.cmd, mut)
		if e == nil {
			out.CreatedRows++
			continue
		}
		if errors.Is(e, catalog.ErrConflict) {
			existing, getErr := s.catalog.Product(ctx, cs, rp.cmd.ID)
			if getErr == nil && sameProduct(existing, rp.cmd) {
				out.UnchangedRows++
				continue
			}
		}
		out.FailedRows++
		if len(out.Errors) < s.policy.MaxErrors {
			out.Errors = append(out.Errors, RowError{Row: rp.row, Code: "commit_failed"})
		} else {
			out.ErrorsTruncated = true
		}
	}
	return out, nil
}

func (s *Service) readVerified(ctx context.Context, scope tenancy.Scope, ref uploads.ReleasedObjectRef) ([]byte, string, error) {
	if s.authorizer.ValidateReleasedRef(ctx, scope, ref) != nil {
		return nil, "", ErrNotReleased
	}
	rc, err := s.reader.OpenReleased(ctx, scope, ref)
	if err != nil {
		return nil, "", ErrNotReleased
	}
	defer rc.Close()
	var b bytes.Buffer
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(&b, h), io.LimitReader(rc, s.policy.MaxImportBytes+1))
	if err != nil {
		return nil, "", err
	}
	if n > s.policy.MaxImportBytes {
		return nil, "", ErrTooLarge
	}
	if n != ref.SizeBytes() {
		return nil, "", ErrNotReleased
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if digest != ref.SHA256() {
		return nil, "", ErrNotReleased
	}
	return b.Bytes(), digest, nil
}

func EncodeProducts(format Format, products []catalog.Product) ([]byte, error) {
	if !format.Valid() {
		return nil, ErrInvalid
	}
	for _, p := range products {
		if p.Validate() != nil {
			return nil, ErrInvalid
		}
	}
	if format == FormatJSON {
		type exportProduct struct {
			ProductID   string         `json:"product_id"`
			Code        string         `json:"code"`
			Title       string         `json:"title"`
			Description string         `json:"description,omitempty"`
			Status      catalog.Status `json:"status"`
			Version     int64          `json:"version"`
		}
		rows := make([]exportProduct, len(products))
		for i, p := range products {
			rows[i] = exportProduct{p.ID.String(), p.Code, p.Title, p.Description, p.Status, p.Version}
		}
		out, err := json.Marshal(rows)
		if err != nil {
			return nil, err
		}
		return append(out, '\n'), nil
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	_ = w.Write([]string{"product_id", "code", "title", "description", "status", "version"})
	for _, p := range products {
		_ = w.Write([]string{p.ID.String(), p.Code, p.Title, p.Description, string(p.Status), strconv.FormatInt(p.Version, 10)})
	}
	w.Flush()
	if w.Error() != nil {
		return nil, w.Error()
	}
	return b.Bytes(), nil
}

func sameProduct(p catalog.Product, c catalog.CreateProduct) bool {
	return p.ID == c.ID && p.Code == c.Code && p.Title == c.Title && p.Description == c.Description
}
func shortFingerprint(v string) string {
	if len(v) > 24 {
		return v[:24]
	}
	return v
}
func validSourceField(v string) bool {
	if v != strings.TrimSpace(v) || !utf8.ValidString(v) || utf8.RuneCountInString(v) < 1 || utf8.RuneCountInString(v) > maxSourceFieldRunes {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func validCell(v string) bool {
	if !utf8.ValidString(v) || len(v) > 20000 {
		return false
	}
	for _, r := range v {
		if r == 0 {
			return false
		}
	}
	return true
}
func validCode(v string) bool {
	if len(v) < 1 || len(v) > 128 {
		return false
	}
	for i, c := range []byte(v) {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '.' || c == '_' || c == ':' || c == '/' || c == '-')) {
			continue
		}
		return false
	}
	return true
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
