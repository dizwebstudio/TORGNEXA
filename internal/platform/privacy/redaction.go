package privacy

import (
	"net"
	"regexp"
	"strings"

	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

const RedactedPIIValue = "[REDACTED_PII]"

var (
	fieldKeyReplacer = strings.NewReplacer("-", "_", ".", "_", " ", "_", ":", "_")
	emailPattern     = regexp.MustCompile(`(?i)^[a-z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)
)

// FieldClass classifies common field names. Explicit schema metadata remains
// authoritative; this helper is a fail-safe for audit/log/support surfaces.
func FieldClass(key string) (DataClass, bool) {
	if secrets.SensitiveKey(key) {
		return ClassSecret, true
	}
	normalized := fieldKeyReplacer.Replace(strings.ToLower(strings.TrimSpace(key)))
	if normalized == "ip" {
		return ClassSensitiveOperational, true
	}
	compact := strings.ReplaceAll(normalized, "_", "")
	for _, marker := range []string{"email", "firstname", "lastname", "fullname", "customername", "recipientname", "contactname", "middlename", "patronymic", "phone", "mobile", "telephone", "birthdate", "dateofbirth", "address", "passport", "snils"} {
		if strings.Contains(compact, marker) {
			return ClassPersonal, true
		}
	}
	for _, marker := range []string{"ipaddress", "clientip", "remoteip", "deviceid", "advertisingid", "useragent", "geolocation", "latitude", "longitude"} {
		if strings.Contains(compact, marker) {
			return ClassSensitiveOperational, true
		}
	}
	return "", false
}

// ValueClass recognizes a deliberately narrow set of high-confidence PII value
// shapes. It never attempts identity inference from arbitrary free text.
func ValueClass(value string) (DataClass, bool) {
	trimmed := strings.TrimSpace(value)
	if secrets.SensitiveString(trimmed) {
		return ClassSecret, true
	}
	if len(trimmed) <= 320 && emailPattern.MatchString(trimmed) {
		return ClassPersonal, true
	}
	if ip := net.ParseIP(trimmed); ip != nil {
		return ClassSensitiveOperational, true
	}
	return "", false
}

func RedactionForKey(key string) (string, bool) {
	class, ok := FieldClass(key)
	if !ok {
		return "", false
	}
	if class.Secret() {
		return secrets.RedactedValue, true
	}
	return RedactedPIIValue, true
}

func RedactString(key, value string) string {
	if marker, ok := RedactionForKey(key); ok {
		return marker
	}
	class, ok := ValueClass(value)
	if !ok {
		return value
	}
	if class.Secret() {
		return secrets.RedactedValue
	}
	return RedactedPIIValue
}
