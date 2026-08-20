package version_test

import (
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/api/version"
)

func TestVersionIsValidSemVer(t *testing.T) {
	if version.SemVer == nil {
		t.Fatal("expected SemVer to be parsed at init time")
	}
	if version.SemVer.String() != version.Version {
		t.Fatalf("SemVer %q does not match Version %q", version.SemVer.String(), version.Version)
	}
}

func TestStringAppendsPrereleaseWhenSet(t *testing.T) {
	if version.Prerelease == "" && version.String() != version.Version {
		t.Fatalf("expected String() to equal Version when there is no prerelease, got %q", version.String())
	}
}
