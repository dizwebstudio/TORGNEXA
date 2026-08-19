package prestashop

import (
	"fmt"
	"math/big"
	"strings"
)

func normalizeMoney(v string) (string, error) {
	r, ok := new(big.Rat).SetString(v)
	if !ok || r.Sign() < 0 {
		return "", ErrInvalidResponse
	}
	return ratDecimal(r), nil
}
func addMoney(a, b string) (string, error) {
	ra, ok := new(big.Rat).SetString(a)
	if !ok {
		return "", ErrInvalidResponse
	}
	rb, ok := new(big.Rat).SetString(b)
	if !ok {
		return "", ErrInvalidResponse
	}
	ra.Add(ra, rb)
	if ra.Sign() < 0 {
		return "", ErrInvalidResponse
	}
	return ratDecimal(ra), nil
}
func subtractMoney(a, b string) (string, error) {
	ra, ok := new(big.Rat).SetString(a)
	if !ok {
		return "", ErrInvalidResponse
	}
	rb, ok := new(big.Rat).SetString(b)
	if !ok {
		return "", ErrInvalidResponse
	}
	ra.Sub(ra, rb)
	return ratDecimalSigned(ra), nil
}
func ratDecimal(r *big.Rat) string {
	s := r.FloatString(9)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" {
		s = "0"
	}
	return s
}
func ratDecimalSigned(r *big.Rat) string                  { return ratDecimal(r) }
func fmtSscanf(str, format string, a ...any) (int, error) { return fmt.Sscanf(str, format, a...) }
