package api

import (
	"strings"
	"testing"
)

func TestProfileFromOIDCClaimsUsesUserInfoAndDropsUnsafeOptionalClaims(t *testing.T) {
	claims := oidcClaims{Username: "token-name", Email: "token@example.test", GivenName: "Token", FamilyName: "User", PictureURL: "https://id.example.test/token.png", Birthdate: "1980-01-01", JobTitle: "Token title", Department: "Token department", PhoneNumber: "+7 000 000-00-00"}
	info := userInfoClaims{Username: "userinfo-name", Email: "USERINFO@EXAMPLE.TEST", GivenName: "Инфо", FamilyName: "Пользователь", PictureURL: "https://id.example.test/userinfo.png", Birthdate: "1988-04-17", Position: "Старший оператор", Department: "Коммерческие операции", PhoneNumber: "+7 900 000-00-00"}
	profile := profileFromOIDCClaims(claims, info, strings.Repeat("a", 64))
	if !profile.Valid() || profile.Username != "userinfo-name" || profile.Email != "userinfo@example.test" || profile.GivenName != "Инфо" || profile.PictureURL != info.PictureURL || profile.Birthdate != info.Birthdate || profile.JobTitle != info.Position || profile.Department != info.Department || profile.PhoneNumber != info.PhoneNumber {
		t.Fatalf("unexpected profile projection: %#v", profile)
	}

	unsafe := oidcClaims{Email: "not-an-email", PictureURL: "javascript:alert(1)", Birthdate: "1988-02-31", JobTitle: "\x00bad"}
	profile = profileFromOIDCClaims(unsafe, userInfoClaims{}, strings.Repeat("b", 64))
	if !profile.Valid() || profile.Email != "" || profile.PictureURL != "" || profile.Birthdate != "" || profile.JobTitle != "" {
		t.Fatalf("unsafe claims were retained: %#v", profile)
	}
}
