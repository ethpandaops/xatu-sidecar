// Package version provides build version information.
package version

var (
	version = "unknown" //nolint:gochecknoglobals // Set by ldflags
	commit  = "unknown" //nolint:gochecknoglobals // Set by ldflags
	date    = "unknown" //nolint:gochecknoglobals // Set by ldflags
)

// Version returns the full version string.
func Version() string {
	return version
}

// Commit returns the git commit hash.
func Commit() string {
	return commit
}

// Date returns the build date.
func Date() string {
	return date
}

// Short returns just the version number.
func Short() string {
	return version
}

// Full returns version with commit info.
func Full() string {
	if commit != "unknown" && len(commit) >= 7 {
		return version + "-" + commit[:7]
	}
	return version
}
