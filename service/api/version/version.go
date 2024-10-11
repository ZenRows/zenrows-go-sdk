// Package version provides a location to set the release versions for all
// packages to consume, without creating import cycles.
//
// This package should not import any other packages.
package version

import (
	"fmt"

	"github.com/hashicorp/go-version"
)

// Version is the main version number that is being run at the moment.
const Version = "1.0.0"

// Prerelease is a prerelease marker for the version. If this is "" (empty string)
// then it means that it is a final release. Otherwise, this is a prerelease
// such as "dev" (in development), "beta", "rc1", etc.
const Prerelease = ""

// SemVer is an instance of version.Version. This has the secondary
// benefit of verifying during tests and init time that our version is a
// proper semantic version, which should always be the case.
//
//goland:noinspection GoUnusedGlobalVariable
var SemVer *version.Version

func init() {
	SemVer = version.Must(version.NewVersion(Version))
}

// String returns the complete version string, including prerelease
func String() string {
	//goland:noinspection GoBoolExpressions
	if Prerelease != "" {
		return fmt.Sprintf("%s-%s", Version, Prerelease)
	}
	return Version
}
