package cli

import (
	"fmt"
	"io"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
)

// Version can be set at build time with -ldflags -X. Development builds use
// 0.1.0-dev; go install at a published version uses the module's build metadata.
var Version = "0.1.0-dev"

// frameworkVersion ties generated applications to the installed CLI release.
func frameworkVersion() string {
	if Version != "0.1.0-dev" {
		return "v" + strings.TrimPrefix(Version, "v")
	}
	if info, ok := debug.ReadBuildInfo(); ok && publishedVersion(info.Main.Version) {
		return info.Main.Version
	}
	return "v0.1.0"
}

var pseudoVersion = regexp.MustCompile(`-[0-9]{14}-[a-f0-9]+(?:\+incompatible)?$`)

func publishedVersion(version string) bool {
	return strings.HasPrefix(version, "v") && !strings.Contains(version, "+dirty") && !pseudoVersion.MatchString(version)
}

func versionCommand(args []string, out, errOut io.Writer) error {
	f := flags("version", errOut)
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("version takes no positional arguments")
	}
	version := Version
	if version == "0.1.0-dev" {
		if info, ok := debug.ReadBuildInfo(); ok && publishedVersion(info.Main.Version) {
			version = info.Main.Version
		}
	}
	_, err := fmt.Fprintf(out, "Graft %s (%s %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
	return err
}
