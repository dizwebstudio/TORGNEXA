package secrets

import "strings"

const RedactedValue = "[REDACTED]"

var keyReplacer = strings.NewReplacer("-", "_", ".", "_", " ", "_", ":", "_")

// SensitiveKey reports whether a field name is credential-bearing. Count/metric fields are explicitly preserved.
func SensitiveKey(key string) bool {
	normalized := keyReplacer.Replace(strings.ToLower(strings.TrimSpace(key)))
	if normalized == "token_count" || strings.HasSuffix(normalized, "_token_count") {
		return false
	}
	compact := strings.ReplaceAll(normalized, "_", "")
	for _, marker := range []string{"authorization", "proxyauthorization", "password", "passwd", "secret", "credential", "privatekey", "privatematerial", "signingkey", "signingmaterial", "verificationcode", "datamatrix", "apikey", "cookie", "setcookie", "sessionid", "accesstoken", "refreshtoken", "idtoken", "apitoken", "accesskey"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	for _, segment := range strings.Split(normalized, "_") {
		switch segment {
		case "authorization", "password", "passwd", "secret", "credential", "credentials", "token", "cookie", "session":
			return true
		}
	}
	return strings.HasPrefix(compact, "token") || strings.HasSuffix(compact, "token")
}

// SensitiveString identifies common credential values that must never be persisted or logged verbatim.
func SensitiveString(value string) bool {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"bearer ", "basic ", "digest ", "negotiate ", "aws4-hmac-sha256 "} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, marker := range []string{"access_token=", "refresh_token=", "client_secret=", "api_key=", "apikey=", "token=", "password=", "authorization="} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.Count(trimmed, ".") == 2 && strings.HasPrefix(trimmed, "eyJ") {
		return true
	}
	upper := strings.ToUpper(trimmed)
	return strings.Contains(upper, "-----BEGIN ") && strings.Contains(upper, "PRIVATE KEY-----")
}

// RedactText returns a fixed marker for credential-shaped text and otherwise leaves it unchanged.
func RedactText(value string) string {
	if SensitiveString(value) {
		return RedactedValue
	}
	return value
}
