package procurement

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/torgnexa/torgnexa/internal/platform/domain"
)

const (
	maxPriceListBytes = 32 << 20
	maxPriceListRows  = 10000
	maxPriceListCols  = 32
)

// PriceListMapping maps normalized fields to source column names. The mapping
// is explicit so a new supplier file cannot silently change offer data.
type PriceListMapping struct {
	Fields map[string]string `json:"fields"`
}

// Fingerprint returns the stable digest of a mapping template.
func (m PriceListMapping) Fingerprint() string {
	keys := []string{"supplier_sku", "gtin", "sku", "unit_price_minor", "minimum_order_minor", "minimum_order_currency", "currency", "moq", "case_pack", "lead_time_days", "priority", "unit"}
	h := sha256.New()
	for _, key := range keys {
		io.WriteString(h, key+"="+m.Fields[key]+"\n")
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ParsePriceList parses a released CSV or XLSX byte stream into bounded,
// provider-neutral rows. It never writes supplier offers.
func ParsePriceList(data []byte, format string, mapping PriceListMapping) ([]PriceListRow, []ImportError, error) {
	if len(data) == 0 || len(data) > maxPriceListBytes || len(mapping.Fields) == 0 {
		return nil, nil, ErrInvalid
	}
	for _, required := range []string{"unit_price_minor", "currency"} {
		if strings.TrimSpace(mapping.Fields[required]) == "" {
			return nil, nil, ErrInvalid
		}
	}
	var records [][]string
	var err error
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "csv", "text/csv":
		records, err = csvRecords(data)
	case "xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		records, err = xlsxRecords(data)
	default:
		return nil, nil, ErrInvalid
	}
	if err != nil || len(records) < 2 || len(records[0]) > maxPriceListCols {
		return nil, nil, ErrInvalid
	}
	indices := make(map[string]int, len(records[0]))
	for index, name := range records[0] {
		name = strings.TrimSpace(name)
		if name == "" || len(name) > 128 || indices[name] != 0 {
			return nil, nil, ErrInvalid
		}
		indices[name] = index + 1
	}
	for _, source := range mapping.Fields {
		if source == "" || indices[source] == 0 {
			return nil, nil, ErrInvalid
		}
	}
	rows := make([]PriceListRow, 0, len(records)-1)
	errorsFound := make([]ImportError, 0, 32)
	for rowNumber, record := range records[1:] {
		if rowNumber+1 > maxPriceListRows {
			return nil, nil, ErrInvalid
		}
		row := PriceListRow{Row: rowNumber + 2, Unit: value(record, indices, mapping.Fields["unit"]), Currency: strings.ToUpper(value(record, indices, mapping.Fields["currency"])), SupplierSKU: value(record, indices, mapping.Fields["supplier_sku"]), GTIN: value(record, indices, mapping.Fields["gtin"]), SKU: value(record, indices, mapping.Fields["sku"]), MOQ: value(record, indices, mapping.Fields["moq"]), CasePack: value(record, indices, mapping.Fields["case_pack"]), MinimumOrderCurrency: strings.ToUpper(value(record, indices, mapping.Fields["minimum_order_currency"]))}
		if row.Unit == "" {
			row.Unit = "PCS"
		}
		priceRaw := value(record, indices, mapping.Fields["unit_price_minor"])
		priceErr := error(nil)
		if priceRaw != "" {
			row.UnitPriceMinor, priceErr = strconv.ParseInt(priceRaw, 10, 64)
		} else {
			priceErr = ErrInvalid
		}
		if priceErr != nil || row.UnitPriceMinor < 0 {
			errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "unit_price_minor", Code: "invalid_price"})
		}
		if raw := value(record, indices, mapping.Fields["minimum_order_minor"]); raw != "" {
			row.MinimumOrderMinor, err = strconv.ParseInt(raw, 10, 64)
			if err != nil || row.MinimumOrderMinor < 0 {
				errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "minimum_order_minor", Code: "invalid_price"})
			}
		}
		if row.MinimumOrderCurrency == "" {
			row.MinimumOrderCurrency = row.Currency
		}
		if _, err := domain.NewCurrency(row.Currency); err != nil {
			errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "currency", Code: "invalid_currency"})
		}
		if row.SupplierSKU == "" && row.GTIN == "" && row.SKU == "" {
			errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "gtin", Code: "missing_match_key"})
		}
		if row.GTIN != "" && (len(row.GTIN) < 8 || len(row.GTIN) > 14 || !digits(row.GTIN)) {
			errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "gtin", Code: "invalid_gtin"})
		}
		if row.MOQ == "" {
			row.MOQ = "1"
		}
		if row.CasePack == "" {
			row.CasePack = "1"
		}
		if _, err := domain.ParseDecimal(row.MOQ); err != nil {
			errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "moq", Code: "invalid_quantity"})
		}
		if _, err := domain.ParseDecimal(row.CasePack); err != nil {
			errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "case_pack", Code: "invalid_quantity"})
		}
		if raw := value(record, indices, mapping.Fields["lead_time_days"]); raw != "" {
			row.LeadTimeDays, err = strconv.Atoi(raw)
			if err != nil || row.LeadTimeDays < 0 || row.LeadTimeDays > 3650 {
				errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "lead_time_days", Code: "invalid_lead_time"})
			}
		}
		if raw := value(record, indices, mapping.Fields["priority"]); raw != "" {
			row.Priority, err = strconv.Atoi(raw)
			if err != nil || row.Priority < 0 {
				errorsFound = appendBounded(errorsFound, ImportError{Row: row.Row, Field: "priority", Code: "invalid_priority"})
			}
		}
		rows = append(rows, row)
	}
	return rows, errorsFound, nil
}

