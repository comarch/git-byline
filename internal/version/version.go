// Package version holds the build version of git-byline.
//
// The release pipeline injects the version string with linker flags:
//
//	-ldflags "-X github.com/comarch/git-byline/internal/version.Version=v0.1.0"
//
// Builds without injection report dev.
package version

import "strings"

// Version is the git-byline version. Release builds inject the validated
// git tag, with its v prefix; local and CI builds keep the dev default.
var Version = "dev"

// IsRelease reports whether Version was stamped by a release build, so
// callers can compare it against release tags only when it is meaningful.
func IsRelease() bool {
	return Version != "dev"
}

// CompareReleaseTags orders two release tags, vMAJOR.MINOR.PATCH. It
// returns -1 when a sorts before b, 0 when they are equal, and 1 when a
// sorts after b. Callers pass tags that already passed the tag shape
// validation, so malformed input compares as all zeros. Fields compare
// as normalized decimals, so tags beyond the platform int range still
// order correctly on every build.
func CompareReleaseTags(a, b string) int {
	fieldsA := releaseTagFields(a)
	fieldsB := releaseTagFields(b)
	for index := range fieldsA {
		if order := compareDecimalFields(fieldsA[index], fieldsB[index]); order != 0 {
			return order
		}
	}
	return 0
}

// releaseTagFields extracts the three numeric fields of one release tag,
// with "0" for tags and fields that do not match the release shape.
func releaseTagFields(tag string) [3]string {
	var out [3]string
	for index := range out {
		out[index] = "0"
	}
	fields := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(fields) != len(out) {
		return out
	}
	for _, field := range fields {
		if !isDecimal(field) {
			return out
		}
	}
	copy(out[:], fields)
	return out
}

// isDecimal reports whether field is a non-empty run of digits.
func isDecimal(field string) bool {
	if field == "" {
		return false
	}
	for _, digit := range field {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

// compareDecimalFields orders two decimal strings without a fixed-width
// conversion, so no field length can overflow. Leading zeros do not
// affect the order.
func compareDecimalFields(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
