// Package buildinfo exposes version metadata injected at link time with -ldflags.
package buildinfo

import "runtime"

// Values overridden at build time, e.g.
//
//	go build -ldflags "-X github.com/yesoreyeram/dv/internal/buildinfo.Version=1.2.3"
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Info is the public build information document (FR-003).
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
}

// Get returns the build information of the running binary.
func Get() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate, GoVersion: runtime.Version()}
}
