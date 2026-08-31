package fivepost

import "testing"

func TestConfigurationRequiresExplicitOrderContract(t *testing.T) {
	valid := Configuration{SenderLocation: "Warehouse_124", ReturnLocation: "Warehouse_124", BrandName: "Магазин", UndeliverableOption: "RETURN", BarcodeEnrichment: "ENABLED"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]Configuration{
		"missing sender location":      {UndeliverableOption: "RETURN", BarcodeEnrichment: "ENABLED"},
		"unknown undeliverable option": {SenderLocation: "Warehouse_124", UndeliverableOption: "DROP", BarcodeEnrichment: "ENABLED"},
		"unknown barcode mode":         {SenderLocation: "Warehouse_124", UndeliverableOption: "RETURN", BarcodeEnrichment: "UNKNOWN"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := value.Validate(); err == nil {
				t.Fatal("expected configuration to be rejected")
			}
		})
	}
}
