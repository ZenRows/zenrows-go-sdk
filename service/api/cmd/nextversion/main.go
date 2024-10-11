package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/zenrows/zenrows-go-sdk/service/api/version"
)

func main() {
	major := flag.Bool("major", false, "Increment the major version")
	minor := flag.Bool("minor", false, "Increment the minor version")
	patch := flag.Bool("patch", false, "Increment the patch version")
	flag.Parse()

	parts := strings.Split(version.Version, ".")
	if len(parts) != 3 {
		fmt.Println("Invalid version format. Must be in the form 'MAJOR.MINOR.PATCH'")
		os.Exit(1)
	}

	var majorVer, minorVer, patchVer int
	_, err := fmt.Sscanf(version.Version, "%d.%d.%d", &majorVer, &minorVer, &patchVer)
	if err != nil {
		fmt.Println("Error parsing version:", err)
		os.Exit(1)
	}

	switch {
	case *major:
		majorVer++
		minorVer = 0
		patchVer = 0
	case *minor:
		minorVer++
		patchVer = 0
	case *patch:
		patchVer++
	default:
		fmt.Println("Please provide a flag: -major, -minor, or -patch")
		os.Exit(1)
	}

	fmt.Printf("%d.%d.%d\n", majorVer, minorVer, patchVer)
}
