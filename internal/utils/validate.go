package utils

import (
	"regexp"
	"strconv"
)

var (
	phoneRe   = regexp.MustCompile(`^(?:\+91)?([6-9][0-9]{9})$`)
	pincodeRe = regexp.MustCompile(`^[1-9][0-9]{5}$`)
)

// NormalizePhone validates an Indian mobile number (optional +91) and returns its 10 digits.
func NormalizePhone(s string) (string, bool) {
	m := phoneRe.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// ValidPincode reports whether p is a 6-digit Indian pincode.
func ValidPincode(p int) bool {
	return p >= 100000 && p <= 999999
}

// ParsePincode parses a 6-digit Indian pincode from a string (e.g. a query param).
func ParsePincode(s string) (int, bool) {
	if !pincodeRe.MatchString(s) {
		return 0, false
	}
	p, _ := strconv.Atoi(s)
	return p, true
}
