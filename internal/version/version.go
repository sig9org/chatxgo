// Package version holds build-time version information for chatxgo.
package version

import "runtime/debug"

// Name is the tool's display name.
const Name = "chatxgo"

// Version is set at build time via -ldflags "-X .../version.Version=vX.Y.Z".
// It is derived from the latest git tag (see Taskfile.yml VERSION/RELEASE_VERSION).
var Version = "dev"

// String returns the "name version (commit)" string shown by -v/-version and
// -h/-help. The commit hash comes from Go's automatic VCS build stamping
// (runtime/debug.ReadBuildInfo), so it is present for both Task-built
// binaries and plain `go build`/`go install` from within the git checkout;
// it is omitted if that information isn't available.
func String() string {
	if c := commit(); c != "" {
		return Name + " " + Version + " (" + c + ")"
	}
	return Name + " " + Version
}

func commit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 7 {
				return s.Value[:7]
			}
			return s.Value
		}
	}
	return ""
}
