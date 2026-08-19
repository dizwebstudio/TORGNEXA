package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-pdf/fpdf"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/reportrepo"
	"golang.org/x/image/font/gofont/goregular"
)

const reportPDFFontFamily = "go-regular"

func writeReportPDF(w http.ResponseWriter, data reportrepo.Data) error {
	document := fpdf.New("L", "mm", "A4", "")
	document.SetMargins(10, 10, 10)
	document.SetAutoPageBreak(false, 10)
	document.AddUTF8FontFromBytes(reportPDFFontFamily, "", goregular.TTF)
	if document.Error() != nil {
		return document.Error()
	}

	columnWidth := reportPDFColumnWidth(len(data.Columns))
	writeHeader := func() {
		document.SetFont(reportPDFFontFamily, "", 8)
		document.SetFillColor(238, 242, 248)
		document.SetTextColor(23, 32, 51)
		for _, column := range data.Columns {
			document.CellFormat(columnWidth, 7, fitPDFText(document, column.Label, columnWidth-3), "1", 0, "L", true, 0, "")
		}
		document.Ln(-1)
	}
	addPage := func(first bool) {
		document.AddPage()
		document.SetFont(reportPDFFontFamily, "", 16)
		if first {
			document.CellFormat(0, 8, reportPDFTitle(data.ID), "", 1, "L", false, 0, "")
			document.SetFont(reportPDFFontFamily, "", 8)
			document.SetTextColor(102, 112, 133)
			document.CellFormat(0, 6, "Сформирован: "+data.GeneratedAt.UTC().Format("02.01.2006 15:04:05 UTC")+" · Источник: "+reportPDFSource(data.Source), "", 1, "L", false, 0, "")
			document.Ln(3)
		} else {
			document.SetFont(reportPDFFontFamily, "", 9)
			document.CellFormat(0, 6, reportPDFTitle(data.ID)+" · продолжение", "", 1, "L", false, 0, "")
		}
		writeHeader()
	}

	addPage(true)
	for _, row := range data.Rows {
		if document.GetY()+7 > 200 {
			addPage(false)
		}
		document.SetFont(reportPDFFontFamily, "", 8)
		document.SetFillColor(255, 255, 255)
		document.SetTextColor(23, 32, 51)
		for index := range data.Columns {
			value := reportPDFValue(data.Columns, row, index)
			document.CellFormat(columnWidth, 7, fitPDFText(document, value, columnWidth-3), "1", 0, "L", false, 0, "")
		}
		document.Ln(-1)
	}

	document.SetY(205)
	document.SetFont(reportPDFFontFamily, "", 7)
	document.SetTextColor(102, 112, 133)
	document.CellFormat(0, 5, "TORGNEXA · Отчёт текущего рабочего пространства", "", 0, "R", false, 0, "")

	var output bytes.Buffer
	if err := document.Output(&output); err != nil {
		return err
	}
	filename := safeReportFilename(data.ID) + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(output.Len()))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(output.Bytes())
	return nil
}

func reportPDFValue(columns []reportrepo.Column, row []string, index int) string {
	if index >= len(columns) || index >= len(row) {
		return ""
	}
	value := cleanPDFText(row[index])
	switch columns[index].Key {
	case "gross_minor_units":
		currency := ""
		for columnIndex, column := range columns {
			if column.Key == "currency" && columnIndex < len(row) {
				currency = cleanPDFText(row[columnIndex])
				break
			}
		}
		return formatPDFMinorUnits(value, currency)
	case "changed_at", "last_occurred_at", "last_ingested_at":
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed.UTC().Format("02.01.2006 15:04:05")
		}
	}
	return value
}

func formatPDFMinorUnits(value, currency string) string {
	sign := ""
	if strings.HasPrefix(value, "-") {
		sign = "-"
		value = strings.TrimPrefix(value, "-")
	}
	if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return cleanPDFText(strings.TrimSpace(sign + value + " " + currency))
	}
	for len(value) < 3 {
		value = "0" + value
	}
	whole, cents := value[:len(value)-2], value[len(value)-2:]
	for position := len(whole) - 3; position > 0; position -= 3 {
		whole = whole[:position] + " " + whole[position:]
	}
	return cleanPDFText(sign + whole + "," + cents + " " + currency)
}

func reportPDFColumnWidth(columns int) float64 {
	if columns < 1 {
		return 277
	}
	return 277 / float64(columns)
}

func fitPDFText(document *fpdf.Fpdf, value string, width float64) string {
	value = cleanPDFText(value)
	if document.GetStringWidth(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && document.GetStringWidth(string(runes)+"…") > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func cleanPDFText(value string) string {
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func safeReportFilename(value string) string {
	var filename strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			filename.WriteRune(r)
		}
	}
	if filename.Len() == 0 {
		return "report"
	}
	return filename.String()
}

func reportPDFTitle(id string) string {
	switch id {
	case "sales_daily":
		return "Продажи по дням"
	case "inventory_current":
		return "Текущие остатки"
	case "ingestion_freshness":
		return "Свежесть данных"
	default:
		return fmt.Sprintf("Отчёт %s", cleanPDFText(id))
	}
}

func reportPDFSource(source string) string {
	if source == "clickhouse" {
		return "ClickHouse"
	}
	if source == "postgresql" {
		return "PostgreSQL"
	}
	return cleanPDFText(source)
}
