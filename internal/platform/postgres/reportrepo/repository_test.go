package reportrepo

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAvailableUsesCommonDecimalScale(t *testing.T) {
	cases := []struct {
		onHand, onScale, reserved, reservedScale int64
		want                                     string
	}{
		{48, 0, 7, 0, "41"},
		{125, 1, 25, 1, "10.0"},
		{12, 0, 25, 1, "9.5"},
	}
	for _, test := range cases {
		if got := available(test.onHand, test.onScale, test.reserved, test.reservedScale); got != test.want {
			t.Fatalf("available(%d,%d,%d,%d)=%q want %q", test.onHand, test.onScale, test.reserved, test.reservedScale, got, test.want)
		}
	}
}

func TestEmptyReportRowsEncodeAsArray(t *testing.T) {
	encoded, err := json.Marshal(Data{ID: "ingestion_freshness", Columns: []Column{{Key: "events", Label: "События"}}, Rows: make([][]string, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"rows":[]`) {
		t.Fatalf("empty rows must be a JSON array: %s", encoded)
	}
}
