// Package version holds build-time version information for chatxgo.
package version

// Name is the tool's display name.
const Name = "chatxgo"

// Version is set at build time via -ldflags "-X .../version.Version=vX.Y.Z".
// It is derived from the latest git tag (see Taskfile.yml VERSION/RELEASE_VERSION).
var Version = "dev"

// String returns the "name version" string shown by -v/-version and -h/-help.
func String() string {
	return Name + " " + Version
}
