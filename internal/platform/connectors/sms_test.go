package connectors

import (
	"testing"
	"time"
)

func TestSMSContractRequiresMarketingConsentAndValidPIIShape(t *testing.T) {
	r := SMSRequest{ExternalID: "n:1", PhoneE164: "+79991234567", Text: "notice", Class: SMSTransactional, IdempotencyKey: "i:1"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Class = SMSMarketing
	if err := r.Validate(); err == nil {
		t.Fatal("marketing consent ref accepted empty")
	}
	r.ConsentRef = "consent:1"
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	out := SMSResult{RemoteID: "sms:1", Status: "accepted", ObservedAt: time.Now().UTC()}
	if err := out.Validate(); err != nil {
		t.Fatal(err)
	}
}
