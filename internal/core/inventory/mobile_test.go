package inventory

import "testing"

func TestMobileFulfillmentModeOwnership(t *testing.T) {
	if !MobileLocalOperationAllowed(FulfillmentFBS, MobileOperationPick) {
		t.Fatal("FBS pick must be locally executable")
	}
	if MobileLocalOperationAllowed(FulfillmentFBO, MobileOperationPick) || MobileLocalOperationAllowed(FulfillmentFBO, MobileOperationPrint) {
		t.Fatal("FBO local pick/print must be blocked")
	}
	if !MobileLocalOperationAllowed(FulfillmentFBO, MobileOperationObserve) {
		t.Fatal("FBO remote observation must remain available")
	}
	if MobileOwnerForMode(FulfillmentFBO) != OwnerMarketplace || MobileOwnerForMode(FulfillmentFBS) != OwnerSellerWarehouse {
		t.Fatal("unexpected mode owner")
	}
}

func TestMobileScanValidationAndDigest(t *testing.T) {
	quantity, err := NewQuantity(mustMobileDecimal(t, "1"), UnitCode("PCS"))
	if err != nil {
		t.Fatal(err)
	}
	input := MobileScanInput{TaskID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0670", DeviceID: "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0671", Kind: ScanProduct, Code: "04601234567890", LocationCode: "A-01", ExpectedVersion: 1, Quantity: quantity}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(MobileCodeDigest(input.Code)) != 64 || MobileCodeDigest(input.Code) != MobileCodeDigest(input.Code) {
		t.Fatal("code digest must be deterministic and bounded")
	}
	input.Code = "bad\ncode"
	if input.Validate() == nil {
		t.Fatal("control characters in scan code were accepted")
	}
}

func TestMobilePackageAndPrintBounds(t *testing.T) {
	if (PackageFacts{PackageCount: 1, WeightGrams: 100, LengthMM: 200, WidthMM: 100, HeightMM: 50}).Validate() != nil {
		t.Fatal("valid package facts were rejected")
	}
	if (PackageFacts{PackageCount: 0}).Validate() == nil || ValidateMobilePrintRequest(PrintLabel, 21) == nil {
		t.Fatal("unsafe package/print bounds were accepted")
	}
}

func TestMobilePlanValidationKeepsFBORemote(t *testing.T) {
	if ValidateMobilePlan(FulfillmentFBO, OwnerMarketplace, false, "") != nil {
		t.Fatal("valid FBO remote plan was rejected")
	}
	if ValidateMobilePlan(FulfillmentFBO, OwnerSellerWarehouse, true, "warehouse-1") == nil {
		t.Fatal("FBO local plan was accepted")
	}
}

func mustMobileDecimal(t *testing.T, value string) Decimal {
	t.Helper()
	decimal, err := ParseDecimal(value)
	if err != nil {
		t.Fatal(err)
	}
	return decimal
}
