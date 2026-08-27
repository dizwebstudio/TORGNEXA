package api

import (
	"testing"

	"github.com/torgnexa/torgnexa/internal/core/social"
)

func TestRelatedSocialIDPreservesUUIDv7AndSeparatesRecords(t *testing.T) {
	publication := "018f0e8b-8a58-7f42-8c2d-5c2f9b1a0204"
	content, err := relatedSocialID(publication, 1)
	if err != nil {
		t.Fatal(err)
	}
	variant, err := relatedSocialID(publication, 2)
	if err != nil {
		t.Fatal(err)
	}
	if content == variant || content == publication || variant == publication {
		t.Fatalf("derived IDs are not distinct: %s %s %s", publication, content, variant)
	}
	if _, err := social.ParseContentID(content); err != nil {
		t.Fatal(err)
	}
	if _, err := social.ParseVariantID(variant); err != nil {
		t.Fatal(err)
	}
}

func TestSocialRoutesExposeOnlyCanonicalChannelAndPublicationSurface(t *testing.T) {
	routes := newSocialRoutes(nil, nil, nil)
	if len(routes) != 5 {
		t.Fatalf("got %d social routes, want 5", len(routes))
	}
	for _, route := range routes {
		if route.Permission != "connectors.read" && route.Permission != "connectors.accounts.write" {
			t.Fatalf("unexpected permission %q", route.Permission)
		}
	}
}

func TestValidSocialTextAppliesProductionLimit(t *testing.T) {
	if !validSocialText("Привет, мир", 4000) {
		t.Fatal("valid Unicode text was rejected")
	}
	for _, value := range []string{"", " с пробелом", string(make([]rune, 4001))} {
		if validSocialText(value, 4000) {
			t.Fatalf("invalid social text was accepted: length=%d", len([]rune(value)))
		}
	}
	if validSocialText("text", 0) || validSocialText("text", -1) {
		t.Fatal("non-executable social text limit was accepted")
	}
}
