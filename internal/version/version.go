// Package version holds the build version of git-byline.
//
// The release pipeline injects the version string with linker flags:
//
//	-ldflags "-X github.com/comarch/git-byline/internal/version.Version=v0.1.0"
//
// Builds without injection report dev.
package version

import (
	"strconv"
	"strings"
)

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
// validation, so malformed input compares as all zeros.
func CompareReleaseTags(a, b string) int {
	aNums := releaseTagNumbers(a)
	bNums := releaseTagNumbers(b)
	for index := range aNums {
		switch {
		case aNums[index] < bNums[index]:
			return -1
		case aNums[index] > bNums[index]:
			return 1
		}
	}
	return 0
}

// releaseTagNumbers extracts the three numeric fields of one release
// tag, with 0 for fields that fail to parse.
func releaseTagNumbers(tag string) [3]int {
	var out [3]int
	fields := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	if len(fields) != len(out) {
		return out
	}
	for index, field := range fields {
		if number, err := strconv.Atoi(field); err == nil {
			out[index] = number
		}
	}
	return out
}
