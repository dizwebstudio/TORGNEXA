package procurement

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParsePriceListCSVNormalizesRowsAndDefaults(t *testing.T) {
	data := []byte("supplier_sku,unit_price_minor,currency,sku\nSUP-1,1250,rub,SKU-1\nSUP-2,900,USD,SKU-2\n")
	rows, issues, err := ParsePriceList(data, "csv", PriceListMapping{Fields: map[string]string{
		"supplier_sku": "supplier_sku", "unit_price_minor": "unit_price_minor", "currency": "currency", "sku": "sku",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 || len(rows) != 2 {
		t.Fatalf("rows=%+v issues=%+v", rows, issues)
	}
	if rows[0].Currency != "RUB" || rows[0].MOQ != "1" || rows[0].CasePack != "1" || rows[0].Unit != "PCS" {
		t.Fatalf("defaults were not normalized: %+v", rows[0])
	}
}

func TestParsePriceListDoesNotCarryPriceErrorAcrossRows(t *testing.T) {
	data := []byte("supplier_sku,unit_price_minor,currency\nSUP-1,nope,RUB\nSUP-2,900,RUB\n")
	rows, issues, err := ParsePriceList(data, "text/csv", PriceListMapping{Fields: map[string]string{
		"supplier_sku": "supplier_sku", "unit_price_minor": "unit_price_minor", "currency": "currency",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || len(issues) != 1 || issues[0].Row != 2 || rows[1].UnitPriceMinor != 900 {
		t.Fatalf("rows=%+v issues=%+v", rows, issues)
	}
}

func TestParsePriceListXLSXFirstSheet(t *testing.T) {
	data := xlsxFixture(t, `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>supplier_sku</t></is></c><c r="B1" t="inlineStr"><is><t>unit_price_minor</t></is></c><c r="C1" t="inlineStr"><is><t>currency</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>SUP-X</t></is></c><c r="B2"><v>700</v></c><c r="C2" t="inlineStr"><is><t>RUB</t></is></c></row></sheetData></worksheet>`)
	rows, issues, err := ParsePriceList(data, "xlsx", PriceListMapping{Fields: map[string]string{
		"supplier_sku": "supplier_sku", "unit_price_minor": "unit_price_minor", "currency": "currency",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 || len(rows) != 1 || rows[0].SupplierSKU != "SUP-X" || rows[0].UnitPriceMinor != 700 {
		t.Fatalf("rows=%+v issues=%+v", rows, issues)
	}
}

func xlsxFixture(t *testing.T, sheet string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	file, err := archive.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte(sheet)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
