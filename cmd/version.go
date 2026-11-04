package cmd

import (
	"runtime/debug"
	"strings"
)

// Version is set at build time via
// -ldflags "-X github.com/LinPr/lazys3/cmd.Version=...".
//
// `go install pkg@version` cannot pass ldflags, so a plain `go install`
// would otherwise report "dev" even though the user installed a tagged
// release. resolvedVersion falls back to the module version the Go
// toolchain records in the binary's build info, which is exactly the tag
// that was installed.
var Version = "dev"

// resolvedVersion is the version reported by --version: the ldflags value
// when a build set one, otherwise the module version from build info.
func resolvedVersion() string {
	if v := strings.TrimSpace(Version); v != "" && v != "dev" {
		return v
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
