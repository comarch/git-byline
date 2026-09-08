// Package version holds the build version of git-byline.
//
// The release pipeline injects the version string with linker flags:
//
//	-ldflags "-X github.com/mrwogu/git-byline/internal/version.Version=v0.1.0"
//
// Builds without injection report dev.
package version

// Version is the git-byline version. Release builds inject the validated
// git tag; local and CI builds keep the dev default.
var Version = "dev"