func csvRecords(data []byte) ([][]string, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	return readRecords(reader)
}

func readRecords(reader *csv.Reader) ([][]string, error) {
	var records [][]string
	for len(records) <= maxPriceListRows {
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		if len(row) > maxPriceListCols {
			return nil, ErrInvalid
		}
		records = append(records, row)
	}
	return nil, ErrInvalid
}

type xlsxWorksheet struct {
	SheetData xlsxSheetData `xml:"sheetData"`
}
type xlsxSheetData struct {
	Rows []xlsxRow `xml:"row"`
}
type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}
type xlsxCell struct {
	Ref    string `xml:"r,attr"`
	Kind   string `xml:"t,attr"`
	Value  string `xml:"v"`
	Inline string `xml:"is>t"`
}
type xlsxSharedStrings struct {
	Items []struct {
		Text string `xml:"t"`
	} `xml:"si"`
}

func xlsxRecords(data []byte) ([][]string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, ErrInvalid
	}
	var sheet, shared []byte
	for _, file := range archive.File {
		if file.Name != "xl/worksheets/sheet1.xml" && file.Name != "xl/sharedStrings.xml" {
			continue
		}
		if file.UncompressedSize64 > maxPriceListBytes {
			return nil, ErrInvalid
		}
		opened, err := file.Open()
		if err != nil {
			return nil, ErrInvalid
		}
		value, readErr := io.ReadAll(io.LimitReader(opened, maxPriceListBytes+1))
		_ = opened.Close()
		if readErr != nil || len(value) > maxPriceListBytes {
			return nil, ErrInvalid
		}
		if file.Name == "xl/worksheets/sheet1.xml" {
			sheet = value
		} else {
			shared = value
		}
	}
	if len(sheet) == 0 {
		return nil, ErrInvalid
	}
	var stringsTable xlsxSharedStrings
	if len(shared) > 0 && xml.Unmarshal(shared, &stringsTable) != nil {
		return nil, ErrInvalid
	}
	var worksheet xlsxWorksheet
	if xml.Unmarshal(sheet, &worksheet) != nil {
		return nil, ErrInvalid
	}
	records := make([][]string, 0, len(worksheet.SheetData.Rows))
	for _, xlsxRow := range worksheet.SheetData.Rows {
		record := make([]string, maxColumn(xlsxRow.Cells))
		for _, cell := range xlsxRow.Cells {
			column := columnNumber(cell.Ref)
			if column < 1 || column > maxPriceListCols {
				return nil, ErrInvalid
			}
			value := cell.Value
			if cell.Kind == "inlineStr" {
				value = cell.Inline
			} else if cell.Kind == "s" {
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || index >= len(stringsTable.Items) {
					return nil, ErrInvalid
				}
				value = stringsTable.Items[index].Text
			}
			record[column-1] = strings.TrimSpace(value)
		}
		records = append(records, record)
	}
	return records, nil
}

func maxColumn(cells []xlsxCell) int {
	max := 0
	for _, cell := range cells {
		if value := columnNumber(cell.Ref); value > max {
			max = value
		}
	}
	return max
}
func columnNumber(ref string) int {
	letters := strings.TrimRight(ref, "0123456789")
	if letters == "" {
		return 0
	}
	result := 0
	for _, r := range strings.ToUpper(letters) {
		if r < 'A' || r > 'Z' {
			return 0
		}
		result = result*26 + int(r-'A'+1)
	}
	return result
}

func value(record []string, indices map[string]int, source string) string {
	if source == "" || indices[source] < 1 || indices[source]-1 >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[indices[source]-1])
}
func appendBounded(items []ImportError, item ImportError) []ImportError {
	if len(items) < 100 {
		return append(items, item)
	}
	return items
}
func digits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
