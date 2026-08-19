package mcpaccounts

import "testing"

func validCreate() CreateAccount {
	return CreateAccount{
		Label: "n8n workflow", AgentID: "agent-1", ModelID: "gpt-5", IntegrationID: "n8n",
		Permissions: []string{"commerce.products.read", "commerce.orders.read"},
	}
}

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate(validCreate()); err != nil {
		t.Fatalf("unexpected error for a valid command: %v", err)
	}

	cases := map[string]CreateAccount{
		"empty label": func() CreateAccount { c := validCreate(); c.Label = ""; return c }(),
		"empty agent_id": func() CreateAccount {
			c := validCreate()
			c.AgentID = ""
			return c
		}(),
		"empty model_id": func() CreateAccount {
			c := validCreate()
			c.ModelID = ""
			return c
		}(),
		"empty integration_id": func() CreateAccount {
			c := validCreate()
			c.IntegrationID = ""
			return c
		}(),
		"no permissions": func() CreateAccount {
			c := validCreate()
			c.Permissions = nil
			return c
		}(),
		"unknown permission": func() CreateAccount {
			c := validCreate()
			c.Permissions = []string{"admin.everything"}
			return c
		}(),
		"duplicate permission": func() CreateAccount {
			c := validCreate()
			c.Permissions = []string{"commerce.products.read", "commerce.products.read"}
			return c
		}(),
		"too many permissions": func() CreateAccount {
			c := validCreate()
			c.Permissions = []string{
				"commerce.products.read", "commerce.orders.read",
				"party.counterparties.read", "commerce.price.change.request",
				"commerce.products.read",
			}
			return c
		}(),
	}
	for name, cmd := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateCreate(cmd); err == nil {
				t.Fatalf("expected an error")
			}
		})
	}
}

func TestTokenRoundTrip(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(secret) != SecretLength {
		t.Fatalf("unexpected secret length: %d", len(secret))
	}
	token := EncodeToken("018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002", "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003", secret)

	decoded, err := DecodeToken(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decoded.OrganizationID != "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0001" || decoded.WorkspaceID != "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0002" || decoded.AccountID != "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0003" {
		t.Fatalf("unexpected decoded routing IDs: %+v", decoded)
	}
	if string(decoded.Secret) != string(secret) {
		t.Fatalf("decoded secret does not match the original")
	}

	presentedHash := HashSecret(decoded.Secret)
	storedHash := HashSecret(secret)
	if string(presentedHash) != string(storedHash) {
		t.Fatalf("hash of the round-tripped secret does not match the original hash")
	}

	// A different secret must never hash to the same value.
	otherSecret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(HashSecret(otherSecret)) == string(storedHash) {
		t.Fatalf("two independently generated secrets produced the same hash")
	}
}

func TestDecodeTokenRejectsMalformed(t *testing.T) {
	cases := []string{
		"",
		"not-an-mcp-token",
		TokenPrefix,
		TokenPrefix + "only.two",
		TokenPrefix + "a.b.c.d.e",
		TokenPrefix + "not-base64!.b.c.d",
	}
	for _, token := range cases {
		if _, err := DecodeToken(token); err == nil {
			t.Fatalf("expected an error for token %q", token)
		}
	}
}
